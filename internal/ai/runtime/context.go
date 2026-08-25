package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"SentinelOps/internal/ai/budgetctx"
	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

// HistoryMessagesFromRevision 将已提交 Session Revision 转换为一次性 Agent History。
// 该函数只读取 durable JSON；Redis、进程内 Memory 和临时缓存不参与 durable Attempt。
func HistoryMessagesFromRevision(stateJSON json.RawMessage) ([]*schema.Message, error) {
	if len(stateJSON) == 0 || !json.Valid(stateJSON) {
		return nil, fmt.Errorf("durable session revision is invalid")
	}
	var state struct {
		Schema     string          `json:"schema"`
		Revision   uint64          `json:"revision"`
		Preference json.RawMessage `json:"preference"`
		Summary    string          `json:"summary"`
		History    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"history"`
	}
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, fmt.Errorf("decode durable session revision: %w", err)
	}
	if state.Schema != workflow.SessionStateSchemaV1 {
		return nil, fmt.Errorf("unsupported durable session revision schema %q", state.Schema)
	}
	messages := make([]*schema.Message, 0, len(state.History)+2)
	if strings.TrimSpace(string(state.Preference)) != "" && string(state.Preference) != "null" && string(state.Preference) != "{}" {
		messages = append(messages, schema.UserMessage("【用户偏好】\n"+string(state.Preference)))
	}
	if strings.TrimSpace(state.Summary) != "" {
		messages = append(messages, schema.UserMessage("【历史对话摘要】\n"+state.Summary))
	}
	for index, item := range state.History {
		role := schema.RoleType(item.Role)
		if role != schema.User && role != schema.Assistant && role != schema.System && role != schema.Tool {
			return nil, fmt.Errorf("durable history entry %d has unsupported role %q", index, item.Role)
		}
		messages = append(messages, &schema.Message{Role: role, Content: item.Content})
	}
	return messages, nil
}

const (
	// SessionRunIDKey 保存 checkpoint-safe 的 immutable Run ID。
	SessionRunIDKey = "sentinelops.run_id"
	// SessionRuntimeVersionKey 保存 checkpoint-safe 的 Runtime Version。
	SessionRuntimeVersionKey = "sentinelops.runtime_version"
	// SessionCompatibilityHashKey 保存 checkpoint-safe 的 compatibility hash。
	SessionCompatibilityHashKey = "sentinelops.runtime_compatibility_hash"
)

var (
	// ErrAttemptContextMissing 表示 durable 调用绕过了 typed Runtime Context builder。
	ErrAttemptContextMissing = errors.New("typed Runtime Context is missing")
	// ErrUnsafeSessionValue 表示值不属于官方状态或项目 immutable scalar allowlist。
	ErrUnsafeSessionValue = errors.New("unsafe Eino SessionValues entry")
)

// BudgetHandle 是 P14 记账服务在 typed Context 中的非序列化运行时引用。
type BudgetHandle interface {
	RuntimeBudgetHandle()
}

// BudgetState 是 Budget service 为当前 Attempt 从 MySQL 重建 handle 的持久化输入。
type BudgetState struct {
	RunID        string
	Limits       json.RawMessage
	Usage        json.RawMessage
	Reservations json.RawMessage
}

// BudgetHandleFactory 由 P14 的唯一 Budget service 实现，P11 不建立平行记账逻辑。
type BudgetHandleFactory interface {
	RebuildBudgetHandle(context.Context, BudgetState) (BudgetHandle, error)
}

// TraceIdentity 是每次实际 Attempt 新建的不可变 Trace 引用。
type TraceIdentity struct {
	ID string
}

// RunIdentity 是 Attempt 所属 Run 的不可变数据库身份。
type RunIdentity struct {
	ID                       string
	SessionID                string
	Attempt                  uint
	LeaseGeneration          uint64
	RuntimeVersion           string
	RuntimeCompatibilityHash string
}

// AttemptContext 保存从 MySQL Run 真值重建的进程内 handle；它本身绝不进入 Checkpoint。
type AttemptContext struct {
	Run      RunIdentity
	Identity policy.Identity
	Scope    policy.Scope
	Budget   BudgetHandle
	Lease    workflow.LeaseToken
	Trace    TraceIdentity
	Deadline time.Time
	Snapshot FrozenRuntimeSnapshot
	History  json.RawMessage

	cancel        context.CancelFunc
	physicalCalls *atomic.Uint64
}

// Cancel 释放 Attempt deadline timer，并传播协作式取消。
func (a *AttemptContext) Cancel() {
	if a != nil && a.cancel != nil {
		a.cancel()
	}
}

type attemptContextKey struct{}

// BuildAttemptContext 从 ClaimedRun 的 MySQL 列重建所有安全值并注入 P09 lease。
func BuildAttemptContext(parent context.Context, claimed workflow.ClaimedRun, budgets BudgetHandleFactory) (context.Context, *AttemptContext, error) {
	if parent == nil {
		return nil, nil, fmt.Errorf("parent context is required")
	}
	if isNilInterface(budgets) {
		return nil, nil, fmt.Errorf("budget handle factory is required")
	}
	run := claimed.Run
	if run.RuntimeMode != workflow.RuntimeModeDurableV1 || run.Status != workflow.RunStatusRunning || run.ID == "" || run.Attempt == 0 {
		return nil, nil, workflow.ErrDurablePrimitiveRequired
	}
	if claimed.Token.RunID != run.ID || claimed.Token.Generation != run.LeaseGeneration ||
		run.LeaseOwner == nil || *run.LeaseOwner != claimed.Token.Owner {
		return nil, nil, workflow.ErrLeaseLost
	}
	identityContext, stored, validatedIdentity, err := claimedRunIdentityContext(parent, run)
	if err != nil {
		return nil, nil, err
	}
	if stored.Schema != workflow.DurableContextSnapshotSchema || stored.DeadlineAt.IsZero() ||
		len(stored.History) == 0 || !json.Valid(stored.History) || len(stored.BudgetLimits) == 0 || !json.Valid(stored.BudgetLimits) {
		return nil, nil, fmt.Errorf("durable Context Snapshot contract is incomplete")
	}
	if run.BudgetLimitsJSON == nil || run.BudgetUsageJSON == nil || run.BudgetReservationsJSON == nil {
		return nil, nil, fmt.Errorf("workflow Run is missing durable Budget state")
	}
	budgetState := BudgetState{
		RunID: run.ID, Limits: json.RawMessage(*run.BudgetLimitsJSON),
		Usage: json.RawMessage(*run.BudgetUsageJSON), Reservations: json.RawMessage(*run.BudgetReservationsJSON),
	}
	for name, value := range map[string]json.RawMessage{
		"limits": budgetState.Limits, "usage": budgetState.Usage, "reservations": budgetState.Reservations,
	} {
		if len(value) == 0 || !json.Valid(value) {
			return nil, nil, fmt.Errorf("durable Budget %s JSON is invalid", name)
		}
	}
	if !jsonEqual(budgetState.Limits, stored.BudgetLimits) {
		return nil, nil, fmt.Errorf("context Snapshot and Run Budget limits differ")
	}
	budget, err := budgets.RebuildBudgetHandle(parent, budgetState)
	if err != nil {
		return nil, nil, fmt.Errorf("rebuild Budget handle: %w", err)
	}
	if isNilInterface(budget) {
		return nil, nil, fmt.Errorf("rebuilt Budget handle is nil")
	}
	snapshot, err := RuntimeSnapshotFromRun(run)
	if err != nil {
		return nil, nil, err
	}
	deadline := stored.DeadlineAt.UTC()
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	deadlineContext, cancel := context.WithDeadline(identityContext, deadline)
	leaseContext, err := workflow.ContextWithLeaseToken(deadlineContext, claimed.Token)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	attempt := &AttemptContext{
		Run: RunIdentity{
			ID: run.ID, SessionID: run.SessionID, Attempt: run.Attempt, LeaseGeneration: run.LeaseGeneration,
			RuntimeVersion: *run.RuntimeVersion, RuntimeCompatibilityHash: *run.RuntimeCompatibilityHash,
		},
		Identity: validatedIdentity, Scope: validatedIdentity.Scope, Budget: budget, Lease: claimed.Token,
		Trace: TraceIdentity{ID: uuid.NewString()}, Deadline: deadline, Snapshot: snapshot,
		History: append(json.RawMessage(nil), stored.History...), cancel: cancel, physicalCalls: &atomic.Uint64{},
	}
	leaseContext = budgetctx.WithProvider(leaseContext, newRAGBudgetProvider(attempt))
	collector := evidence.NewCollector(run.ID)
	collector.Scope = evidence.Scope{UserID: validatedIdentity.UserID, Role: string(validatedIdentity.Role), AccessScope: "user:" + validatedIdentity.UserID}
	leaseContext = evidence.WithCollector(leaseContext, collector)
	return context.WithValue(leaseContext, attemptContextKey{}, attempt), attempt, nil
}

// claimedRunIdentityContext 从服务端冻结的 Context Snapshot 重建 Worker 写入所需身份。
// Worker 的进程 context 不携带请求身份，终态、重试和 parked 写入仍必须通过同一 owner/scope 校验。
func claimedRunIdentityContext(parent context.Context, run mysql.WorkflowRun) (context.Context, workflow.DurableContextSnapshot, policy.Identity, error) {
	if parent == nil {
		return nil, workflow.DurableContextSnapshot{}, policy.Identity{}, fmt.Errorf("parent context is required")
	}
	if run.ContextSnapshotJSON == nil {
		return nil, workflow.DurableContextSnapshot{}, policy.Identity{}, fmt.Errorf("workflow Run is missing context_snapshot_json")
	}
	var stored workflow.DurableContextSnapshot
	decoder := json.NewDecoder(strings.NewReader(*run.ContextSnapshotJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return nil, workflow.DurableContextSnapshot{}, policy.Identity{}, fmt.Errorf("decode durable Context Snapshot: %w", err)
	}
	identity := policy.Identity{
		UserID: stored.Identity.UserID, Username: stored.Identity.Username,
		Role: policy.Role(stored.Identity.Role), Scope: stored.Identity.Scope,
		AuthDisabled: stored.Identity.AuthDisabled,
	}
	identityContext := policy.WithIdentity(parent, identity)
	validatedIdentity, err := policy.IdentityFromContext(identityContext)
	if err != nil {
		return nil, workflow.DurableContextSnapshot{}, policy.Identity{}, fmt.Errorf("validate durable Identity Snapshot: %w", err)
	}
	if validatedIdentity.UserID != run.UserID {
		return nil, workflow.DurableContextSnapshot{}, policy.Identity{}, fmt.Errorf("durable Identity Snapshot user does not own Run")
	}
	return identityContext, stored, validatedIdentity, nil
}

// AttemptContextFromContext 读取唯一 typed Runtime Context。
func AttemptContextFromContext(ctx context.Context) (*AttemptContext, error) {
	if ctx == nil {
		return nil, ErrAttemptContextMissing
	}
	attempt, ok := ctx.Value(attemptContextKey{}).(*AttemptContext)
	if !ok || attempt == nil {
		return nil, ErrAttemptContextMissing
	}
	return attempt, nil
}

// IsDurableV1Context 判断调用是否来自已经通过 typed Context builder 的 durable_v1 Attempt。
// legacy 兼容入口用它拒绝任何新 Runtime 调用，不能只依赖调用方自报模式。
func IsDurableV1Context(ctx context.Context) bool {
	attempt, err := AttemptContextFromContext(ctx)
	return err == nil && attempt.Run.ID != ""
}

// SafeSessionValues 校验并复制可交给 Eino WithSessionValues 的严格白名单。
func SafeSessionValues(values map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(values))
	for key, value := range values {
		switch key {
		case planexecute.UserInputSessionKey:
			if _, ok := value.([]adk.Message); !ok {
				return nil, fmt.Errorf("%w: %s must contain official []adk.Message state", ErrUnsafeSessionValue, key)
			}
		case planexecute.PlanSessionKey:
			if plan, ok := value.(planexecute.Plan); !ok || isNilInterface(plan) {
				return nil, fmt.Errorf("%w: %s must contain official planexecute.Plan state", ErrUnsafeSessionValue, key)
			}
		case planexecute.ExecutedStepSessionKey:
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("%w: %s must contain official string state", ErrUnsafeSessionValue, key)
			}
		case planexecute.ExecutedStepsSessionKey:
			if _, ok := value.([]planexecute.ExecutedStep); !ok {
				return nil, fmt.Errorf("%w: %s must contain official []ExecutedStep state", ErrUnsafeSessionValue, key)
			}
		case SessionRunIDKey, SessionRuntimeVersionKey, SessionCompatibilityHashKey:
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("%w: %s must contain a non-empty immutable string", ErrUnsafeSessionValue, key)
			}
			if key == SessionCompatibilityHashKey {
				if err := validateSnapshotHash("Session compatibility hash", text); err != nil {
					return nil, fmt.Errorf("%w: %v", ErrUnsafeSessionValue, err)
				}
			}
		default:
			return nil, fmt.Errorf("%w: key %q is not allowlisted", ErrUnsafeSessionValue, key)
		}
		result[key] = value
	}
	return result, nil
}

// LiteralGenModelInput 是所有后续 durable ChatModelAgent 复用的显式输入函数。
// 它故意不读取 SessionValues，Instruction 中的花括号始终按字面量发送。
func LiteralGenModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	if input == nil {
		return nil, fmt.Errorf("agent input is required")
	}
	messages := make([]adk.Message, 0, len(input.Messages)+1)
	if instruction != "" {
		messages = append(messages, schema.SystemMessage(instruction))
	}
	messages = append(messages, input.Messages...)
	return messages, nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func jsonEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
