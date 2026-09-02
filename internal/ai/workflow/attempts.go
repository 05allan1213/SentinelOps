package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FinishAttemptInput describes the fenced projection close written together
// with the Run/Event truth.  Empty optional fields remain NULL/unknown.
type FinishAttemptInput struct {
	Lease          LeaseToken
	Status         string
	CurrentPhase   string
	FailureCode    string
	FailureMessage string
	UsageQuality   string
	TraceQuality   string
	FinishedAt     time.Time
	RetryCount     *uint
	FailoverCount  *uint
	OperationID    string
}

// AttemptFingerprintDomain separates the deterministic Attempt identity from
// Run, Checkpoint and Worker fingerprints.
const AttemptFingerprintDomain = "sentinelops/runtime-attempt/v1"

// AttemptFingerprint derives a stable, secret-free Attempt identity from the
// Run ID, Attempt number and fenced lease generation.  It never reuses
// executing_worker_fingerprint, which records the physical Worker snapshot.
func AttemptFingerprint(runID string, attempt uint, generation uint64) string {
	payload := AttemptFingerprintDomain + "\x00" + runID + "\x00" +
		strconv.FormatUint(uint64(attempt), 10) + "\x00" +
		strconv.FormatUint(generation, 10)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

// BeginAttemptTx inserts the current Run attempt.  It is intentionally a
// transaction-local primitive; callers that also mutate Run/Event truth pass
// the same *gorm.DB transaction through the unexported helper below.
func (s *GORMStore) BeginAttemptTx(ctx context.Context, token LeaseToken, workerID string) error {
	if err := validateLeaseToken(token); err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, token, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		return beginAttemptTx(tx, run, token, workerID, "")
	})
}

func beginAttemptTx(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, workerID, executingWorkerFingerprint string) error {
	if run == nil || run.ID != token.RunID || run.Attempt == 0 || run.LeaseGeneration != token.Generation {
		return ErrLeaseLost
	}
	mode, status, phase := "fresh", RunStatusRunning, "unknown"
	worker := strings.TrimSpace(workerID)
	if worker == "" && run.LeaseOwner != nil {
		worker = *run.LeaseOwner
	}
	var runtimeVersion, runtimeHash, workerValue, executingFingerprint *string
	if run.RuntimeVersion != nil {
		value := *run.RuntimeVersion
		runtimeVersion = &value
	}
	if run.RuntimeCompatibilityHash != nil {
		value := *run.RuntimeCompatibilityHash
		runtimeHash = &value
	}
	if worker != "" {
		workerValue = &worker
	}
	if fingerprint := strings.TrimSpace(executingWorkerFingerprint); fingerprint != "" {
		executingFingerprint = &fingerprint
	}
	generation := token.Generation
	now := time.Now().UTC()
	row := mysql.WorkflowAttempt{
		ID: uuid.NewString(), RunID: run.ID, Attempt: run.Attempt,
		Mode: &mode, Status: &status, CurrentPhase: &phase, WorkerID: workerValue,
		LeaseGeneration: &generation, RuntimeVersion: runtimeVersion,
		RunCompatibilityHash:       runtimeHash,
		ExecutingWorkerFingerprint: executingFingerprint,
		UsageQuality:               stringPtr("unknown"), TraceQuality: stringPtr("unknown"),
		StartedAt: &now, CreatedAt: &now, UpdatedAt: &now,
	}
	// A duplicate attempt is only acceptable when all fenced identity facts
	// match.  This keeps transaction retries idempotent without overwriting a
	// newer generation.
	var existing mysql.WorkflowAttempt
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND attempt = ?", run.ID, run.Attempt).First(&existing)
	if lookup.Error == nil {
		if existing.LeaseGeneration == nil || *existing.LeaseGeneration != token.Generation {
			return ErrLeaseLost
		}
		return nil
	}
	if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return fmt.Errorf("lock workflow attempt: %w", lookup.Error)
	}
	if err := tx.Create(&row).Error; err != nil {
		return fmt.Errorf("begin workflow attempt: %w", err)
	}
	return nil
}

// SetAttemptRecoveryModeTx updates mode and checkpoint fingerprint under the
// current fenced generation.  A missing checkpoint fingerprint stays NULL.
func (s *GORMStore) SetAttemptRecoveryModeTx(ctx context.Context, token LeaseToken, mode RecoveryMode, checkpointFingerprint string) error {
	if err := validateLeaseToken(token); err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, token, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		return setAttemptRecoveryModeTx(tx, run, token, mode, checkpointFingerprint)
	})
}

func setAttemptRecoveryModeTx(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, mode RecoveryMode, checkpointFingerprint string) error {
	if run == nil || run.LeaseGeneration != token.Generation {
		return ErrLeaseLost
	}
	updates := map[string]any{"mode": string(mode), "current_phase": "recovering", "updated_at": gorm.Expr("CURRENT_TIMESTAMP(3)")}
	if strings.TrimSpace(checkpointFingerprint) != "" {
		updates["checkpoint_compatibility_hash"] = checkpointFingerprint
	}
	return updateAttemptTx(tx, run, token, updates)
}

func attachAttemptTraceTx(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, traceID string) error {
	if strings.TrimSpace(traceID) == "" {
		return nil
	}
	if len(traceID) > 64 || policy.NewRedactor().RedactText(traceID) != traceID {
		return fmt.Errorf("attempt trace id is invalid")
	}
	if run == nil || run.LeaseGeneration != token.Generation || run.Attempt == 0 {
		return ErrLeaseLost
	}
	var existing mysql.WorkflowAttempt
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("run_id = ? AND attempt = ? AND lease_generation = ? AND finished_at IS NULL", run.ID, run.Attempt, token.Generation).
		First(&existing)
	if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return ErrLeaseLost
	}
	if lookup.Error != nil {
		return fmt.Errorf("lock workflow attempt before attaching trace: %w", lookup.Error)
	}
	if existing.TraceID != nil {
		if *existing.TraceID == traceID {
			return nil
		}
		return fmt.Errorf("%w: attempt trace identity is immutable", ErrRunCASConflict)
	}
	result := tx.Model(&mysql.WorkflowAttempt{}).
		Where("id = ? AND lease_generation = ? AND finished_at IS NULL AND trace_id IS NULL", existing.ID, token.Generation).
		Updates(map[string]any{"trace_id": traceID, "updated_at": gorm.Expr("CURRENT_TIMESTAMP(3)")})
	if result.Error != nil {
		return fmt.Errorf("attach workflow attempt trace: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// AttachAttemptTraceTx records the trace only after tracing has successfully
// started.  It never invents a worker fingerprint or physical model identity.
func (s *GORMStore) AttachAttemptTraceTx(ctx context.Context, token LeaseToken, traceID string) error {
	if err := validateLeaseToken(token); err != nil {
		return err
	}
	if strings.TrimSpace(traceID) == "" || len(traceID) > 64 {
		return fmt.Errorf("attempt trace id is required and must be at most 64 bytes")
	}
	return s.withFencedRunTransaction(ctx, token, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if policy.NewRedactor().RedactText(traceID) != traceID {
			return fmt.Errorf("attempt trace id contains sensitive material")
		}
		return attachAttemptTraceTx(tx, run, token, traceID)
	})
}

// AttachAttemptTrace is the public fenced alias used by Runtime after the
// trace backend accepts StartAttempt.
func (s *GORMStore) AttachAttemptTrace(ctx context.Context, token LeaseToken, traceID string) error {
	return s.AttachAttemptTraceTx(ctx, token, traceID)
}

func updateAttemptTx(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, updates map[string]any) error {
	if run == nil || run.LeaseGeneration != token.Generation || run.Attempt == 0 {
		return ErrLeaseLost
	}
	result := tx.Model(&mysql.WorkflowAttempt{}).
		Where("run_id = ? AND attempt = ? AND lease_generation = ? AND finished_at IS NULL", run.ID, run.Attempt, token.Generation).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update workflow attempt: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// FinishAttemptTx closes the current Attempt.  It is fenced by Run ID,
// attempt and lease generation, so stale workers cannot alter projection truth.
func (s *GORMStore) FinishAttemptTx(ctx context.Context, input FinishAttemptInput) error {
	if err := validateLeaseToken(input.Lease); err != nil {
		return err
	}
	if input.Status == "" {
		return fmt.Errorf("attempt status is required")
	}
	return s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		return finishAttemptTx(tx, run, input)
	})
}

func finishAttemptTx(tx *gorm.DB, run *mysql.WorkflowRun, input FinishAttemptInput) error {
	if run == nil || run.LeaseGeneration != input.Lease.Generation {
		return ErrLeaseLost
	}
	if run.Attempt == 0 {
		return nil
	}
	var existing mysql.WorkflowAttempt
	lookup := tx.Where("run_id = ? AND attempt = ?", run.ID, run.Attempt).First(&existing)
	if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		// Some legacy/unit transition fixtures move a Run without a Worker claim;
		// there is no Attempt to close in that path.
		return nil
	}
	if lookup.Error != nil {
		return fmt.Errorf("read workflow attempt before finish: %w", lookup.Error)
	}
	if existing.FinishedAt != nil {
		// Recovery/reconciliation may terminalize a Run after its execution
		// Attempt was already parked.  Preserve the first terminal projection;
		// never rewrite a finished Attempt from a later generation/path.
		return nil
	}
	updates := map[string]any{
		"status": input.Status, "current_phase": input.CurrentPhase,
		"finished_at": input.FinishedAt.UTC(), "updated_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
	}
	if input.RetryCount != nil {
		updates["retry_count"] = *input.RetryCount
	}
	if input.FailoverCount != nil {
		updates["failover_count"] = *input.FailoverCount
	}
	if input.FinishedAt.IsZero() {
		updates["finished_at"] = gorm.Expr("CURRENT_TIMESTAMP(3)")
	}
	if input.FailureCode != "" {
		updates["failure_code"] = input.FailureCode
	}
	if input.FailureMessage != "" {
		updates["failure_message_redacted"] = policy.NewRedactor().RedactText(input.FailureMessage)
	}
	if input.UsageQuality != "" {
		updates["usage_quality"] = input.UsageQuality
	}
	if input.TraceQuality != "" {
		updates["trace_quality"] = input.TraceQuality
	}
	if input.OperationID != "" {
		updates["operation_id"] = input.OperationID
	}
	return updateAttemptTx(tx, run, input.Lease, updates)
}

// finishAttemptAfterLeaseExpiryTx 在持有 Run 行锁时关闭租约已过期的开放 Attempt，
// 写入始终以调用方观察到的精确 generation 作为条件。
func finishAttemptAfterLeaseExpiryTx(tx *gorm.DB, run *mysql.WorkflowRun, generation uint64, status, phase, failureCode, failureMessage string) error {
	if run == nil || run.LeaseGeneration != generation {
		return ErrLeaseLost
	}
	if run.Attempt == 0 {
		return nil
	}
	var existing mysql.WorkflowAttempt
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND attempt = ?", run.ID, run.Attempt).First(&existing)
	if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return nil
	}
	if lookup.Error != nil {
		return fmt.Errorf("lock expired workflow attempt: %w", lookup.Error)
	}
	if existing.LeaseGeneration == nil || *existing.LeaseGeneration != generation {
		return ErrLeaseLost
	}
	if existing.FinishedAt != nil {
		return nil
	}
	updates := map[string]any{
		"status": status, "current_phase": phase,
		"failure_code": failureCode, "usage_quality": "unknown", "trace_quality": "unknown",
		"finished_at": gorm.Expr("CURRENT_TIMESTAMP(3)"), "updated_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
	}
	if strings.TrimSpace(failureMessage) != "" {
		updates["failure_message_redacted"] = policy.NewRedactor().RedactText(failureMessage)
	}
	result := tx.Model(&mysql.WorkflowAttempt{}).
		Where("id = ? AND lease_generation = ? AND finished_at IS NULL", existing.ID, generation).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("close expired workflow attempt: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// recordAttemptReliabilityFactTx 只记录已成功预留的 retry/failover 可靠性事实。
func recordAttemptReliabilityFactTx(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, kind BaseBudgetKind) error {
	field := ""
	switch kind {
	case BaseBudgetKindRetry:
		field = "retry_count"
	case BaseBudgetKindFailover:
		field = "failover_count"
	default:
		return nil
	}
	if run == nil || run.Attempt == 0 || run.LeaseGeneration != token.Generation {
		return ErrLeaseLost
	}
	result := tx.Model(&mysql.WorkflowAttempt{}).
		Where("run_id = ? AND attempt = ? AND lease_generation = ? AND finished_at IS NULL", run.ID, run.Attempt, token.Generation).
		Update(field, gorm.Expr("COALESCE("+field+", 0) + 1"))
	if result.Error != nil {
		return fmt.Errorf("record Attempt reliability fact: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// RebuildAttemptsFromEvents deterministically reconstructs an Attempt read
// projection from ordered canonical Events.  Missing facts remain NULL and
// lower data quality to partial; no worker/trace/fingerprint is fabricated.
func (s *GORMStore) RebuildAttemptsFromEvents(ctx context.Context, runID string) ([]mysql.WorkflowAttempt, string, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, "partial", fmt.Errorf("run id is required")
	}
	var events []mysql.WorkflowEvent
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Order("seq ASC").Find(&events).Error; err != nil {
		return nil, "partial", fmt.Errorf("read workflow events: %w", err)
	}
	rows := map[uint]*mysql.WorkflowAttempt{}
	quality := "reconstructed"
	for _, event := range events {
		attrs := eventAttributes(event.Payload)
		attemptNumber, ok := eventUint(attrs, "attempt")
		if !ok || attemptNumber == 0 {
			quality = "partial"
			continue
		}
		row := rows[uint(attemptNumber)]
		if row == nil {
			row = &mysql.WorkflowAttempt{RunID: runID, Attempt: uint(attemptNumber), Mode: stringPtr("fresh"), Status: stringPtr(RunStatusRunning), CurrentPhase: stringPtr("unknown"), UsageQuality: stringPtr("unknown"), TraceQuality: stringPtr("unknown")}
			rows[uint(attemptNumber)] = row
		}
		if generation, ok := eventUint64(attrs, "generation", "lease_generation"); ok {
			row.LeaseGeneration = &generation
		} else {
			quality = "partial"
		}
		if event.TraceID != "" {
			trace := event.TraceID
			row.TraceID = &trace
		}
		if worker, ok := attrs["owner"].(string); ok && worker != "" {
			row.WorkerID = &worker
		}
		switch event.EventType {
		case EventRunResumed:
			mode := string(RecoveryModeResume)
			row.Mode = &mode
		case EventRunReplayed:
			mode := string(RecoveryModeReplay)
			row.Mode = &mode
		case EventRunCompleted:
			status := RunStatusSucceeded
			row.Status = &status
			phase := attemptPhaseForStatus(status)
			row.CurrentPhase = &phase
		case EventRunFailed:
			status := RunStatusFailed
			if target, ok := attrs["to_status"].(string); ok && target == RunStatusCanceled {
				status = RunStatusCanceled
			}
			if retryable, ok := attrs["retryable"].(bool); ok && retryable {
				status = RunStatusRetryableFailed
			}
			row.Status = &status
			phase := attemptPhaseForStatus(status)
			row.CurrentPhase = &phase
		case EventApprovalRequested, EventApprovalPreparing:
			status := RunStatusWaitingApproval
			phase := "waiting_approval"
			row.Status, row.CurrentPhase = &status, &phase
		case EventBudgetReserved:
			if kind, ok := attrs["kind"].(string); ok {
				switch BaseBudgetKind(kind) {
				case BaseBudgetKindRetry:
					count := uint(1)
					if row.RetryCount != nil {
						count = *row.RetryCount + 1
					}
					row.RetryCount = &count
				case BaseBudgetKindFailover:
					count := uint(1)
					if row.FailoverCount != nil {
						count = *row.FailoverCount + 1
					}
					row.FailoverCount = &count
				}
			}
		case EventRunParked:
			status := RunStatusParked
			row.Status = &status
			phase := "unknown"
			row.CurrentPhase = &phase
		}
	}
	keys := make([]int, 0, len(rows))
	for attempt := range rows {
		keys = append(keys, int(attempt))
	}
	sort.Ints(keys)
	result := make([]mysql.WorkflowAttempt, 0, len(keys))
	for _, key := range keys {
		if row := rows[uint(key)]; row != nil {
			if row.LeaseGeneration == nil || row.WorkerID == nil || row.TraceID == nil {
				quality = "partial"
			}
			result = append(result, *row)
		}
	}
	if len(result) == 0 && len(events) > 0 {
		quality = "partial"
	}
	return result, quality, nil
}

func eventAttributes(payload string) map[string]any {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal([]byte(payload), &envelope) != nil || envelope.Data == nil {
		return nil
	}
	return envelope.Data
}
func eventUint(values map[string]any, key string) (uint, bool) {
	value, ok := values[key].(float64)
	return uint(value), ok && value > 0 && value == float64(uint(value))
}
func eventUint64(values map[string]any, keys ...string) (uint64, bool) {
	for _, key := range keys {
		if value, ok := values[key].(float64); ok && value > 0 && value == float64(uint64(value)) {
			return uint64(value), true
		}
	}
	return 0, false
}

func attemptPhaseForStatus(status string) string {
	switch status {
	case RunStatusSucceeded:
		return "completed"
	case RunStatusFailed, RunStatusCanceled, RunStatusRetryableFailed:
		return "failed"
	case RunStatusWaitingApproval:
		return "waiting_approval"
	case RunStatusReconciling:
		return "reconciling"
	default:
		return "unknown"
	}
}
func stringPtr(value string) *string { return &value }
