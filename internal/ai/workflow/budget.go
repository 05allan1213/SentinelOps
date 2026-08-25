package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

const (
	// BaseBudgetSchema 是 P14 初始 Model/L0 Tool 调用预算的持久化版本。
	BaseBudgetSchema = "sentinelops/run-base-budget/v1"

	// BaseBudgetKindModelCall 表示一次真实 Model endpoint 调用。
	BaseBudgetKindModelCall BaseBudgetKind = "model_call"
	// BaseBudgetKindL0ToolCall 表示一次 L0 Tool endpoint 调用。
	BaseBudgetKindL0ToolCall BaseBudgetKind = "l0_tool_call"
	BaseBudgetKindPlanner    BaseBudgetKind = "planner"
	BaseBudgetKindExecutor   BaseBudgetKind = "executor"
	BaseBudgetKindReplanner  BaseBudgetKind = "replanner"
	BaseBudgetKindRetry      BaseBudgetKind = "retry"
	BaseBudgetKindFailover   BaseBudgetKind = "failover"
	BaseBudgetKindMCP        BaseBudgetKind = "mcp"
	BaseBudgetKindRAG        BaseBudgetKind = "rag"
	BaseBudgetKindSkill      BaseBudgetKind = "skill"

	// BaseBudgetReservationPending 表示额度已占用但 endpoint 结果尚未结算。
	BaseBudgetReservationPending BaseBudgetReservationState = "pending"
	// BaseBudgetReservationSettled 表示 endpoint 已返回并完成基础结算。
	BaseBudgetReservationSettled BaseBudgetReservationState = "settled"
	// BaseBudgetReservationExhausted 表示本次 identity 在 endpoint 前被硬预算拒绝。
	BaseBudgetReservationExhausted BaseBudgetReservationState = "exhausted"

	// BaseBudgetOutcomeSucceeded 表示 endpoint 正常返回。
	BaseBudgetOutcomeSucceeded BaseBudgetOutcome = "succeeded"
	// BaseBudgetOutcomeFailed 表示 endpoint 返回错误。
	BaseBudgetOutcomeFailed BaseBudgetOutcome = "failed"

	maxBaseBudgetDurationMS = int64((time.Duration(1<<63 - 1)) / time.Millisecond)
)

var (
	// ErrBaseBudgetExhausted 表示确定性调用次数硬预算已耗尽。
	ErrBaseBudgetExhausted = errors.New("base run budget exhausted")
	// ErrBaseBudgetDeadlineExceeded 表示 Run deadline 或最大持续时间已经耗尽。
	ErrBaseBudgetDeadlineExceeded = errors.New("base run budget deadline exceeded")
	// ErrBudgetReservationConflict 表示相同 reservation identity 被用于不同物理调用。
	ErrBudgetReservationConflict = errors.New("budget reservation identity conflict")
	// ErrBaseBudgetLimitsInvalid 表示 P14 调用需要的持久化 limit 缺失或非法。
	ErrBaseBudgetLimitsInvalid = errors.New("base run budget limits invalid")
	ErrBaseBudgetUsageUnknown  = errors.New("run budget usage is unknown")
)

// BaseBudgetKind 是 P14 唯一支持的基础 reservation 分类。
type BaseBudgetKind string

// BaseBudgetReservationState 是 reservation 的 crash-preserved 状态。
type BaseBudgetReservationState string

// BaseBudgetOutcome 是 endpoint 的基础结算结果；P28 再补 Token/Cost quality。
type BaseBudgetOutcome string

// BaseBudgetLimits 是 P14 从 Run budget_limits_json 消费的最小硬限制。
// P28 只能在同一 JSON 文档中增加字段，不能迁移 identity 或建立第二个 Store。
type BaseBudgetLimits struct {
	MaxModelCalls        int64   `json:"max_model_calls"`
	MaxL0ToolCalls       int64   `json:"max_l0_tool_calls"`
	MaxDurationMS        int64   `json:"max_duration_ms"`
	MaxPlannerRounds     int64   `json:"max_planner_rounds,omitempty"`
	MaxExecutorRounds    int64   `json:"max_executor_rounds,omitempty"`
	MaxReplannerRounds   int64   `json:"max_replanner_rounds,omitempty"`
	MaxRetryCalls        int64   `json:"max_retry_calls,omitempty"`
	MaxFailoverCalls     int64   `json:"max_failover_calls,omitempty"`
	MaxModelOutputTokens int64   `json:"max_model_output_tokens,omitempty"`
	MaxInputTokens       int64   `json:"max_input_tokens,omitempty"`
	MaxOutputTokens      int64   `json:"max_output_tokens,omitempty"`
	MaxMCPCalls          int64   `json:"max_mcp_calls,omitempty"`
	MaxMCPConcurrency    int64   `json:"max_mcp_concurrency,omitempty"`
	MaxMCPResultChars    int64   `json:"max_mcp_result_chars,omitempty"`
	MaxMCPResultBytes    int64   `json:"max_mcp_result_bytes,omitempty"`
	MaxRAGDocuments      int64   `json:"max_rag_documents,omitempty"`
	MaxRAGContextChars   int64   `json:"max_rag_context_chars,omitempty"`
	MaxCostCNY           float64 `json:"max_cost_cny,omitempty"`
}

// BaseBudgetUsage 保存所有 pending/settled reservation 已永久占用的次数。
type BaseBudgetUsage struct {
	Schema            string  `json:"schema"`
	ModelCalls        int64   `json:"model_calls"`
	L0ToolCalls       int64   `json:"l0_tool_calls"`
	PlannerRounds     int64   `json:"planner_rounds,omitempty"`
	ExecutorRounds    int64   `json:"executor_rounds,omitempty"`
	ReplannerRounds   int64   `json:"replanner_rounds,omitempty"`
	RetryCalls        int64   `json:"retry_calls,omitempty"`
	FailoverCalls     int64   `json:"failover_calls,omitempty"`
	MCPCalls          int64   `json:"mcp_calls,omitempty"`
	InputTokens       int64   `json:"input_tokens,omitempty"`
	CachedInputTokens int64   `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64   `json:"output_tokens,omitempty"`
	ReasoningTokens   int64   `json:"reasoning_tokens,omitempty"`
	MCPConcurrency    int64   `json:"mcp_concurrency,omitempty"`
	MCPResultChars    int64   `json:"mcp_result_chars,omitempty"`
	MCPResultBytes    int64   `json:"mcp_result_bytes,omitempty"`
	RAGDocuments      int64   `json:"rag_documents,omitempty"`
	RAGContextChars   int64   `json:"rag_context_chars,omitempty"`
	CostCNY           float64 `json:"cost_cny,omitempty"`
	UsageUnknown      bool    `json:"usage_unknown,omitempty"`
}

// BaseBudgetMetadata 保存 endpoint 前可审计且不含 Secret 的调用身份。
type BaseBudgetMetadata struct {
	CatalogRef       string  `json:"catalog_ref,omitempty"`
	Provider         string  `json:"provider,omitempty"`
	Driver           string  `json:"driver,omitempty"`
	ModelID          string  `json:"model_id,omitempty"`
	Profile          string  `json:"profile,omitempty"`
	SnapshotIdentity string  `json:"snapshot_identity,omitempty"`
	ToolName         string  `json:"tool_name,omitempty"`
	Phase            string  `json:"phase,omitempty"`
	PricingRevision  string  `json:"pricing_revision,omitempty"`
	PricingCurrency  string  `json:"pricing_currency,omitempty"`
	PricingUnit      string  `json:"pricing_unit,omitempty"`
	InputPrice       float64 `json:"input_price,omitempty"`
	CachedInputPrice float64 `json:"cached_input_price,omitempty"`
	OutputPrice      float64 `json:"output_price,omitempty"`
}

// BaseBudgetEstimate 是 endpoint 前的保守资源上界。
type BaseBudgetEstimate struct {
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	ResultChars  int64   `json:"result_chars,omitempty"`
	ResultBytes  int64   `json:"result_bytes,omitempty"`
	Documents    int64   `json:"documents,omitempty"`
	ContextChars int64   `json:"context_chars,omitempty"`
	Concurrency  int64   `json:"concurrency,omitempty"`
	CostCNY      float64 `json:"cost_cny,omitempty"`
}

// BaseBudgetActual 是 settle 时来自可靠 usage 的实际值。
type BaseBudgetActual struct {
	InputTokens       int64   `json:"input_tokens,omitempty"`
	CachedInputTokens int64   `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64   `json:"output_tokens,omitempty"`
	ReasoningTokens   int64   `json:"reasoning_tokens,omitempty"`
	ResultChars       int64   `json:"result_chars,omitempty"`
	ResultBytes       int64   `json:"result_bytes,omitempty"`
	Documents         int64   `json:"documents,omitempty"`
	ContextChars      int64   `json:"context_chars,omitempty"`
	CostCNY           float64 `json:"cost_cny,omitempty"`
}

// BaseBudgetReservation 是同一 Run 内稳定 identity 的 durable 真值。
type BaseBudgetReservation struct {
	Identity           string                     `json:"identity"`
	Kind               BaseBudgetKind             `json:"kind"`
	Subject            string                     `json:"subject"`
	Metadata           BaseBudgetMetadata         `json:"metadata"`
	State              BaseBudgetReservationState `json:"state"`
	Outcome            BaseBudgetOutcome          `json:"outcome,omitempty"`
	ExhaustedReason    string                     `json:"exhausted_reason,omitempty"`
	ReservedAt         time.Time                  `json:"reserved_at"`
	SettledAt          *time.Time                 `json:"settled_at,omitempty"`
	ReservedAttempt    uint                       `json:"reserved_attempt"`
	ReservedGeneration uint64                     `json:"reserved_generation"`
	Estimate           BaseBudgetEstimate         `json:"estimate,omitempty"`
	Actual             *BaseBudgetActual          `json:"actual,omitempty"`
	UsageQuality       string                     `json:"usage_quality,omitempty"`
}

// BaseBudgetReservations 是 budget_reservations_json 的唯一 P14 envelope。
type BaseBudgetReservations struct {
	Schema string                           `json:"schema"`
	Items  map[string]BaseBudgetReservation `json:"items"`
}

// ReserveBaseBudgetInput 描述一次 endpoint 前的 generation-fenced CAS reserve。
type ReserveBaseBudgetInput struct {
	Lease    LeaseToken
	Identity string
	Kind     BaseBudgetKind
	Subject  string
	TraceID  string
	Deadline time.Time
	Metadata BaseBudgetMetadata
	Estimate BaseBudgetEstimate
}

// SettleBaseBudgetInput 描述 endpoint 返回后的基础结算。
type SettleBaseBudgetInput struct {
	Lease        LeaseToken
	Identity     string
	Outcome      BaseBudgetOutcome
	TraceID      string
	Actual       *BaseBudgetActual
	UsageQuality string
}

// ReserveBaseBudget 在唯一 workflow_runs 行锁内执行 deadline、duration 和调用次数硬限制。
// 相同 identity 只读取原 reservation；换租约、Resume 或 Replay 都不会重复放大额度。
func (s *GORMStore) ReserveBaseBudget(ctx context.Context, input ReserveBaseBudgetInput) (BaseBudgetReservation, error) {
	if err := s.authorizeRunScope(ctx, input.Lease.RunID); err != nil {
		return BaseBudgetReservation{}, err
	}
	if err := validateBaseBudgetReserveInput(input); err != nil {
		return BaseBudgetReservation{}, err
	}

	var result BaseBudgetReservation
	var resultErr error
	err := s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		limits, usage, reservations, err := decodeBaseBudgetState(run)
		if err != nil {
			return err
		}
		if usage.UsageUnknown {
			resultErr = ErrBaseBudgetUsageUnknown
			return nil
		}
		databaseNow, err := baseBudgetDatabaseNow(tx)
		if err != nil {
			return err
		}
		temporalReason, err := baseBudgetTemporalExhaustedReason(run, limits, input.Deadline, databaseNow)
		if err != nil {
			return err
		}
		if existing, ok := reservations.Items[input.Identity]; ok {
			if !sameBaseBudgetCall(existing, input) {
				return ErrBudgetReservationConflict
			}
			result = existing
			if existing.State == BaseBudgetReservationExhausted {
				resultErr = exhaustedReasonError(existing.ExhaustedReason)
			} else if temporalReason != "" {
				resultErr = exhaustedReasonError(temporalReason)
			}
			return nil
		}

		exhaustedReason := temporalReason
		if exhaustedReason == "" {
			exhaustedReason = baseBudgetCountExhaustedReason(limits, usage, input.Kind)
		}
		if exhaustedReason == "" {
			exhaustedReason = baseBudgetEstimateExhaustedReason(limits, usage, input)
		}
		if exhaustedReason == "" {
			exhaustedReason = baseBudgetConcurrencyExhaustedReason(limits, reservations, input)
		}
		result = BaseBudgetReservation{
			Identity: input.Identity, Kind: input.Kind, Subject: input.Subject, Metadata: input.Metadata,
			State: BaseBudgetReservationPending, ReservedAt: databaseNow,
			ReservedAttempt: run.Attempt, ReservedGeneration: run.LeaseGeneration,
			Estimate: input.Estimate,
		}
		eventType := EventBudgetReserved
		if exhaustedReason != "" {
			result.State = BaseBudgetReservationExhausted
			result.ExhaustedReason = exhaustedReason
			eventType = EventBudgetExhausted
			resultErr = exhaustedReasonError(exhaustedReason)
		} else {
			switch input.Kind {
			case BaseBudgetKindModelCall:
				usage.ModelCalls++
			case BaseBudgetKindL0ToolCall:
				usage.L0ToolCalls++
			default:
				incrementBudgetKind(&usage, input.Kind)
			}
		}
		reservations.Items[input.Identity] = result
		usageJSON, reservationsJSON, err := encodeBaseBudgetState(usage, reservations)
		if err != nil {
			return err
		}
		event := WorkflowEventInput{
			Type: eventType, TraceID: input.TraceID,
			Payload: EventPayload{Attributes: map[string]any{
				"reservation_identity": input.Identity, "kind": input.Kind, "subject": input.Subject,
				"state": result.State, "reason": result.ExhaustedReason, "metadata": input.Metadata, "estimate": input.Estimate,
			}},
		}
		eventPayload, err := marshalDurableEvent(event)
		if err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, run.Status, input.Lease, map[string]any{
			"budget_usage_json": usageJSON, "budget_reservations_json": reservationsJSON,
		})
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, event, eventPayload)
	})
	if err != nil {
		return BaseBudgetReservation{}, err
	}
	return result, resultErr
}

// SettleBaseBudget 将 pending reservation 标为 settled；额度不会退回。
// P28 在同一 reservation 上增加 Token/Cost settle，不得改变本方法的 identity 语义。
func (s *GORMStore) SettleBaseBudget(ctx context.Context, input SettleBaseBudgetInput) error {
	if err := s.authorizeRunScope(ctx, input.Lease.RunID); err != nil {
		return err
	}
	if strings.TrimSpace(input.Identity) == "" || len(input.Identity) > 256 {
		return fmt.Errorf("budget reservation identity is required")
	}
	if input.Outcome != BaseBudgetOutcomeSucceeded && input.Outcome != BaseBudgetOutcomeFailed {
		return fmt.Errorf("unsupported base budget outcome %q", input.Outcome)
	}

	return s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		_, usage, reservations, err := decodeBaseBudgetState(run)
		if err != nil {
			return err
		}
		reservation, ok := reservations.Items[input.Identity]
		if !ok {
			return fmt.Errorf("reservation %q not found: %w", input.Identity, ErrBudgetReservationConflict)
		}
		if reservation.State == BaseBudgetReservationExhausted {
			return exhaustedReasonError(reservation.ExhaustedReason)
		}
		if reservation.State == BaseBudgetReservationSettled {
			if reservation.Outcome != input.Outcome {
				return ErrBudgetReservationConflict
			}
			return nil
		}
		if reservation.State != BaseBudgetReservationPending {
			return ErrBudgetReservationConflict
		}
		databaseNow, err := baseBudgetDatabaseNow(tx)
		if err != nil {
			return err
		}
		reservation.State = BaseBudgetReservationSettled
		reservation.Outcome = input.Outcome
		reservation.SettledAt = &databaseNow
		if input.Actual != nil {
			actual := sanitizeBudgetActual(*input.Actual)
			reservation.Actual = &actual
			reservation.UsageQuality = input.UsageQuality
			if input.UsageQuality == "unknown" || input.UsageQuality == "" {
				usage.UsageUnknown = true
			} else {
				usage.InputTokens += actual.InputTokens
				usage.CachedInputTokens += actual.CachedInputTokens
				usage.OutputTokens += actual.OutputTokens
				usage.ReasoningTokens += actual.ReasoningTokens
				usage.CostCNY += actual.CostCNY
				usage.MCPResultChars += actual.ResultChars
				usage.MCPResultBytes += actual.ResultBytes
				usage.RAGDocuments += actual.Documents
				usage.RAGContextChars += actual.ContextChars
			}
		}
		if input.UsageQuality == "unknown" {
			usage.UsageUnknown = true
			reservation.UsageQuality = "unknown"
		}
		reservations.Items[input.Identity] = reservation
		usageJSON, reservationsJSON, err := encodeBaseBudgetState(usage, reservations)
		if err != nil {
			return err
		}
		event := WorkflowEventInput{
			Type: EventBudgetSettled, TraceID: input.TraceID,
			Payload: EventPayload{Attributes: map[string]any{
				"reservation_identity": input.Identity, "kind": reservation.Kind,
				"subject": reservation.Subject, "outcome": input.Outcome, "metadata": reservation.Metadata, "actual": input.Actual, "usage_quality": input.UsageQuality,
			}},
		}
		eventPayload, err := marshalDurableEvent(event)
		if err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, run.Status, input.Lease, map[string]any{
			"budget_usage_json": usageJSON, "budget_reservations_json": reservationsJSON,
		})
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, event, eventPayload)
	})
}

func validateBaseBudgetReserveInput(input ReserveBaseBudgetInput) error {
	if err := validateLeaseToken(input.Lease); err != nil {
		return err
	}
	if strings.TrimSpace(input.Identity) == "" || len(input.Identity) > 256 {
		return fmt.Errorf("budget reservation identity is required and must not exceed 256 bytes")
	}
	if strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 256 {
		return fmt.Errorf("budget subject is required and must not exceed 256 bytes")
	}
	redactor := policy.NewRedactor()
	for name, value := range map[string]string{
		"identity": input.Identity, "subject": input.Subject, "catalog_ref": input.Metadata.CatalogRef,
		"provider": input.Metadata.Provider, "driver": input.Metadata.Driver, "model_id": input.Metadata.ModelID,
		"profile": input.Metadata.Profile, "snapshot_identity": input.Metadata.SnapshotIdentity, "tool_name": input.Metadata.ToolName,
		"phase": input.Metadata.Phase, "pricing_revision": input.Metadata.PricingRevision,
		"pricing_currency": input.Metadata.PricingCurrency, "pricing_unit": input.Metadata.PricingUnit,
	} {
		if redactor.RedactText(value) != value {
			return fmt.Errorf("budget %s contains sensitive material", name)
		}
	}
	switch input.Kind {
	case BaseBudgetKindModelCall:
		if input.Metadata.CatalogRef != input.Subject || input.Metadata.Provider == "" || input.Metadata.Driver == "" ||
			input.Metadata.ModelID == "" || input.Metadata.Profile == "" || input.Metadata.SnapshotIdentity == "" || input.Metadata.ToolName != "" {
			return fmt.Errorf("model budget metadata is incomplete or inconsistent")
		}
	case BaseBudgetKindL0ToolCall:
		if input.Metadata.ToolName != input.Subject || input.Metadata.CatalogRef != "" || input.Metadata.Provider != "" ||
			input.Metadata.Driver != "" || input.Metadata.ModelID != "" || input.Metadata.Profile != "" || input.Metadata.SnapshotIdentity != "" {
			return fmt.Errorf("L0 tool budget metadata is incomplete or inconsistent")
		}
	default:
		if !isExtendedBudgetKind(input.Kind) || input.Metadata.Phase == "" {
			return fmt.Errorf("extended budget metadata is incomplete")
		}
	}
	if input.Estimate.InputTokens < 0 || input.Estimate.OutputTokens < 0 || input.Estimate.ResultChars < 0 || input.Estimate.ResultBytes < 0 || input.Estimate.Documents < 0 || input.Estimate.ContextChars < 0 || input.Estimate.Concurrency < 0 || input.Estimate.CostCNY < 0 {
		return fmt.Errorf("budget estimate must be non-negative")
	}
	return nil
}

func isExtendedBudgetKind(kind BaseBudgetKind) bool {
	switch kind {
	case BaseBudgetKindPlanner, BaseBudgetKindExecutor, BaseBudgetKindReplanner, BaseBudgetKindRetry, BaseBudgetKindFailover, BaseBudgetKindMCP, BaseBudgetKindRAG, BaseBudgetKindSkill:
		return true
	default:
		return false
	}
}

func decodeBaseBudgetState(run *mysql.WorkflowRun) (BaseBudgetLimits, BaseBudgetUsage, BaseBudgetReservations, error) {
	var limits BaseBudgetLimits
	if run.BudgetLimitsJSON == nil || json.Unmarshal([]byte(*run.BudgetLimitsJSON), &limits) != nil {
		return limits, BaseBudgetUsage{}, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
	}
	if limits.MaxModelCalls <= 0 || limits.MaxL0ToolCalls <= 0 || limits.MaxDurationMS <= 0 || limits.MaxDurationMS > maxBaseBudgetDurationMS || limits.MaxPlannerRounds < 0 || limits.MaxExecutorRounds < 0 || limits.MaxReplannerRounds < 0 || limits.MaxRetryCalls < 0 || limits.MaxFailoverCalls < 0 || limits.MaxModelOutputTokens < 0 || limits.MaxInputTokens < 0 || limits.MaxOutputTokens < 0 || limits.MaxMCPCalls < 0 || limits.MaxMCPConcurrency < 0 || limits.MaxMCPResultChars < 0 || limits.MaxMCPResultBytes < 0 || limits.MaxRAGDocuments < 0 || limits.MaxRAGContextChars < 0 || limits.MaxCostCNY < 0 {
		return limits, BaseBudgetUsage{}, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
	}
	usage := BaseBudgetUsage{Schema: BaseBudgetSchema}
	if run.BudgetUsageJSON == nil || strings.TrimSpace(*run.BudgetUsageJSON) == "" {
		return limits, usage, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
	}
	if strings.TrimSpace(*run.BudgetUsageJSON) != "{}" {
		if err := json.Unmarshal([]byte(*run.BudgetUsageJSON), &usage); err != nil || usage.Schema != BaseBudgetSchema || usage.ModelCalls < 0 || usage.L0ToolCalls < 0 || usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CostCNY < 0 {
			return limits, BaseBudgetUsage{}, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
		}
	}
	reservations := BaseBudgetReservations{Schema: BaseBudgetSchema, Items: map[string]BaseBudgetReservation{}}
	if run.BudgetReservationsJSON == nil || strings.TrimSpace(*run.BudgetReservationsJSON) == "" {
		return limits, usage, reservations, ErrBaseBudgetLimitsInvalid
	}
	if strings.TrimSpace(*run.BudgetReservationsJSON) != "{}" {
		if err := json.Unmarshal([]byte(*run.BudgetReservationsJSON), &reservations); err != nil || reservations.Schema != BaseBudgetSchema || reservations.Items == nil {
			return limits, usage, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
		}
	}
	if !validBaseBudgetTruth(usage, reservations) {
		return limits, BaseBudgetUsage{}, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
	}
	return limits, usage, reservations, nil
}

func validBaseBudgetTruth(usage BaseBudgetUsage, reservations BaseBudgetReservations) bool {
	var expected BaseBudgetUsage
	for identity, reservation := range reservations.Items {
		if identity == "" || identity != reservation.Identity || len(identity) > 256 || reservation.Subject == "" || reservation.ReservedAt.IsZero() ||
			reservation.ReservedAttempt == 0 || reservation.ReservedGeneration == 0 {
			return false
		}
		input := ReserveBaseBudgetInput{
			Lease:    LeaseToken{RunID: "validation", Owner: "validation", Generation: 1},
			Identity: reservation.Identity, Kind: reservation.Kind, Subject: reservation.Subject, Metadata: reservation.Metadata,
		}
		if validateBaseBudgetReserveInput(input) != nil {
			return false
		}
		switch reservation.State {
		case BaseBudgetReservationPending:
			if reservation.Outcome != "" || reservation.SettledAt != nil || reservation.ExhaustedReason != "" {
				return false
			}
		case BaseBudgetReservationSettled:
			if (reservation.Outcome != BaseBudgetOutcomeSucceeded && reservation.Outcome != BaseBudgetOutcomeFailed) ||
				reservation.SettledAt == nil || reservation.ExhaustedReason != "" {
				return false
			}
		case BaseBudgetReservationExhausted:
			if reservation.Outcome != "" || reservation.SettledAt != nil ||
				(reservation.ExhaustedReason != "deadline" && reservation.ExhaustedReason != "duration" &&
					reservation.ExhaustedReason != "model_calls" && reservation.ExhaustedReason != "l0_tool_calls" && reservation.ExhaustedReason != "mcp_concurrency" && reservation.ExhaustedReason != string(reservation.Kind)) {
				return false
			}
			continue
		default:
			return false
		}
		switch reservation.Kind {
		case BaseBudgetKindModelCall:
			expected.ModelCalls++
		case BaseBudgetKindL0ToolCall:
			expected.L0ToolCalls++
		default:
			incrementBudgetKind(&expected, reservation.Kind)
		}
		if reservation.Actual != nil && reservation.UsageQuality != "unknown" && reservation.UsageQuality != "" {
			actual := sanitizeBudgetActual(*reservation.Actual)
			expected.InputTokens += actual.InputTokens
			expected.CachedInputTokens += actual.CachedInputTokens
			expected.OutputTokens += actual.OutputTokens
			expected.ReasoningTokens += actual.ReasoningTokens
			expected.CostCNY += actual.CostCNY
			expected.MCPResultChars += actual.ResultChars
			expected.MCPResultBytes += actual.ResultBytes
			expected.RAGDocuments += actual.Documents
			expected.RAGContextChars += actual.ContextChars
		}
	}
	return usage.ModelCalls == expected.ModelCalls && usage.L0ToolCalls == expected.L0ToolCalls && usage.PlannerRounds == expected.PlannerRounds && usage.ExecutorRounds == expected.ExecutorRounds && usage.ReplannerRounds == expected.ReplannerRounds && usage.RetryCalls == expected.RetryCalls && usage.FailoverCalls == expected.FailoverCalls && usage.MCPCalls == expected.MCPCalls && usage.InputTokens == expected.InputTokens && usage.CachedInputTokens == expected.CachedInputTokens && usage.OutputTokens == expected.OutputTokens && usage.ReasoningTokens == expected.ReasoningTokens && usage.CostCNY == expected.CostCNY && usage.MCPResultChars == expected.MCPResultChars && usage.MCPResultBytes == expected.MCPResultBytes && usage.RAGDocuments == expected.RAGDocuments && usage.RAGContextChars == expected.RAGContextChars
}

func encodeBaseBudgetState(usage BaseBudgetUsage, reservations BaseBudgetReservations) (string, string, error) {
	usage.Schema = BaseBudgetSchema
	reservations.Schema = BaseBudgetSchema
	if reservations.Items == nil {
		reservations.Items = map[string]BaseBudgetReservation{}
	}
	usageJSON, err := policy.CanonicalJSON(usage)
	if err != nil {
		return "", "", fmt.Errorf("encode base budget usage: %w", err)
	}
	reservationsJSON, err := policy.CanonicalJSON(reservations)
	if err != nil {
		return "", "", fmt.Errorf("encode base budget reservations: %w", err)
	}
	return string(usageJSON), string(reservationsJSON), nil
}

func baseBudgetDatabaseNow(tx *gorm.DB) (time.Time, error) {
	var databaseNow time.Time
	if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&databaseNow).Error; err != nil {
		return time.Time{}, fmt.Errorf("读取 MySQL budget 时间: %w", err)
	}
	return databaseNow.UTC(), nil
}

func baseBudgetTemporalExhaustedReason(run *mysql.WorkflowRun, limits BaseBudgetLimits, attemptDeadline, databaseNow time.Time) (string, error) {
	if run.ContextSnapshotJSON == nil {
		return "", ErrBaseBudgetLimitsInvalid
	}
	var snapshot DurableContextSnapshot
	if err := json.Unmarshal([]byte(*run.ContextSnapshotJSON), &snapshot); err != nil || snapshot.Schema != DurableContextSnapshotSchema || snapshot.DeadlineAt.IsZero() {
		return "", ErrBaseBudgetLimitsInvalid
	}
	deadline := snapshot.DeadlineAt.UTC()
	if !attemptDeadline.IsZero() && attemptDeadline.Before(deadline) {
		deadline = attemptDeadline.UTC()
	}
	if !databaseNow.Before(deadline) {
		return "deadline", nil
	}
	if run.StartedAt.IsZero() || databaseNow.Sub(run.StartedAt.UTC()) >= time.Duration(limits.MaxDurationMS)*time.Millisecond {
		return "duration", nil
	}
	return "", nil
}

func baseBudgetCountExhaustedReason(limits BaseBudgetLimits, usage BaseBudgetUsage, kind BaseBudgetKind) string {
	switch kind {
	case BaseBudgetKindModelCall:
		if usage.ModelCalls >= limits.MaxModelCalls {
			return "model_calls"
		}
	case BaseBudgetKindL0ToolCall:
		if usage.L0ToolCalls >= limits.MaxL0ToolCalls {
			return "l0_tool_calls"
		}
	default:
		used, limit := budgetKindUsageLimit(limits, usage, kind)
		if limit > 0 && used >= limit {
			return string(kind)
		}
	}
	if limits.MaxInputTokens > 0 && usage.InputTokens >= limits.MaxInputTokens {
		return "input_tokens"
	}
	if limits.MaxOutputTokens > 0 && usage.OutputTokens >= limits.MaxOutputTokens {
		return "output_tokens"
	}
	if limits.MaxCostCNY > 0 && usage.CostCNY >= limits.MaxCostCNY {
		return "cost"
	}
	if limits.MaxMCPResultChars > 0 && usage.MCPResultChars >= limits.MaxMCPResultChars {
		return "mcp_result_chars"
	}
	if limits.MaxMCPResultBytes > 0 && usage.MCPResultBytes >= limits.MaxMCPResultBytes {
		return "mcp_result_bytes"
	}
	if limits.MaxRAGDocuments > 0 && usage.RAGDocuments >= limits.MaxRAGDocuments {
		return "rag_documents"
	}
	if limits.MaxRAGContextChars > 0 && usage.RAGContextChars >= limits.MaxRAGContextChars {
		return "rag_context_chars"
	}
	return ""
}

func baseBudgetEstimateExhaustedReason(limits BaseBudgetLimits, usage BaseBudgetUsage, input ReserveBaseBudgetInput) string {
	if limits.MaxModelOutputTokens > 0 && input.Estimate.OutputTokens > limits.MaxModelOutputTokens {
		return "model_output_tokens"
	}
	if limits.MaxInputTokens > 0 && usage.InputTokens+input.Estimate.InputTokens > limits.MaxInputTokens {
		return "input_tokens"
	}
	if limits.MaxOutputTokens > 0 && usage.OutputTokens+input.Estimate.OutputTokens > limits.MaxOutputTokens {
		return "output_tokens"
	}
	if limits.MaxCostCNY > 0 && usage.CostCNY+input.Estimate.CostCNY > limits.MaxCostCNY {
		return "cost"
	}
	if limits.MaxMCPResultChars > 0 && usage.MCPResultChars+input.Estimate.ResultChars > limits.MaxMCPResultChars {
		return "mcp_result_chars"
	}
	if limits.MaxMCPResultBytes > 0 && usage.MCPResultBytes+input.Estimate.ResultBytes > limits.MaxMCPResultBytes {
		return "mcp_result_bytes"
	}
	if limits.MaxRAGDocuments > 0 && usage.RAGDocuments+input.Estimate.Documents > limits.MaxRAGDocuments {
		return "rag_documents"
	}
	if limits.MaxRAGContextChars > 0 && usage.RAGContextChars+input.Estimate.ContextChars > limits.MaxRAGContextChars {
		return "rag_context_chars"
	}
	return ""
}

func baseBudgetConcurrencyExhaustedReason(limits BaseBudgetLimits, reservations BaseBudgetReservations, input ReserveBaseBudgetInput) string {
	if limits.MaxMCPConcurrency <= 0 || input.Kind != BaseBudgetKindMCP {
		return ""
	}
	var active int64
	for _, reservation := range reservations.Items {
		if reservation.Kind == BaseBudgetKindMCP && reservation.State == BaseBudgetReservationPending {
			active += reservation.Estimate.Concurrency
		}
	}
	if active+input.Estimate.Concurrency > limits.MaxMCPConcurrency {
		return "mcp_concurrency"
	}
	return ""
}

func budgetKindUsageLimit(limits BaseBudgetLimits, usage BaseBudgetUsage, kind BaseBudgetKind) (int64, int64) {
	switch kind {
	case BaseBudgetKindPlanner:
		return usage.PlannerRounds, limits.MaxPlannerRounds
	case BaseBudgetKindExecutor:
		return usage.ExecutorRounds, limits.MaxExecutorRounds
	case BaseBudgetKindReplanner:
		return usage.ReplannerRounds, limits.MaxReplannerRounds
	case BaseBudgetKindRetry:
		return usage.RetryCalls, limits.MaxRetryCalls
	case BaseBudgetKindFailover:
		return usage.FailoverCalls, limits.MaxFailoverCalls
	case BaseBudgetKindMCP:
		return usage.MCPCalls, limits.MaxMCPCalls
	}
	return 0, 0
}

func incrementBudgetKind(usage *BaseBudgetUsage, kind BaseBudgetKind) {
	switch kind {
	case BaseBudgetKindPlanner:
		usage.PlannerRounds++
	case BaseBudgetKindExecutor:
		usage.ExecutorRounds++
	case BaseBudgetKindReplanner:
		usage.ReplannerRounds++
	case BaseBudgetKindRetry:
		usage.RetryCalls++
	case BaseBudgetKindFailover:
		usage.FailoverCalls++
	case BaseBudgetKindMCP:
		usage.MCPCalls++
	}
}

func sanitizeBudgetActual(actual BaseBudgetActual) BaseBudgetActual {
	if actual.InputTokens < 0 {
		actual.InputTokens = 0
	}
	if actual.CachedInputTokens < 0 {
		actual.CachedInputTokens = 0
	}
	if actual.CachedInputTokens > actual.InputTokens {
		actual.CachedInputTokens = actual.InputTokens
	}
	if actual.OutputTokens < 0 {
		actual.OutputTokens = 0
	}
	if actual.ReasoningTokens < 0 {
		actual.ReasoningTokens = 0
	}
	if actual.ReasoningTokens > actual.OutputTokens {
		actual.ReasoningTokens = actual.OutputTokens
	}
	if actual.ResultChars < 0 {
		actual.ResultChars = 0
	}
	if actual.ResultBytes < 0 {
		actual.ResultBytes = 0
	}
	if actual.Documents < 0 {
		actual.Documents = 0
	}
	if actual.ContextChars < 0 {
		actual.ContextChars = 0
	}
	if actual.CostCNY < 0 {
		actual.CostCNY = 0
	}
	return actual
}

func sameBaseBudgetCall(existing BaseBudgetReservation, input ReserveBaseBudgetInput) bool {
	return existing.Identity == input.Identity && existing.Kind == input.Kind && existing.Subject == input.Subject && existing.Metadata == input.Metadata && existing.Estimate == input.Estimate
}

func exhaustedReasonError(reason string) error {
	switch reason {
	case "deadline", "duration":
		return fmt.Errorf("%s: %w", reason, ErrBaseBudgetDeadlineExceeded)
	default:
		return fmt.Errorf("%s: %w", reason, ErrBaseBudgetExhausted)
	}
}
