package workflow

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TransitionIntentDefault              = "default"
	TransitionIntentApprovalDecided      = "approval_decided"
	TransitionIntentRetryReady           = "retry_ready"
	TransitionIntentRuntimeRestored      = "runtime_restored"
	TransitionIntentEffectReconciliation = "effect_reconciliation"
	TransitionIntentEffectResolved       = "effect_resolved"
	TransitionIntentEffectStillUnknown   = "effect_still_unknown"
)

const (
	ParkReasonRuntimeIncompatible = "runtime_incompatible"
	ParkReasonEffectUnknown       = "effect_unknown"
	ParkReasonCheckpointMissing   = "checkpoint_missing"
	ParkReasonCheckpointCorrupt   = "checkpoint_corrupt"
)

var (
	// ErrSessionRunActive 稳定表示同一 Session 已存在非终态 Run，API 后续映射为 409。
	ErrSessionRunActive = errors.New("session run active")
	// ErrRunCASConflict 表示期望旧状态不再成立，调用方已经失去本次状态转换权。
	ErrRunCASConflict = errors.New("workflow run compare-and-swap conflict")
	// ErrRunTransitionDenied 表示状态边或显式 transition intent 不符合冻结矩阵。
	ErrRunTransitionDenied = errors.New("workflow run transition denied")
	// ErrDurablePrimitiveRequired 表示 legacy 写入口被用于 durable_v1 Run。
	ErrDurablePrimitiveRequired = errors.New("durable workflow primitive required")
)

// CreateRunInput 是首次 durable Run 原子创建所需的不可变输入。
type CreateRunInput struct {
	ID                       string
	WorkflowKey              string
	SessionID                string
	ParentRunID              string
	QueryText                string
	ImmutableInputJSON       json.RawMessage
	RuntimeVersion           string
	RuntimeCompatibilityHash string
	CreatedEvent             WorkflowEventInput
	StartedAt                time.Time
}

// RunTransition 描述一次带 Event 的显式 Run CAS。
type RunTransition struct {
	RunID          string
	ExpectedStatus string
	TargetStatus   string
	Intent         string
	ParkReason     string
	Lease          LeaseToken
	Event          WorkflowEventInput
}

// CompleteRunInput 描述唯一 durable 完成 primitive 的调用输入。
type CompleteRunInput struct {
	RunID             string
	ExpectedStatus    string
	TargetStatus      string
	Lease             LeaseToken
	OutputPayload     string
	ErrorMessage      string
	TraceQuality      string
	TraceID           string
	RevisionStateJSON json.RawMessage
}

// CreateRunWithSessionLock 原子建立 Revision 0、冻结 Snapshot、占用 Session 并写 run.created。
func (s *GORMStore) CreateRunWithSessionLock(ctx context.Context, input CreateRunInput) (*mysql.WorkflowRun, error) {
	userID, err := authorizeDurableCreate(ctx, input)
	if err != nil {
		return nil, err
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	if input.CreatedEvent.Type == "" {
		input.CreatedEvent.Type = EventRunCreated
	}
	if input.CreatedEvent.Type != EventRunCreated {
		return nil, fmt.Errorf("created event must be %s", EventRunCreated)
	}
	eventPayload, err := marshalDurableEvent(input.CreatedEvent)
	if err != nil {
		return nil, err
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now()
	}

	var lastTransactionErr error
	for attempt := 0; attempt < 3; attempt++ {
		run, txErr := s.createRunWithSessionLockOnce(ctx, userID, input, eventPayload)
		if txErr == nil {
			return run, nil
		}
		if errors.Is(txErr, ErrSessionRunActive) {
			return nil, txErr
		}
		if !isMySQLRetryableTransactionError(txErr) {
			return nil, txErr
		}
		lastTransactionErr = txErr
	}
	return nil, fmt.Errorf("创建 durable Run 重试耗尽: %w", lastTransactionErr)
}

func (s *GORMStore) createRunWithSessionLockOnce(ctx context.Context, userID string, input CreateRunInput, eventPayload string) (*mysql.WorkflowRun, error) {
	var created *mysql.WorkflowRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := bootstrapRevisionZero(ctx, tx, input.SessionID, userID); err != nil {
			return err
		}

		var latest mysql.SessionStateRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ?", input.SessionID).
			Order("revision DESC").First(&latest).Error; err != nil {
			return fmt.Errorf("读取最新 Session Revision: %w", err)
		}
		activeSessionKey := input.SessionID
		immutableInput := string(input.ImmutableInputJSON)
		contextSnapshot := latest.StateJSON
		sessionRevision := latest.Revision
		runtimeVersion := input.RuntimeVersion
		runtimeCompatibilityHash := input.RuntimeCompatibilityHash
		created = &mysql.WorkflowRun{
			ID:                       input.ID,
			WorkflowKey:              input.WorkflowKey,
			UserID:                   userID,
			SessionID:                input.SessionID,
			ParentRunID:              input.ParentRunID,
			ActiveSessionKey:         &activeSessionKey,
			Status:                   RunStatusPending,
			RuntimeMode:              RuntimeModeDurableV1,
			ImmutableInputJSON:       &immutableInput,
			QueryText:                input.QueryText,
			ContextSnapshotJSON:      &contextSnapshot,
			SessionRevision:          &sessionRevision,
			RuntimeVersion:           &runtimeVersion,
			RuntimeCompatibilityHash: &runtimeCompatibilityHash,
			LastEventSeq:             1,
			StartedAt:                input.StartedAt,
		}
		if err := tx.Create(created).Error; err != nil {
			if isActiveSessionConflict(err) {
				return fmt.Errorf("%w: session_id=%s", ErrSessionRunActive, input.SessionID)
			}
			return fmt.Errorf("创建 durable workflow Run: %w", err)
		}
		if err := insertDurableEvent(tx, created.ID, 1, input.CreatedEvent, eventPayload); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// TransitionRunWithEvent 在同一事务中提交状态 CAS、数据库 seq 和 Event。
func (s *GORMStore) TransitionRunWithEvent(ctx context.Context, transition RunTransition) error {
	if err := s.authorizeRunScope(ctx, transition.RunID); err != nil {
		return err
	}
	if transition.Lease.RunID != transition.RunID {
		return ErrLeaseLost
	}
	if !RunOccupiesSession(transition.TargetStatus) {
		return fmt.Errorf("terminal transition %q: %w", transition.TargetStatus, ErrDurablePrimitiveRequired)
	}
	if transition.Intent == "" {
		transition.Intent = TransitionIntentDefault
	}
	eventPayload, err := marshalDurableEvent(transition.Event)
	if err != nil {
		return err
	}

	return s.withFencedRunTransaction(ctx, transition.Lease, transition.ExpectedStatus, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		parkReason := transition.ParkReason
		if parkReason == "" && run.ParkReason != nil {
			parkReason = *run.ParkReason
		}
		if err := ValidateRunTransition(run.Status, transition.TargetStatus, transition.Intent, parkReason); err != nil {
			return err
		}

		updates := map[string]any{"status": transition.TargetStatus}
		switch {
		case transition.TargetStatus == RunStatusParked:
			updates["park_reason"] = parkReason
		case transition.TargetStatus == RunStatusPending && run.Status == RunStatusParked:
			updates["park_reason"] = nil
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, run.Status, transition.Lease, updates)
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, transition.Event, eventPayload)
	})
}

// CompleteRunAndCommitSession 是 durable Run 写终态和释放 Session 的唯一 primitive。
func (s *GORMStore) CompleteRunAndCommitSession(ctx context.Context, input CompleteRunInput) error {
	if err := s.authorizeRunScope(ctx, input.RunID); err != nil {
		return err
	}
	if input.Lease.RunID != input.RunID {
		return ErrLeaseLost
	}
	if input.TargetStatus != RunStatusSucceeded && input.TargetStatus != RunStatusFailed && input.TargetStatus != RunStatusCanceled {
		return fmt.Errorf("completion target %q: %w", input.TargetStatus, ErrRunTransitionDenied)
	}
	if input.TargetStatus == RunStatusSucceeded {
		if len(input.RevisionStateJSON) == 0 || !json.Valid(input.RevisionStateJSON) {
			return fmt.Errorf("successful completion requires valid Revision JSON")
		}
	} else if len(input.RevisionStateJSON) != 0 {
		return fmt.Errorf("failed or canceled completion must not include a successful Revision")
	}
	if input.TraceQuality == "" {
		input.TraceQuality = "unknown"
	}
	outputPayload, err := redactPersistentPayload(input.OutputPayload)
	if err != nil {
		return err
	}
	errorMessage := policy.NewRedactor().RedactText(input.ErrorMessage)

	return s.withFencedRunTransaction(ctx, input.Lease, input.ExpectedStatus, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		parkReason := ""
		if run.ParkReason != nil {
			parkReason = *run.ParkReason
		}
		if err := ValidateRunTransition(run.Status, input.TargetStatus, TransitionIntentDefault, parkReason); err != nil {
			return err
		}

		if input.TargetStatus == RunStatusSucceeded {
			if err := insertNextSessionRevision(tx, run, input.RevisionStateJSON); err != nil {
				return err
			}
		}
		eventType := EventRunFailed
		if input.TargetStatus == RunStatusSucceeded {
			eventType = EventRunCompleted
		}
		event := WorkflowEventInput{
			Type:    eventType,
			TraceID: input.TraceID,
			Payload: EventPayload{Attributes: map[string]any{
				"from_status": run.Status,
				"to_status":   input.TargetStatus,
			}},
		}
		eventPayload, err := marshalDurableEvent(event)
		if err != nil {
			return err
		}
		now := time.Now()
		updates := map[string]any{
			"status":             input.TargetStatus,
			"active_session_key": nil,
			"finished_at":        &now,
			"duration_ms":        max(now.Sub(run.StartedAt).Milliseconds(), 0),
			"output_payload":     outputPayload,
			"error_message":      errorMessage,
			"trace_quality":      input.TraceQuality,
			"park_reason":        nil,
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, run.Status, input.Lease, updates)
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, event, eventPayload)
	})
}

// ValidateRunTransition 校验完整 durable 状态矩阵和 parked 的显式解锁意图。
func ValidateRunTransition(from, to, intent, parkReason string) error {
	if intent == "" {
		intent = TransitionIntentDefault
	}
	if !isDurableRunStatus(from) || !isDurableRunStatus(to) {
		return fmt.Errorf("%w: unknown status %q -> %q", ErrRunTransitionDenied, from, to)
	}
	if !RunOccupiesSession(from) {
		return fmt.Errorf("%w: terminal status %q cannot transition", ErrRunTransitionDenied, from)
	}
	if to == RunStatusFailed || to == RunStatusCanceled {
		if intent == TransitionIntentDefault {
			return nil
		}
		return fmt.Errorf("%w: terminal transition requires default intent", ErrRunTransitionDenied)
	}

	allowed := false
	switch {
	case from == RunStatusPending && to == RunStatusRunning:
		allowed = intent == TransitionIntentDefault
	case from == RunStatusRunning && to == RunStatusWaitingApproval:
		allowed = intent == TransitionIntentDefault
	case from == RunStatusRunning && to == RunStatusRetryableFailed:
		allowed = intent == TransitionIntentDefault
	case from == RunStatusWaitingApproval && to == RunStatusPending:
		allowed = intent == TransitionIntentApprovalDecided
	case from == RunStatusRetryableFailed && to == RunStatusPending:
		allowed = intent == TransitionIntentRetryReady
	case (from == RunStatusRunning || from == RunStatusWaitingApproval || from == RunStatusRetryableFailed) && to == RunStatusParked:
		allowed = intent == TransitionIntentDefault && strings.TrimSpace(parkReason) != ""
	case from == RunStatusParked && to == RunStatusPending:
		allowed = intent == TransitionIntentRuntimeRestored && parkReason == ParkReasonRuntimeIncompatible
	case from == RunStatusParked && to == RunStatusReconciling:
		allowed = intent == TransitionIntentEffectReconciliation && parkReason == ParkReasonEffectUnknown
	case from == RunStatusReconciling && to == RunStatusPending:
		allowed = intent == TransitionIntentEffectResolved && parkReason == ParkReasonEffectUnknown
	case from == RunStatusReconciling && to == RunStatusParked:
		allowed = intent == TransitionIntentEffectStillUnknown && parkReason == ParkReasonEffectUnknown
	case from == RunStatusRunning && to == RunStatusSucceeded:
		allowed = intent == TransitionIntentDefault
	}
	if !allowed {
		return fmt.Errorf("%w: %s -> %s intent=%s park_reason=%s", ErrRunTransitionDenied, from, to, intent, parkReason)
	}
	return nil
}

func authorizeDurableCreate(ctx context.Context, input CreateRunInput) (string, error) {
	if err := policy.Authorize(ctx, policy.PermissionCreateReadOnlyRun, policy.Resource{}); err != nil {
		return "", err
	}
	userID, err := policy.UserID(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(input.WorkflowKey) == "" || strings.TrimSpace(input.SessionID) == "" {
		return "", fmt.Errorf("workflow key and session id are required")
	}
	if len(input.SessionID) > 64 || len(input.ID) > 64 || len(input.ParentRunID) > 64 {
		return "", fmt.Errorf("durable Run identifier exceeds schema limit")
	}
	if len(input.ImmutableInputJSON) == 0 || !json.Valid(input.ImmutableInputJSON) {
		return "", fmt.Errorf("valid immutable input JSON is required")
	}
	if strings.TrimSpace(input.RuntimeVersion) == "" || len(input.RuntimeVersion) > 128 {
		return "", fmt.Errorf("runtime version is required")
	}
	if len(input.RuntimeCompatibilityHash) != 64 {
		return "", fmt.Errorf("runtime compatibility hash must be 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(input.RuntimeCompatibilityHash); err != nil {
		return "", fmt.Errorf("runtime compatibility hash is not hexadecimal: %w", err)
	}
	return userID, nil
}

func insertDurableEvent(tx *gorm.DB, runID string, seq uint64, input WorkflowEventInput, payload string) error {
	event := mysql.WorkflowEvent{
		RunID:          runID,
		Seq:            seq,
		EventType:      input.Type,
		Payload:        payload,
		PayloadVersion: EventPayloadVersion,
		TraceID:        input.TraceID,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("插入 durable workflow Event: %w", err)
	}
	return nil
}

func insertNextSessionRevision(tx *gorm.DB, run *mysql.WorkflowRun, stateJSON json.RawMessage) error {
	if run.SessionRevision == nil || strings.TrimSpace(run.SessionID) == "" {
		return fmt.Errorf("durable Run is missing frozen Session Revision")
	}
	var latest uint64
	if err := tx.Model(&mysql.SessionStateRevision{}).
		Where("session_id = ?", run.SessionID).
		Order("revision DESC").Limit(1).Pluck("revision", &latest).Error; err != nil {
		return fmt.Errorf("读取完成前 Session Revision: %w", err)
	}
	if latest != *run.SessionRevision {
		return fmt.Errorf("session revision changed: frozen=%d latest=%d: %w", *run.SessionRevision, latest, ErrRunCASConflict)
	}
	next := mysql.SessionStateRevision{
		SessionID: run.SessionID,
		Revision:  latest + 1,
		StateJSON: string(stateJSON),
	}
	if err := tx.Create(&next).Error; err != nil {
		return fmt.Errorf("插入完成 Session Revision: %w", err)
	}
	return nil
}

func isDurableRunStatus(status string) bool {
	switch status {
	case RunStatusPending, RunStatusRunning, RunStatusWaitingApproval, RunStatusRetryableFailed,
		RunStatusParked, RunStatusReconciling, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
		return true
	default:
		return false
	}
}

func isActiveSessionConflict(err error) bool {
	var mysqlErr *driver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && strings.Contains(mysqlErr.Message, "uidx_workflow_runs_active_session")
}

func isMySQLRetryableTransactionError(err error) bool {
	var mysqlErr *driver.MySQLError
	return errors.As(err, &mysqlErr) && (mysqlErr.Number == 1205 || mysqlErr.Number == 1213)
}
