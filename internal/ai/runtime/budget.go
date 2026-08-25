package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"SentinelOps/internal/ai/budgetctx"
	"SentinelOps/internal/ai/workflow"
)

const (
	// BudgetCallKindModel 表示一次真实 Model endpoint 调用。
	BudgetCallKindModel BudgetCallKind = "model_call"
	// BudgetCallKindL0Tool 表示一次 L0 Tool endpoint 调用。
	BudgetCallKindL0Tool    BudgetCallKind = "l0_tool_call"
	BudgetCallKindPlanner   BudgetCallKind = "planner"
	BudgetCallKindExecutor  BudgetCallKind = "executor"
	BudgetCallKindReplanner BudgetCallKind = "replanner"
	BudgetCallKindRetry     BudgetCallKind = "retry"
	BudgetCallKindFailover  BudgetCallKind = "failover"
	BudgetCallKindMCP       BudgetCallKind = "mcp"
	BudgetCallKindRAG       BudgetCallKind = "rag"
	BudgetCallKindSkill     BudgetCallKind = "skill"
)

// BudgetCallKind 是 RuntimeHandler 与同一 durable Budget handle 的调用分类。
type BudgetCallKind string

// ModelInvocation 是单次物理模型调用必须显式携带的 provider-qualified 身份。
// ReservationIdentity 由调用构建方稳定生成；Resume/Replay 同一调用必须复用该值。
type ModelInvocation struct {
	ReservationIdentity string
	CatalogRef          string
	Provider            string
	Driver              string
	ModelID             string
	Profile             string
	SnapshotIdentity    string
	PricingRevision     string
	PricingCurrency     string
	PricingUnit         string
	InputPrice          float64
	CachedInputPrice    float64
	OutputPrice         float64
}

// BudgetCall 描述 RuntimeHandler 在 endpoint 前交给 durable handle 的安全事实。
type BudgetCall struct {
	ReservationIdentity string
	Kind                BudgetCallKind
	Subject             string
	Lease               workflow.LeaseToken
	TraceID             string
	Deadline            time.Time
	Model               *ModelInvocation
	Estimate            workflow.BaseBudgetEstimate
}

// BudgetReservation 是 Handler 需要传播到 endpoint metadata 的稳定 identity。
type BudgetReservation struct {
	Identity string
}

// BudgetSettlement 描述 endpoint 返回后的基础结算。
type BudgetSettlement struct {
	ReservationIdentity string
	Lease               workflow.LeaseToken
	TraceID             string
	Succeeded           bool
	Actual              *workflow.BaseBudgetActual
	UsageQuality        string
}

// CallBudget 是 P14 RuntimeHandler 消费的最小 durable Budget 能力。
// 它扩展 P11 handle，不定义第二套 Store 或业务 Service 接口。
type CallBudget interface {
	BudgetHandle
	ReserveCall(context.Context, BudgetCall) (BudgetReservation, error)
	SettleCall(context.Context, BudgetSettlement) error
}

// ControlPlaneBudget 是 Runner 在 Planner/Executor/Replanner attempt 前复用的同一 reservation primitive。
type ControlPlaneBudget interface {
	CallBudget
	ReserveControlPlane(context.Context, BudgetCall) (BudgetReservation, error)
}

// DurableBudget 在现有 workflow.GORMStore 上重建 immutable、并发安全的 Run handle。
type DurableBudget struct {
	store *workflow.GORMStore
}

// NewDurableBudget 创建 P14/P28 唯一 durable Budget factory。
func NewDurableBudget(store *workflow.GORMStore) (*DurableBudget, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow GORMStore is required")
	}
	return &DurableBudget{store: store}, nil
}

// RebuildBudgetHandle 只校验 P11 读出的持久化 JSON，不把额度复制为进程内真值。
func (b *DurableBudget) RebuildBudgetHandle(_ context.Context, state BudgetState) (BudgetHandle, error) {
	if b == nil || b.store == nil {
		return nil, fmt.Errorf("durable Budget Store is required")
	}
	if strings.TrimSpace(state.RunID) == "" {
		return nil, fmt.Errorf("durable Budget Run ID is required")
	}
	for name, value := range map[string]json.RawMessage{
		"limits": state.Limits, "usage": state.Usage, "reservations": state.Reservations,
	} {
		if len(value) == 0 || !json.Valid(value) {
			return nil, fmt.Errorf("durable Budget %s JSON is invalid", name)
		}
	}
	return &durableBudgetHandle{store: b.store, runID: state.RunID}, nil
}

type durableBudgetHandle struct {
	store *workflow.GORMStore
	runID string
}

type ragBudgetProvider struct{ attempt *AttemptContext }
type ragBudgetReservation struct {
	budget            CallBudget
	lease             workflow.LeaseToken
	traceID, identity string
}

func newRAGBudgetProvider(attempt *AttemptContext) budgetctx.Provider {
	return &ragBudgetProvider{attempt: attempt}
}

func (p *ragBudgetProvider) ReserveRAG(ctx context.Context, identity string, documents, contextChars int64) (budgetctx.RAGReservation, error) {
	if p == nil || p.attempt == nil {
		return nil, ErrAttemptContextMissing
	}
	budget, err := requireCallBudget(p.attempt.Budget)
	if err != nil {
		return nil, err
	}
	if _, err := budget.ReserveCall(ctx, BudgetCall{
		ReservationIdentity: identity, Kind: BudgetCallKindRAG, Subject: "documents",
		Lease: p.attempt.Lease, TraceID: p.attempt.Trace.ID, Deadline: p.attempt.Deadline,
		Estimate: workflow.BaseBudgetEstimate{Documents: documents, ContextChars: contextChars},
	}); err != nil {
		return nil, err
	}
	return &ragBudgetReservation{budget: budget, lease: p.attempt.Lease, traceID: p.attempt.Trace.ID, identity: identity}, nil
}

func (r *ragBudgetReservation) Settle(ctx context.Context, documents, contextChars int64, succeeded bool) error {
	return r.budget.SettleCall(ctx, BudgetSettlement{ReservationIdentity: r.identity, Lease: r.lease, TraceID: r.traceID, Succeeded: succeeded, Actual: &workflow.BaseBudgetActual{Documents: documents, ContextChars: contextChars}, UsageQuality: "reliable"})
}

func (*durableBudgetHandle) RuntimeBudgetHandle() {}

// ReserveControlPlane 为官方 planexecute 的控制面 attempt 预留额度，不创建第二个 Store。
func (h *durableBudgetHandle) ReserveControlPlane(ctx context.Context, call BudgetCall) (BudgetReservation, error) {
	if call.Kind != BudgetCallKindPlanner && call.Kind != BudgetCallKindExecutor && call.Kind != BudgetCallKindReplanner {
		return BudgetReservation{}, fmt.Errorf("unsupported control-plane budget kind %q", call.Kind)
	}
	return h.ReserveCall(ctx, call)
}

func (h *durableBudgetHandle) ReserveCall(ctx context.Context, call BudgetCall) (BudgetReservation, error) {
	if h == nil || h.store == nil || call.Lease.RunID != h.runID {
		return BudgetReservation{}, workflow.ErrLeaseLost
	}
	if call.Deadline.IsZero() {
		return BudgetReservation{}, fmt.Errorf("runtime budget deadline is required")
	}
	metadata := workflow.BaseBudgetMetadata{ToolName: call.Subject}
	kind := workflow.BaseBudgetKindL0ToolCall
	if call.Kind == BudgetCallKindModel {
		if call.Model == nil {
			return BudgetReservation{}, fmt.Errorf("model invocation metadata is required")
		}
		kind = workflow.BaseBudgetKindModelCall
		metadata = workflow.BaseBudgetMetadata{
			CatalogRef: call.Model.CatalogRef, Provider: call.Model.Provider, Driver: call.Model.Driver,
			ModelID: call.Model.ModelID, Profile: call.Model.Profile, SnapshotIdentity: call.Model.SnapshotIdentity,
			PricingRevision: call.Model.PricingRevision, PricingCurrency: call.Model.PricingCurrency, PricingUnit: call.Model.PricingUnit,
			InputPrice: call.Model.InputPrice, CachedInputPrice: call.Model.CachedInputPrice, OutputPrice: call.Model.OutputPrice,
		}
	} else if call.Kind != BudgetCallKindL0Tool {
		metadata = workflow.BaseBudgetMetadata{ToolName: call.Subject, Phase: string(call.Kind)}
		switch call.Kind {
		case BudgetCallKindPlanner, BudgetCallKindExecutor, BudgetCallKindReplanner, BudgetCallKindRetry, BudgetCallKindFailover, BudgetCallKindMCP, BudgetCallKindRAG, BudgetCallKindSkill:
			kind = workflow.BaseBudgetKind(call.Kind)
		default:
			return BudgetReservation{}, fmt.Errorf("unsupported Runtime budget call kind %q", call.Kind)
		}
	}
	persistenceContext, cancel := durableBudgetPersistenceContext(ctx)
	defer cancel()
	reservation, err := h.store.ReserveBaseBudget(persistenceContext, workflow.ReserveBaseBudgetInput{
		Lease: call.Lease, Identity: call.ReservationIdentity, Kind: kind, Subject: call.Subject,
		TraceID: call.TraceID, Deadline: call.Deadline, Metadata: metadata, Estimate: call.Estimate,
	})
	if err != nil {
		return BudgetReservation{}, err
	}
	return BudgetReservation{Identity: reservation.Identity}, nil
}

func (h *durableBudgetHandle) SettleCall(ctx context.Context, settlement BudgetSettlement) error {
	if h == nil || h.store == nil || settlement.Lease.RunID != h.runID {
		return workflow.ErrLeaseLost
	}
	outcome := workflow.BaseBudgetOutcomeFailed
	if settlement.Succeeded {
		outcome = workflow.BaseBudgetOutcomeSucceeded
	}
	persistenceContext, cancel := durableBudgetPersistenceContext(ctx)
	defer cancel()
	return h.store.SettleBaseBudget(persistenceContext, workflow.SettleBaseBudgetInput{
		Lease: settlement.Lease, Identity: settlement.ReservationIdentity,
		Outcome: outcome, TraceID: settlement.TraceID, Actual: settlement.Actual, UsageQuality: settlement.UsageQuality,
	})
}

func durableBudgetPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx == nil {
		return context.WithTimeout(base, 5*time.Second)
	}
	base = context.WithoutCancel(ctx)
	return context.WithTimeout(base, 5*time.Second)
}

func requireCallBudget(handle BudgetHandle) (CallBudget, error) {
	budget, ok := handle.(CallBudget)
	if !ok || isNilBudget(budget) {
		return nil, fmt.Errorf("typed Runtime Context Budget does not implement durable call reservation")
	}
	return budget, nil
}

func isNilBudget(value CallBudget) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}
