package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/google/uuid"
)

const (
	// DurableAgentPlan 是 P26 后所有新 durable Run 使用的官方 Plan/Executor/Replanner 入口。
	DurableAgentPlan         = "plan_agent"
	defaultDurableRunTimeout = 15 * time.Minute
)

// CreateDurableRunRequest 是 API 创建 durable Run 的客户端无身份请求。
// Identity、Runtime Snapshot、Budget 和 Run ID 全部由服务端固化。
type CreateDurableRunRequest struct {
	SessionID string
	Query     string
	Agent     string
}

// DurableServiceConfig 描述 P20 API 所需的唯一 Store primitive。
// CreateRun/ListEvents 只用于单元测试替换具体 GORMStore 方法，不是第二套 Store。
type DurableServiceConfig struct {
	Store          *workflow.GORMStore
	AcceptNewRuns  bool
	Snapshot       runtime.FrozenRuntimeSnapshot
	SnapshotLoader func(context.Context) (runtime.FrozenRuntimeSnapshot, error)
	RunTimeout     time.Duration
	CreateRun      func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error)
	ListEvents     func(context.Context, string, int64) ([]workflow.StreamEvent, error)
}

// DurableService 是 API 侧的单点入口：只创建 Run 或读取 Event，不持有 Agent 执行。
type DurableService struct {
	snapshotLoader func(context.Context) (runtime.FrozenRuntimeSnapshot, error)
	runTimeout     time.Duration
	createRun      func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error)
	listEvents     func(context.Context, string, int64) ([]workflow.StreamEvent, error)
}

// NewDurableService 创建只拥有 API 读写 primitive 的服务。
func NewDurableService(config DurableServiceConfig) (*DurableService, error) {
	if config.CreateRun == nil && config.Store == nil {
		return nil, fmt.Errorf("durable workflow Store is required")
	}
	if config.ListEvents == nil && config.Store == nil {
		return nil, fmt.Errorf("durable event Store is required")
	}
	if config.RunTimeout <= 0 {
		config.RunTimeout = defaultDurableRunTimeout
	}
	service := &DurableService{
		snapshotLoader: config.SnapshotLoader,
		runTimeout:     config.RunTimeout,
		createRun:      config.CreateRun,
		listEvents:     config.ListEvents,
	}
	if service.snapshotLoader == nil {
		service.snapshotLoader = func(context.Context) (runtime.FrozenRuntimeSnapshot, error) {
			if !config.AcceptNewRuns {
				return runtime.FrozenRuntimeSnapshot{}, ErrDurableRunGateClosed
			}
			return config.Snapshot, nil
		}
	}
	if service.createRun == nil {
		service.createRun = config.Store.CreateRunWithSessionLock
	}
	if service.listEvents == nil {
		service.listEvents = config.Store.ListEventsAfter
	}
	return service, nil
}

// CreateRun 将一次 API 请求转换为不可变 durable Run，并且只调用一次 P08 primitive。
func (s *DurableService) CreateRun(ctx context.Context, request CreateDurableRunRequest) (*mysql.WorkflowRun, error) {
	if s == nil || s.createRun == nil {
		return nil, fmt.Errorf("durable API service is not initialized")
	}
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.Query = strings.TrimSpace(request.Query)
	if request.SessionID == "" || request.Query == "" {
		return nil, fmt.Errorf("session_id and query are required")
	}
	if request.Agent == "" {
		request.Agent = DurableAgentPlan
	}
	if request.Agent != DurableAgentPlan {
		return nil, fmt.Errorf("durable Agent %q is not enabled", request.Agent)
	}
	immutableInput, err := json.Marshal(map[string]any{
		"agent": request.Agent, "query": request.Query,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal immutable durable input: %w", err)
	}
	// P14 requires the three base hard limits on every durable Run.  Keep the
	// API default bounded to the same fifteen-minute lifetime as the Run
	// deadline; token limits are enforced by the model adapter, not by the
	// durable base-budget contract.
	budgetLimits := json.RawMessage(`{"max_model_calls":64,"max_l0_tool_calls":32,"max_duration_ms":900000}`)
	snapshot, err := s.snapshotLoader(ctx)
	if err != nil {
		return nil, err
	}
	if !snapshot.FeatureGate(runtime.GateAgentRuntimeEnabled) || !snapshot.FeatureGate(runtime.GateAgentRuntimeAcceptNewRuns) {
		return nil, ErrDurableRunGateClosed
	}
	input := workflow.CreateRunInput{
		ID:                 uuid.NewString(),
		WorkflowKey:        "chat.intent",
		SessionID:          request.SessionID,
		QueryText:          request.Query,
		ImmutableInputJSON: immutableInput,
		RuntimeSnapshot:    snapshot.WorkflowFields(),
		BudgetLimitsJSON:   budgetLimits,
		DeadlineAt:         time.Now().Add(s.runTimeout),
		CreatedEvent: workflow.WorkflowEventInput{
			Type: workflow.EventRunCreated,
			Payload: workflow.EventPayload{Attributes: map[string]any{
				"agent": request.Agent, "session_id": request.SessionID,
			}},
		},
		StartedAt: time.Now(),
	}
	run, err := s.createRun(ctx, input)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// Events 只读取 workflow_events；afterSeq 是持久化 seq 游标，不会启动 Runner。
func (s *DurableService) Events(ctx context.Context, runID string, afterSeq int64) ([]workflow.StreamEvent, error) {
	if s == nil || s.listEvents == nil {
		return nil, fmt.Errorf("durable event service is not initialized")
	}
	if strings.TrimSpace(runID) == "" || afterSeq < 0 {
		return nil, fmt.Errorf("run_id and non-negative after_seq are required")
	}
	return s.listEvents(ctx, runID, afterSeq)
}

// ErrDurableRunGateClosed 表示生产 accept-new-runs Gate 尚未开启。
var ErrDurableRunGateClosed = errors.New("durable Run accept-new-runs gate is closed")
