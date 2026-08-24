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
	MaxModelCalls  int64 `json:"max_model_calls"`
	MaxL0ToolCalls int64 `json:"max_l0_tool_calls"`
	MaxDurationMS  int64 `json:"max_duration_ms"`
}

// BaseBudgetUsage 保存所有 pending/settled reservation 已永久占用的次数。
type BaseBudgetUsage struct {
	Schema      string `json:"schema"`
	ModelCalls  int64  `json:"model_calls"`
	L0ToolCalls int64  `json:"l0_tool_calls"`
}

// BaseBudgetMetadata 保存 endpoint 前可审计且不含 Secret 的调用身份。
type BaseBudgetMetadata struct {
	CatalogRef       string `json:"catalog_ref,omitempty"`
	Provider         string `json:"provider,omitempty"`
	Driver           string `json:"driver,omitempty"`
	ModelID          string `json:"model_id,omitempty"`
	Profile          string `json:"profile,omitempty"`
	SnapshotIdentity string `json:"snapshot_identity,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
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
}

// SettleBaseBudgetInput 描述 endpoint 返回后的基础结算。
type SettleBaseBudgetInput struct {
	Lease    LeaseToken
	Identity string
	Outcome  BaseBudgetOutcome
	TraceID  string
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
		result = BaseBudgetReservation{
			Identity: input.Identity, Kind: input.Kind, Subject: input.Subject, Metadata: input.Metadata,
			State: BaseBudgetReservationPending, ReservedAt: databaseNow,
			ReservedAttempt: run.Attempt, ReservedGeneration: run.LeaseGeneration,
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
				"state": result.State, "reason": result.ExhaustedReason, "metadata": input.Metadata,
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
		reservations.Items[input.Identity] = reservation
		usageJSON, reservationsJSON, err := encodeBaseBudgetState(usage, reservations)
		if err != nil {
			return err
		}
		event := WorkflowEventInput{
			Type: EventBudgetSettled, TraceID: input.TraceID,
			Payload: EventPayload{Attributes: map[string]any{
				"reservation_identity": input.Identity, "kind": reservation.Kind,
				"subject": reservation.Subject, "outcome": input.Outcome, "metadata": reservation.Metadata,
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
		return fmt.Errorf("unsupported base budget kind %q", input.Kind)
	}
	return nil
}

func decodeBaseBudgetState(run *mysql.WorkflowRun) (BaseBudgetLimits, BaseBudgetUsage, BaseBudgetReservations, error) {
	var limits BaseBudgetLimits
	if run.BudgetLimitsJSON == nil || json.Unmarshal([]byte(*run.BudgetLimitsJSON), &limits) != nil {
		return limits, BaseBudgetUsage{}, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
	}
	if limits.MaxModelCalls <= 0 || limits.MaxL0ToolCalls <= 0 || limits.MaxDurationMS <= 0 || limits.MaxDurationMS > maxBaseBudgetDurationMS {
		return limits, BaseBudgetUsage{}, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
	}
	usage := BaseBudgetUsage{Schema: BaseBudgetSchema}
	if run.BudgetUsageJSON == nil || strings.TrimSpace(*run.BudgetUsageJSON) == "" {
		return limits, usage, BaseBudgetReservations{}, ErrBaseBudgetLimitsInvalid
	}
	if strings.TrimSpace(*run.BudgetUsageJSON) != "{}" {
		if err := json.Unmarshal([]byte(*run.BudgetUsageJSON), &usage); err != nil || usage.Schema != BaseBudgetSchema || usage.ModelCalls < 0 || usage.L0ToolCalls < 0 {
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
	var modelCalls, l0ToolCalls int64
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
					reservation.ExhaustedReason != "model_calls" && reservation.ExhaustedReason != "l0_tool_calls") {
				return false
			}
			continue
		default:
			return false
		}
		switch reservation.Kind {
		case BaseBudgetKindModelCall:
			modelCalls++
		case BaseBudgetKindL0ToolCall:
			l0ToolCalls++
		}
	}
	return usage.ModelCalls == modelCalls && usage.L0ToolCalls == l0ToolCalls
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
	}
	return ""
}

func sameBaseBudgetCall(existing BaseBudgetReservation, input ReserveBaseBudgetInput) bool {
	return existing.Identity == input.Identity && existing.Kind == input.Kind && existing.Subject == input.Subject && existing.Metadata == input.Metadata
}

func exhaustedReasonError(reason string) error {
	switch reason {
	case "deadline", "duration":
		return fmt.Errorf("%s: %w", reason, ErrBaseBudgetDeadlineExceeded)
	default:
		return fmt.Errorf("%s: %w", reason, ErrBaseBudgetExhausted)
	}
}
