package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const OperationIdentityDomain = "sentinelops/runtime-operation/v1"

const (
	OperationActionResume  = "resume"
	OperationActionReplay  = "replay"
	OperationActionCancel  = "cancel"
	OperationActionRestore = "restore"

	OperationStatusAccepted  = "accepted"
	OperationStatusRunning   = "running"
	OperationStatusSucceeded = "succeeded"
	OperationStatusFailed    = "failed"
	OperationStatusCanceled  = "canceled"
	OperationStatusRejected  = "rejected"
)

var (
	ErrInvalidOperationInput        = errors.New("invalid operation input")
	ErrInvalidOperationTransition   = errors.New("invalid operation event transition")
	ErrOperationConflict            = errors.New("workflow recovery operation conflict")
	ErrOperationIdempotencyConflict = errors.New("workflow recovery operation idempotency conflict")
	ErrOperationPrecondition        = errors.New("workflow recovery operation precondition failed")
	ErrCancelRequested              = errors.New("workflow run cancellation requested")
	compatibilityHashPattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	operationIDPattern              = regexp.MustCompile(`^op-[0-9a-f]{64}$`)
	operationReasonCodePattern      = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)
)

// CancelRequested reports the persisted cancellation flag for the current
// fenced lease. It is read by the existing Worker heartbeat, which then
// triggers the active Eino AgentCancelFunc safe-point path.
func (s *GORMStore) CancelRequested(ctx context.Context, token LeaseToken) (bool, error) {
	if err := validateLeaseToken(token); err != nil {
		return false, err
	}
	var run mysql.WorkflowRun
	if err := applyDurableRuntimeContract(s.db.WithContext(ctx)).Where("id = ?", token.RunID).First(&run).Error; err != nil {
		return false, err
	}
	if run.LeaseGeneration != token.Generation || run.LeaseOwner == nil || *run.LeaseOwner != token.Owner {
		return false, ErrLeaseLost
	}
	return run.CancelRequestedAt != nil, nil
}

// OperationRequestInput 是幂等冲突判断使用的精确请求材料；Reason 必须先校验原文。
type OperationRequestInput struct {
	RunID                     string
	Action                    string
	Reason                    string
	ExpectedGeneration        uint64
	ExpectedCompatibilityHash string
}

// Operation 从关联的 workflow Event 重建，不引入第二持久化模型或命令表。
type Operation struct {
	OperationID               string
	RunID                     string
	Action                    string
	Status                    string
	Terminal                  bool
	IdempotencyKeyDigest      string
	RequestFingerprint        string
	ActorID                   string
	Reason                    string
	AcceptedAt                time.Time
	StartedAt                 *time.Time
	FinishedAt                *time.Time
	AcceptedSeq               uint64
	StartedSeq                uint64
	CorrelationSeq            uint64
	ExpectedGeneration        uint64
	ExpectedCompatibilityHash string
	ExecutionGeneration       uint64
	ErrorCode                 string
	ResultReason              string
}

// OperationRecord 是命令接纳结果；IdempotentReplay 仅表示同 key、同 fingerprint
// 返回了已有 Operation。终态 cancel 不写命令 Event，而是返回 Run 的既有终态。
type OperationRecord struct {
	Operation
	IdempotentReplay bool
}

// RecoveryOperationClaim 是 Worker 消费 command Event 的最小快照。
// Lease 可以复用仍有效的 Run lease；只有接管过期/无 lease 的 Run 才递增 generation。
type RecoveryOperationClaim struct {
	Operation Operation
	Run       mysql.WorkflowRun
	Lease     LeaseToken
}

// ClaimNextRecoveryOperation 在 Run 行锁内选择一个 accepted 或可 reclaim 的
// started operation。首次 accepted claim 才会创建恢复 Attempt；started
// reclaim 只接管同一个 Attempt，绝不递增 Attempt 第二次。
func (s *GORMStore) ClaimNextRecoveryOperation(ctx context.Context, owner string, leaseDuration time.Duration) (*RecoveryOperationClaim, bool, error) {
	if err := validateLeaseOwner(owner); err != nil {
		return nil, false, err
	}
	if leaseDuration < time.Millisecond || leaseDuration > maxLeaseDuration {
		return nil, false, fmt.Errorf("lease duration must be between 1ms and %s", maxLeaseDuration)
	}
	var claimed *RecoveryOperationClaim
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		query := applyDurableRuntimeContract(tx.Model(&mysql.WorkflowRun{})).
			Where("runtime_mode = ?", RuntimeModeDurableV1).
			Where("status NOT IN ?", []string{RunStatusSucceeded, RunStatusSuccess, RunStatusFailed, RunStatusCanceled}).
			Where("EXISTS (SELECT 1 FROM workflow_events e WHERE e.run_id = workflow_runs.id AND e.event_type = ? AND e.operation_id IS NOT NULL)", EventOperationAccepted).
			Order("priority DESC, available_at ASC, id ASC").
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Limit(1)
		if err := query.First(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("锁定 Recovery Run: %w", err)
		}
		ops, err := listActiveOperationsForRunTx(tx, run.ID)
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			return nil
		}
		op := ops[0]
		var now time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&now).Error; err != nil {
			return err
		}
		leaseValid := run.LeaseOwner != nil && run.LeaseUntil != nil && run.LeaseUntil.After(now)
		if leaseValid && *run.LeaseOwner != owner {
			return nil
		}
		if op.Status != OperationStatusAccepted && op.Status != OperationStatusRunning {
			return nil
		}
		firstClaim := op.Status == OperationStatusAccepted
		acceptedGeneration := run.LeaseGeneration
		if firstClaim {
			if (op.Action == OperationActionResume || op.Action == OperationActionReplay) && run.Attempt >= run.MaxAttempts {
				return rejectAcceptedOperationTx(tx, &run, op, "attempt_limit_exhausted")
			}
			facts := RecoveryFacts{}
			if op.Action != OperationActionCancel {
				facts, err = loadRecoveryFacts(tx, &run)
				if err != nil {
					return err
				}
			}
			if err := validateRecoveryOperationTx(tx, &run, facts, OperationRequestInput{
				RunID: run.ID, Action: op.Action, Reason: op.Reason,
				ExpectedGeneration: op.ExpectedGeneration, ExpectedCompatibilityHash: op.ExpectedCompatibilityHash,
			}); err != nil {
				if errors.Is(err, ErrOperationPrecondition) {
					return rejectAcceptedOperationTx(tx, &run, op, "recovery_precondition_failed")
				}
				return fmt.Errorf("revalidate accepted recovery operation: %w", err)
			}
		} else if op.Action != OperationActionCancel {
			facts, factsErr := loadRecoveryFacts(tx, &run)
			if factsErr != nil {
				return factsErr
			}
			if err := validateRecoveryOperationDependenciesTx(tx, &run, facts, op.Action, op.ExpectedCompatibilityHash); err != nil {
				if errors.Is(err, ErrOperationPrecondition) {
					correlation, correlationErr := latestPriorRunEventSeqTx(tx, run.ID)
					if correlationErr != nil {
						return correlationErr
					}
					return insertOperationTerminalEventTx(tx, run.ID, op.OperationID, op.Action,
						EventOperationFailed, correlation, "recovery_precondition_failed", "recovery_precondition_failed")
				}
				return fmt.Errorf("revalidate started recovery operation: %w", err)
			}
		}

		generation := acceptedGeneration
		updates := map[string]any{
			"lease_owner":  owner,
			"lease_until":  gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL ? MICROSECOND)", leaseDuration.Microseconds()),
			"heartbeat_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		}
		if op.Action != OperationActionRestore && run.Status != RunStatusRunning {
			updates["status"] = RunStatusRunning
		}
		// A recovery command fences any prior execution.  It gets exactly one
		// new generation and Attempt on its first claim.  Reclaim only bumps the
		// generation when the previous lease expired, preserving the Attempt row.
		if firstClaim && (op.Action == OperationActionResume || op.Action == OperationActionReplay) {
			if run.Status == RunStatusRunning && run.Attempt > 0 {
				if err := finishAttemptAfterLeaseExpiryTx(tx, &run, run.LeaseGeneration, RunStatusFailed, "failed", "recovery_superseded", "recovery operation superseded the previous execution"); err != nil {
					return err
				}
			}
			updates["attempt"] = gorm.Expr("attempt + 1")
		}
		if !leaseValid || (firstClaim && (op.Action == OperationActionResume || op.Action == OperationActionReplay)) {
			updates["lease_generation"] = gorm.Expr("lease_generation + 1")
			generation++
		}
		if result := tx.Model(&mysql.WorkflowRun{}).Where("id = ? AND lease_generation = ?", run.ID, run.LeaseGeneration).Updates(updates); result.Error != nil {
			return fmt.Errorf("取得 Recovery lease: %w", result.Error)
		} else if result.RowsAffected != 1 {
			return ErrLeaseLost
		}
		if err := tx.Where("id = ?", run.ID).First(&run).Error; err != nil {
			return err
		}
		claimed = &RecoveryOperationClaim{Operation: op, Run: run, Lease: LeaseToken{RunID: run.ID, Owner: owner, Generation: generation}}
		claimed.Operation.RunID = run.ID
		claimed.Operation.OperationID = op.OperationID
		claimed.Operation.Action = op.Action
		if firstClaim {
			startedAt := time.Now().UTC()
			claimed.Operation.Status = OperationStatusRunning
			claimed.Operation.StartedSeq = run.LastEventSeq + 1
			claimed.Operation.StartedAt = &startedAt
			claimed.Operation.ExecutionGeneration = generation
		}
		if firstClaim && (op.Action == OperationActionResume || op.Action == OperationActionReplay) {
			if err := beginAttemptTx(tx, &run, claimed.Lease, owner); err != nil {
				return err
			}
			if err := setAttemptOperationTx(tx, &run, claimed.Lease, op.OperationID); err != nil {
				return err
			}
		} else if !firstClaim && (op.Action == OperationActionResume || op.Action == OperationActionReplay) {
			if err := reclaimAttemptTx(tx, &run, claimed.Lease, owner, op.OperationID, !leaseValid); err != nil {
				return err
			}
		} else if !firstClaim && op.Action == OperationActionCancel && run.Attempt > 0 {
			// The lease reaper preserves an open Attempt for a started cancel
			// operation. Rebind it to the reclaimed generation before the
			// completion primitive closes it.
			if err := reclaimAttemptTx(tx, &run, claimed.Lease, owner, op.OperationID, !leaseValid); err != nil {
				return err
			}
		} else if op.Action == OperationActionCancel && run.Attempt > 0 {
			// Running cancel has no new Attempt, but its operation identity is
			// attached to the open current Attempt when one exists.
			if err := setAttemptOperationTx(tx, &run, claimed.Lease, op.OperationID); err != nil && !errors.Is(err, ErrLeaseLost) {
				return err
			}
		}
		// Claim and operation.started are one transaction. This closes the
		// crash window where a process could have created an Attempt but left an
		// accepted command to be claimed a second time.
		if err := startRecoveryOperationTx(tx, &run, *claimed); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return claimed, claimed != nil, nil
}

func setAttemptOperationTx(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, operationID string) error {
	if strings.TrimSpace(operationID) == "" || run == nil || run.Attempt == 0 {
		return nil
	}
	result := tx.Model(&mysql.WorkflowAttempt{}).
		Where("run_id = ? AND attempt = ? AND lease_generation = ? AND finished_at IS NULL", run.ID, run.Attempt, token.Generation).
		Update("operation_id", operationID)
	if result.Error != nil {
		return fmt.Errorf("attach recovery operation to Attempt: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// reclaimAttemptTx rebinds an unfinished recovery Attempt to a newly fenced
// worker generation.  The projection remains one Attempt for one operation;
// a new trace may be attached after a process crash.
func reclaimAttemptTx(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, workerID, operationID string, generationChanged bool) error {
	if run == nil || run.Attempt == 0 {
		return ErrLeaseLost
	}
	var attempt mysql.WorkflowAttempt
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND attempt = ?", run.ID, run.Attempt).First(&attempt)
	if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return ErrLeaseLost
	}
	if lookup.Error != nil {
		return fmt.Errorf("lock recovery Attempt for reclaim: %w", lookup.Error)
	}
	if attempt.FinishedAt != nil {
		return ErrLeaseLost
	}
	if attempt.OperationID == nil || *attempt.OperationID != operationID {
		return ErrLeaseLost
	}
	updates := map[string]any{
		"status": RunStatusRunning, "current_phase": "recovering", "worker_id": workerID,
		"operation_id": operationID, "updated_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
	}
	if generationChanged {
		updates["lease_generation"] = token.Generation
	}
	// A crashed process may have already attached a trace.  That trace is an
	// incomplete observation; clear it so the new actual Runner invocation can
	// attach its own Attempt-scoped trace without rewriting Run history.
	updates["trace_id"] = nil
	updates["trace_quality"] = "unknown"
	result := tx.Model(&mysql.WorkflowAttempt{}).Where("id = ? AND finished_at IS NULL", attempt.ID).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("reclaim recovery Attempt: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// StartRecoveryOperation 在当前 fenced lease 内按 run event → operation.started
// 顺序写入同一事务；started Event 的 correlation_seq 永远指向更早 Run Event。
func (s *GORMStore) StartRecoveryOperation(ctx context.Context, claim RecoveryOperationClaim) error {
	if claim.Lease.RunID == "" || claim.Operation.OperationID == "" {
		return ErrInvalidOperationInput
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		if err := applyDurableRuntimeContract(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&mysql.WorkflowRun{})).Where("id = ?", claim.Lease.RunID).First(&run).Error; err != nil {
			return err
		}
		if err := ensureCurrentLease(tx, &run, claim.Lease); err != nil {
			return err
		}
		return startRecoveryOperationTx(tx, &run, claim)
	})
}

func startRecoveryOperationTx(tx *gorm.DB, run *mysql.WorkflowRun, claim RecoveryOperationClaim) error {
	if run == nil {
		return ErrLeaseLost
	}
	if claim.Operation.ExpectedCompatibilityHash != "" && (run.RuntimeCompatibilityHash == nil || *run.RuntimeCompatibilityHash != claim.Operation.ExpectedCompatibilityHash) {
		return ErrOperationPrecondition
	}
	var accepted mysql.WorkflowEvent
	if err := tx.Where("run_id = ? AND operation_id = ? AND event_type = ?", run.ID, claim.Operation.OperationID, EventOperationAccepted).First(&accepted).Error; err != nil {
		return err
	}
	var started mysql.WorkflowEvent
	if err := tx.Where("run_id = ? AND operation_id = ? AND event_type = ?", run.ID, claim.Operation.OperationID, EventOperationStarted).First(&started).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// The existing StartRecovery/RecordRecoverySelection primitive is the
	// sole writer of run.resumed/run.replayed.  Writing those events here
	// would duplicate the lifecycle phase on every operation reclaim.
	correlationSeq, err := latestPriorRunEventSeqTx(tx, run.ID)
	if err != nil {
		return err
	}
	opEvent, err := (OperationEventInput{Type: EventOperationStarted, OperationID: claim.Operation.OperationID, Action: claim.Operation.Action, CorrelationSeq: correlationSeq, ExecutionGeneration: claim.Lease.Generation}).WorkflowEventInput()
	if err != nil {
		return err
	}
	opPayload, err := marshalDurableEvent(opEvent)
	if err != nil {
		return err
	}
	opSeq, err := allocateEventSeqTx(tx, run.ID)
	if err != nil {
		return err
	}
	return insertDurableEvent(tx, run.ID, opSeq, opEvent, opPayload)
}

func latestPriorRunEventSeqTx(tx *gorm.DB, runID string) (uint64, error) {
	var seq uint64
	if err := tx.Model(&mysql.WorkflowEvent{}).
		Where("run_id = ? AND operation_id IS NULL", runID).
		Order("seq DESC").Limit(1).Pluck("seq", &seq).Error; err != nil {
		return 0, err
	}
	if seq == 0 {
		return 0, ErrOperationPrecondition
	}
	return seq, nil
}

// insertOperationTerminalEventTx appends one terminal operation Event after
// its correlated Run Event.  Callers use it inside their existing fenced
// transaction so a Run transition and operation result commit or roll back
// together.
func insertOperationTerminalEventTx(tx *gorm.DB, runID, operationID, action, eventType string, correlationSeq uint64, errorCode, reason string) error {
	if strings.TrimSpace(operationID) == "" || strings.TrimSpace(action) == "" || correlationSeq == 0 {
		return ErrInvalidOperationInput
	}
	var existing mysql.WorkflowEvent
	lookup := tx.Where("run_id = ? AND operation_id = ? AND event_type IN ?", runID, operationID,
		[]string{EventOperationSucceeded, EventOperationFailed, EventOperationCanceled, EventOperationRejected}).First(&existing)
	if lookup.Error == nil {
		if existing.EventType == eventType {
			return nil
		}
		return ErrInvalidOperationTransition
	}
	if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return lookup.Error
	}
	var started mysql.WorkflowEvent
	if err := tx.Where("run_id = ? AND operation_id = ? AND event_type = ?", runID, operationID, EventOperationStarted).First(&started).Error; err != nil {
		return fmt.Errorf("operation terminal requires started: %w", err)
	}
	opEvent, err := (OperationEventInput{
		Type: eventType, OperationID: operationID, Action: action,
		CorrelationSeq: correlationSeq, ErrorCode: errorCode, ResultReason: reason,
	}).WorkflowEventInput()
	if err != nil {
		return err
	}
	payload, err := marshalDurableEvent(opEvent)
	if err != nil {
		return err
	}
	seq, err := allocateEventSeqTx(tx, runID)
	if err != nil {
		return err
	}
	return insertDurableEvent(tx, runID, seq, opEvent, payload)
}

// FinishRecoveryOperationResult carries the execution result that must be
// committed with the Run terminal transition.  In particular, successful
// recovery must provide the new Session revision; callers cannot publish a
// success operation while bypassing CompleteRunAndCommitSession.
type FinishRecoveryOperationResult struct {
	Status            string
	OutputPayload     string
	ErrorMessage      string
	ErrorCode         string
	Reason            string
	TraceQuality      string
	TraceID           string
	RevisionStateJSON json.RawMessage
	// OperationFinalized is set when an existing Run transition (for example
	// Approval publication or fail-closed parking) already wrote the terminal
	// operation Event in that same transaction.
	OperationFinalized bool
	RunTransitioned    bool
}

// FinishRecoveryOperation is the backwards-compatible adapter used by the
// Worker for failure/cancel results that have no additional output.  All
// durable writes still go through CompleteRunAndCommitSession.
func (s *GORMStore) FinishRecoveryOperation(ctx context.Context, claim RecoveryOperationClaim, status, errorCode, reason string) error {
	return s.FinishRecoveryOperationWithResult(ctx, claim, FinishRecoveryOperationResult{
		Status: status, ErrorCode: errorCode, Reason: reason, ErrorMessage: reason,
	})
}

// FailRecoveryOperation closes a started command without changing Run state.
// It is used only when a safe-point or transition handoff fails; the Run keeps
// its Session lock and remains eligible for a later explicit command.
func (s *GORMStore) FailRecoveryOperation(ctx context.Context, claim RecoveryOperationClaim, errorCode, reason string) error {
	if strings.TrimSpace(claim.Operation.OperationID) == "" || strings.TrimSpace(claim.Operation.Action) == "" {
		return ErrInvalidOperationInput
	}
	if errorCode == "" {
		errorCode = "recovery_failed"
	}
	if reason == "" {
		reason = "recovery_failed"
	}
	if !operationReasonCodePattern.MatchString(errorCode) || !operationReasonCodePattern.MatchString(reason) {
		return ErrInvalidOperationInput
	}
	return s.withFencedRunTransaction(ctx, claim.Lease, claim.Run.Status, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		correlation, err := latestPriorRunEventSeqTx(tx, run.ID)
		if err != nil {
			return err
		}
		return insertOperationTerminalEventTx(tx, run.ID, claim.Operation.OperationID, claim.Operation.Action,
			EventOperationFailed, correlation, errorCode, reason)
	})
}

// FailRecoveryOperationAfterTransition closes a command whose Run transition
// has already released the execution lease. It is a defensive fallback for a
// transition path that failed to include operation metadata; normal Approval,
// parking, and restore paths close the operation in their own transaction.
func (s *GORMStore) FailRecoveryOperationAfterTransition(ctx context.Context, claim RecoveryOperationClaim, errorCode, reason string) error {
	if strings.TrimSpace(claim.Operation.OperationID) == "" || strings.TrimSpace(claim.Operation.Action) == "" {
		return ErrInvalidOperationInput
	}
	if errorCode == "" {
		errorCode = "recovery_transition_unclosed"
	}
	if reason == "" {
		reason = "recovery_transition_unclosed"
	}
	if !operationReasonCodePattern.MatchString(errorCode) || !operationReasonCodePattern.MatchString(reason) {
		return ErrInvalidOperationInput
	}
	if err := s.authorizeRunScope(ctx, claim.Lease.RunID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", claim.Lease.RunID).First(&run).Error; err != nil {
			return err
		}
		var started mysql.WorkflowEvent
		if err := tx.Where("run_id = ? AND operation_id = ? AND event_type = ?", run.ID, claim.Operation.OperationID, EventOperationStarted).First(&started).Error; err != nil {
			return err
		}
		if facts, err := decodeOperationPayloadFacts(started); err != nil {
			return err
		} else if facts.ExecutionGeneration != 0 && facts.ExecutionGeneration != claim.Lease.Generation {
			return ErrLeaseLost
		}
		correlation, err := latestPriorRunEventSeqTx(tx, run.ID)
		if err != nil {
			return err
		}
		return insertOperationTerminalEventTx(tx, run.ID, claim.Operation.OperationID, claim.Operation.Action,
			EventOperationFailed, correlation, errorCode, reason)
	})
}

// RestoreRecoveryOperation atomically clears a runtime-incompatible parked Run,
// returns it to pending, and closes the operation. No Attempt or Runner is
// created; the ordinary claim loop performs the next execution.
func (s *GORMStore) RestoreRecoveryOperation(ctx context.Context, claim RecoveryOperationClaim) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		if err := applyDurableRuntimeContract(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&mysql.WorkflowRun{})).Where("id = ?", claim.Lease.RunID).First(&run).Error; err != nil {
			return err
		}
		if run.Status != RunStatusParked || run.ParkReason == nil || *run.ParkReason != ParkReasonRuntimeIncompatible {
			return ErrOperationPrecondition
		}
		if run.RuntimeCompatibilityHash == nil || claim.Operation.ExpectedCompatibilityHash == "" || *run.RuntimeCompatibilityHash != claim.Operation.ExpectedCompatibilityHash {
			return ErrOperationPrecondition
		}
		if err := ensureCurrentLease(tx, &run, claim.Lease); err != nil {
			return err
		}
		facts, err := loadRecoveryFacts(tx, &run)
		if err != nil {
			return err
		}
		if err := validateRecoveryOperationDependenciesTx(tx, &run, facts, OperationActionRestore, claim.Operation.ExpectedCompatibilityHash); err != nil {
			return err
		}
		var started mysql.WorkflowEvent
		if err := tx.Where("run_id = ? AND operation_id = ? AND event_type = ?", run.ID, claim.Operation.OperationID, EventOperationStarted).First(&started).Error; err != nil {
			return err
		}
		if err := ValidateRunTransition(run.Status, RunStatusPending, TransitionIntentRuntimeRestored, ParkReasonRuntimeIncompatible); err != nil {
			return err
		}
		// Reuse the canonical fenced transition primitive; Restore is only a
		// runtime-incompatible unlock, never a generic status edit.
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusParked, claim.Lease, map[string]any{
			"status": RunStatusPending, "park_reason": nil, "lease_owner": nil,
			"lease_until": nil, "heartbeat_at": nil, "available_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		})
		if err != nil {
			return err
		}
		runEvent := WorkflowEventInput{Type: EventRunResumed, Payload: EventPayload{Attributes: map[string]any{"operation_id": claim.Operation.OperationID, "restore": true}}}
		runPayload, err := marshalDurableEvent(runEvent)
		if err != nil {
			return err
		}
		if err := insertDurableEvent(tx, run.ID, seq, runEvent, runPayload); err != nil {
			return err
		}
		return insertOperationTerminalEventTx(tx, run.ID, claim.Operation.OperationID, claim.Operation.Action,
			EventOperationSucceeded, seq, "", "runtime_restored")
	})
}

// FinishRecoveryOperationWithResult closes a recovery operation through the
// sole durable completion primitive.  The primitive owns Run terminal state,
// Session revision/release, Approval invalidation, Attempt projection, and
// the correlated operation terminal Event in one transaction.
func (s *GORMStore) FinishRecoveryOperationWithResult(ctx context.Context, claim RecoveryOperationClaim, result FinishRecoveryOperationResult) error {
	if result.OperationFinalized {
		return nil
	}
	if result.Status != OperationStatusSucceeded && result.Status != OperationStatusFailed && result.Status != OperationStatusCanceled {
		return ErrInvalidOperationInput
	}
	if result.ErrorCode != "" && !operationReasonCodePattern.MatchString(result.ErrorCode) {
		return ErrInvalidOperationInput
	}
	if result.Reason != "" && !operationReasonCodePattern.MatchString(result.Reason) {
		return ErrInvalidOperationInput
	}
	if strings.TrimSpace(claim.Operation.OperationID) == "" || strings.TrimSpace(claim.Operation.Action) == "" {
		return ErrInvalidOperationInput
	}
	if claim.Lease.RunID == "" || claim.Lease.RunID != claim.Operation.RunID {
		return ErrLeaseLost
	}

	return s.CompleteRunAndCommitSession(ctx, CompleteRunInput{
		RunID:              claim.Lease.RunID,
		ExpectedStatus:     claim.Run.Status,
		TargetStatus:       recoveryOperationRunStatus(result.Status),
		Lease:              claim.Lease,
		OutputPayload:      result.OutputPayload,
		ErrorMessage:       result.ErrorMessage,
		TraceQuality:       result.TraceQuality,
		TraceID:            result.TraceID,
		RevisionStateJSON:  result.RevisionStateJSON,
		OperationID:        claim.Operation.OperationID,
		OperationAction:    claim.Operation.Action,
		OperationErrorCode: result.ErrorCode,
		OperationReason:    result.Reason,
	})
}

func recoveryOperationRunStatus(status string) string {
	switch status {
	case OperationStatusSucceeded:
		return RunStatusSucceeded
	case OperationStatusCanceled:
		return RunStatusCanceled
	default:
		return RunStatusFailed
	}
}

func ensureCurrentLease(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken) error {
	if run == nil || run.ID != token.RunID || run.LeaseOwner == nil || *run.LeaseOwner != token.Owner || run.LeaseGeneration != token.Generation || run.LeaseUntil == nil {
		return ErrLeaseLost
	}
	var now time.Time
	if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&now).Error; err != nil {
		return err
	}
	if !run.LeaseUntil.After(now) {
		return ErrLeaseLost
	}
	return nil
}

func allocateEventSeqTx(tx *gorm.DB, runID string) (uint64, error) {
	var run mysql.WorkflowRun
	if err := tx.Select("last_event_seq").Where("id = ?", runID).First(&run).Error; err != nil {
		return 0, err
	}
	seq := run.LastEventSeq + 1
	if result := tx.Model(&mysql.WorkflowRun{}).Where("id = ? AND last_event_seq = ?", runID, run.LastEventSeq).Update("last_event_seq", seq); result.Error != nil {
		return 0, result.Error
	} else if result.RowsAffected != 1 {
		return 0, ErrRunCASConflict
	}
	return seq, nil
}

// AcceptRecoveryOperationInput 只包含客户端命令材料；Actor 必须从 Context 读取。
type AcceptRecoveryOperationInput struct {
	OperationRequestInput
	IdempotencyKey string
}

// OperationEventInput 是 canonical Operation Event 的类型化构造输入。
type OperationEventInput struct {
	Type                      string
	OperationID               string
	Action                    string
	IdempotencyKeyDigest      string
	RequestFingerprint        string
	ActorID                   string
	Reason                    string
	CorrelationSeq            uint64
	TraceID                   string
	ExpectedGeneration        uint64
	ExpectedCompatibilityHash string
	ExecutionGeneration       uint64
	ErrorCode                 string
	ResultReason              string
}

// WorkflowEventInput 将 Operation 元数据转换为公共 Event 输入。
// operation ID/action 仅为兼容旧 mapper 镜像到安全 payload；原始 key/reason 不进入正文。
func (input OperationEventInput) WorkflowEventInput() (WorkflowEventInput, error) {
	if !isOperationEventType(input.Type) {
		return WorkflowEventInput{}, fmt.Errorf("%w: unknown operation event %q", ErrInvalidOperationInput, input.Type)
	}
	if strings.TrimSpace(input.OperationID) == "" || strings.TrimSpace(input.Action) == "" {
		return WorkflowEventInput{}, fmt.Errorf("%w: operation_id and action are required", ErrInvalidOperationInput)
	}
	if err := validateOperationMetadata(input.Type, &OperationEventMetadata{OperationID: input.OperationID, CommandAction: input.Action, IdempotencyKeyDigest: input.IdempotencyKeyDigest, RequestFingerprint: input.RequestFingerprint, ActorID: input.ActorID, Reason: input.Reason, CorrelationSeq: input.CorrelationSeq, ExpectedGeneration: input.ExpectedGeneration, ExpectedCompatibilityHash: input.ExpectedCompatibilityHash, ExecutionGeneration: input.ExecutionGeneration, ErrorCode: input.ErrorCode, ResultReason: input.ResultReason}); err != nil {
		return WorkflowEventInput{}, err
	}
	attrs := map[string]any{"operation_id": input.OperationID, "action": input.Action}
	if input.Type == EventOperationAccepted {
		attrs["expected_generation"] = input.ExpectedGeneration
		attrs["expected_compatibility_hash"] = input.ExpectedCompatibilityHash
	}
	if input.ExecutionGeneration != 0 {
		attrs["execution_generation"] = input.ExecutionGeneration
	}
	if input.ErrorCode != "" {
		attrs["error_code"] = input.ErrorCode
	}
	if input.ResultReason != "" {
		attrs["reason_code"] = input.ResultReason
	}
	return WorkflowEventInput{
		Type:    input.Type,
		TraceID: input.TraceID,
		Payload: EventPayload{Attributes: attrs},
		Operation: &OperationEventMetadata{
			OperationID: input.OperationID, CommandAction: input.Action,
			IdempotencyKeyDigest: input.IdempotencyKeyDigest,
			RequestFingerprint:   input.RequestFingerprint, ActorID: input.ActorID,
			Reason: input.Reason, CorrelationSeq: input.CorrelationSeq,
			ExpectedGeneration: input.ExpectedGeneration, ExpectedCompatibilityHash: input.ExpectedCompatibilityHash,
			ExecutionGeneration: input.ExecutionGeneration,
			ErrorCode:           input.ErrorCode, ResultReason: input.ResultReason,
		},
	}, nil
}

func validateOperationMetadata(eventType string, metadata *OperationEventMetadata) error {
	if !isOperationEventType(eventType) {
		if metadata != nil {
			return fmt.Errorf("%w: non-operation event cannot carry operation metadata", ErrInvalidOperationInput)
		}
		return nil
	}
	if metadata == nil {
		return fmt.Errorf("%w: operation event requires metadata", ErrInvalidOperationInput)
	}
	if !operationIDPattern.MatchString(metadata.OperationID) {
		return fmt.Errorf("%w: operation_id must be op-hex64", ErrInvalidOperationInput)
	}
	switch metadata.CommandAction {
	case OperationActionResume, OperationActionReplay, OperationActionCancel, OperationActionRestore:
	default:
		return fmt.Errorf("%w: invalid operation action", ErrInvalidOperationInput)
	}
	for name, value := range map[string]string{"idempotency_key_digest": metadata.IdempotencyKeyDigest, "request_fingerprint": metadata.RequestFingerprint} {
		if value != "" && !compatibilityHashPattern.MatchString(value) {
			return fmt.Errorf("%w: %s must be lowercase hex64", ErrInvalidOperationInput, name)
		}
	}
	if len(metadata.ActorID) > 128 || !utf8.ValidString(metadata.ActorID) {
		return fmt.Errorf("%w: actor_id exceeds 128 bytes", ErrInvalidOperationInput)
	}
	if eventType == EventOperationAccepted {
		if metadata.IdempotencyKeyDigest == "" || metadata.RequestFingerprint == "" || strings.TrimSpace(metadata.ActorID) == "" {
			return fmt.Errorf("%w: accepted metadata is incomplete", ErrInvalidOperationInput)
		}
		if !utf8.ValidString(metadata.Reason) || len(metadata.Reason) < 1 || len(metadata.Reason) > 1000 || strings.TrimSpace(metadata.Reason) == "" {
			return fmt.Errorf("%w: accepted reason length must be 1..1000 bytes", ErrInvalidOperationInput)
		}
		if metadata.CorrelationSeq != 0 {
			return fmt.Errorf("%w: accepted event cannot carry correlation_seq", ErrInvalidOperationInput)
		}
		if metadata.ExpectedCompatibilityHash != "" && !compatibilityHashPattern.MatchString(metadata.ExpectedCompatibilityHash) {
			return fmt.Errorf("%w: accepted expected_compatibility_hash must be lowercase hex64", ErrInvalidOperationInput)
		}
		if metadata.ExecutionGeneration != 0 {
			return fmt.Errorf("%w: accepted event cannot carry execution_generation", ErrInvalidOperationInput)
		}
	} else if metadata.IdempotencyKeyDigest != "" || metadata.RequestFingerprint != "" || metadata.ActorID != "" || metadata.Reason != "" || metadata.ExpectedGeneration != 0 || metadata.ExpectedCompatibilityHash != "" {
		return fmt.Errorf("%w: non-accepted event carries acceptance metadata", ErrInvalidOperationInput)
	} else if metadata.CorrelationSeq == 0 {
		return fmt.Errorf("%w: lifecycle event requires correlation_seq", ErrInvalidOperationInput)
	}
	if eventType != EventOperationStarted && metadata.ExecutionGeneration != 0 {
		return fmt.Errorf("%w: execution_generation is only valid for operation.started", ErrInvalidOperationInput)
	}
	if metadata.ErrorCode != "" && !operationReasonCodePattern.MatchString(metadata.ErrorCode) {
		return fmt.Errorf("%w: invalid operation error_code", ErrInvalidOperationInput)
	}
	if metadata.ResultReason != "" && !operationReasonCodePattern.MatchString(metadata.ResultReason) {
		return fmt.Errorf("%w: invalid operation reason_code", ErrInvalidOperationInput)
	}
	if eventType != EventOperationFailed && metadata.ErrorCode != "" {
		return fmt.Errorf("%w: error_code is only valid for operation.failed", ErrInvalidOperationInput)
	}
	if metadata.ResultReason != "" {
		switch eventType {
		case EventOperationSucceeded, EventOperationFailed, EventOperationCanceled, EventOperationRejected:
		default:
			return fmt.Errorf("%w: reason_code is only valid for terminal operation events", ErrInvalidOperationInput)
		}
	}
	return nil
}

// OperationIdentity 计算稳定 Operation ID 和唯一可持久化的 key 材料。
// 内层摘要以原始 32 字节参与 domain-separated Hash。
func OperationIdentity(runID, idempotencyKey string) (string, string, error) {
	if strings.TrimSpace(runID) == "" || len(runID) > 64 || !utf8.ValidString(runID) {
		return "", "", fmt.Errorf("%w: invalid run_id", ErrInvalidOperationInput)
	}
	if len(idempotencyKey) < 16 || len(idempotencyKey) > 128 || !utf8.ValidString(idempotencyKey) {
		return "", "", fmt.Errorf("%w: idempotency key length must be 16..128 bytes", ErrInvalidOperationInput)
	}
	inner := sha256.Sum256([]byte(idempotencyKey))
	input := append([]byte(OperationIdentityDomain+"\x00"+runID+"\x00"), inner[:]...)
	outer := sha256.Sum256(input)
	return "op-" + hex.EncodeToString(outer[:]), hex.EncodeToString(inner[:]), nil
}

// OperationRequestFingerprint 对五个精确请求字段的 canonical JSON 做 SHA-256；Reason
// 在 Hash 前绝不脱敏。
func OperationRequestFingerprint(input OperationRequestInput) (string, error) {
	if err := validateOperationRequest(input); err != nil {
		return "", err
	}
	canonical, err := policy.CanonicalJSON(struct {
		RunID                     string `json:"run_id"`
		Action                    string `json:"action"`
		Reason                    string `json:"reason"`
		ExpectedGeneration        uint64 `json:"expected_generation"`
		ExpectedCompatibilityHash string `json:"expected_compatibility_hash"`
	}{input.RunID, input.Action, input.Reason, input.ExpectedGeneration, input.ExpectedCompatibilityHash})
	if err != nil {
		return "", fmt.Errorf("canonical operation request: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func validateOperationRequest(input OperationRequestInput) error {
	if strings.TrimSpace(input.RunID) == "" || len(input.RunID) > 64 || !utf8.ValidString(input.RunID) {
		return fmt.Errorf("%w: invalid run_id", ErrInvalidOperationInput)
	}
	switch input.Action {
	case OperationActionResume, OperationActionReplay, OperationActionCancel, OperationActionRestore:
	default:
		return fmt.Errorf("%w: invalid action", ErrInvalidOperationInput)
	}
	if !utf8.ValidString(input.Reason) || len(input.Reason) < 1 || len(input.Reason) > 1000 || strings.TrimSpace(input.Reason) == "" {
		return fmt.Errorf("%w: reason length must be 1..1000 bytes", ErrInvalidOperationInput)
	}
	if input.Action != OperationActionCancel && input.ExpectedGeneration == 0 {
		return fmt.Errorf("%w: expected_generation must be positive", ErrInvalidOperationInput)
	}
	if input.ExpectedCompatibilityHash != "" && !compatibilityHashPattern.MatchString(input.ExpectedCompatibilityHash) {
		return fmt.Errorf("%w: expected_compatibility_hash must be lowercase hex64", ErrInvalidOperationInput)
	}
	if input.Action == OperationActionRestore && input.ExpectedCompatibilityHash == "" {
		return fmt.Errorf("%w: expected_compatibility_hash is required for restore", ErrInvalidOperationInput)
	}
	return nil
}

func isOperationEventType(eventType string) bool {
	switch eventType {
	case EventOperationAccepted, EventOperationStarted, EventOperationSucceeded,
		EventOperationFailed, EventOperationCanceled, EventOperationRejected:
		return true
	default:
		return false
	}
}

// DeriveOperation 校验并按 seq 折叠 Operation Event 为单一状态。
func DeriveOperation(events []mysql.WorkflowEvent) (Operation, error) {
	if len(events) == 0 {
		return Operation{}, fmt.Errorf("%w: no events", ErrInvalidOperationTransition)
	}
	ordered := append([]mysql.WorkflowEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].Seq == ordered[index].Seq {
			return Operation{}, fmt.Errorf("%w: duplicate seq %d", ErrInvalidOperationTransition, ordered[index].Seq)
		}
	}
	var operation Operation
	for index, event := range ordered {
		if !isOperationEventType(event.EventType) || event.OperationID == nil || strings.TrimSpace(*event.OperationID) == "" || event.CommandAction == nil || strings.TrimSpace(*event.CommandAction) == "" {
			return Operation{}, fmt.Errorf("%w: malformed event at seq %d", ErrInvalidOperationTransition, event.Seq)
		}
		if index == 0 {
			operation.OperationID, operation.RunID, operation.Action = *event.OperationID, event.RunID, *event.CommandAction
		} else if *event.OperationID != operation.OperationID || event.RunID != operation.RunID || *event.CommandAction != operation.Action {
			return Operation{}, fmt.Errorf("%w: operation identity changed at seq %d", ErrInvalidOperationTransition, event.Seq)
		}
		metadata := &OperationEventMetadata{OperationID: *event.OperationID, CommandAction: *event.CommandAction}
		if event.IdempotencyKeyDigest != nil {
			metadata.IdempotencyKeyDigest = *event.IdempotencyKeyDigest
		}
		if event.RequestFingerprint != nil {
			metadata.RequestFingerprint = *event.RequestFingerprint
		}
		if event.ActorID != nil {
			metadata.ActorID = *event.ActorID
		}
		if event.ReasonRedacted != nil {
			metadata.Reason = *event.ReasonRedacted
		}
		if event.CorrelationSeq != nil {
			metadata.CorrelationSeq = *event.CorrelationSeq
		}
		if err := validateOperationMetadata(event.EventType, metadata); err != nil {
			return Operation{}, fmt.Errorf("%w at seq %d: %v", ErrInvalidOperationTransition, event.Seq, err)
		}
		payloadFacts, err := decodeOperationPayloadFacts(event)
		if err != nil {
			return Operation{}, fmt.Errorf("%w at seq %d: %v", ErrInvalidOperationTransition, event.Seq, err)
		}
		if event.EventType != EventOperationAccepted && metadata.CorrelationSeq >= event.Seq {
			return Operation{}, fmt.Errorf("%w: correlation_seq %d must reference an earlier Run Event before seq %d", ErrInvalidOperationTransition, metadata.CorrelationSeq, event.Seq)
		}
		switch event.EventType {
		case EventOperationAccepted:
			if index != 0 {
				return Operation{}, fmt.Errorf("%w: accepted must be first", ErrInvalidOperationTransition)
			}
			operation.Status, operation.AcceptedSeq = OperationStatusAccepted, event.Seq
			if event.IdempotencyKeyDigest != nil {
				operation.IdempotencyKeyDigest = *event.IdempotencyKeyDigest
			}
			if event.RequestFingerprint != nil {
				operation.RequestFingerprint = *event.RequestFingerprint
			}
			if event.ActorID != nil {
				operation.ActorID = *event.ActorID
			}
			if event.ReasonRedacted != nil {
				operation.Reason = *event.ReasonRedacted
			}
			operation.AcceptedAt = event.CreatedAt
			operation.ExpectedGeneration = payloadFacts.ExpectedGeneration
			operation.ExpectedCompatibilityHash = payloadFacts.ExpectedCompatibilityHash
		case EventOperationStarted:
			if index == 0 || operation.Status != OperationStatusAccepted {
				return Operation{}, fmt.Errorf("%w: started requires accepted", ErrInvalidOperationTransition)
			}
			operation.Status, operation.StartedSeq = OperationStatusRunning, event.Seq
			operation.ExecutionGeneration = payloadFacts.ExecutionGeneration
			t := event.CreatedAt
			operation.StartedAt = &t
		case EventOperationSucceeded, EventOperationFailed, EventOperationCanceled, EventOperationRejected:
			if index == 0 || (operation.Status != OperationStatusAccepted && operation.Status != OperationStatusRunning) || (event.EventType != EventOperationRejected && operation.Status != OperationStatusRunning) {
				return Operation{}, fmt.Errorf("%w: terminal event requires started", ErrInvalidOperationTransition)
			}
			switch event.EventType {
			case EventOperationSucceeded:
				operation.Status = OperationStatusSucceeded
			case EventOperationFailed:
				operation.Status = OperationStatusFailed
			case EventOperationCanceled:
				operation.Status = OperationStatusCanceled
			case EventOperationRejected:
				operation.Status = OperationStatusRejected
			}
			operation.Terminal = true
			operation.ErrorCode = payloadFacts.ErrorCode
			operation.ResultReason = payloadFacts.ResultReason
			t := event.CreatedAt
			operation.FinishedAt = &t
		}
		if operation.Terminal && index < len(ordered)-1 {
			return Operation{}, fmt.Errorf("%w: event after terminal", ErrInvalidOperationTransition)
		}
		if event.CorrelationSeq != nil {
			operation.CorrelationSeq = *event.CorrelationSeq
		}
	}
	if operation.Status == "" {
		return Operation{}, fmt.Errorf("%w: empty status", ErrInvalidOperationTransition)
	}
	return operation, nil
}

type operationPayloadFacts struct {
	ExpectedGeneration        uint64
	ExpectedCompatibilityHash string
	ExecutionGeneration       uint64
	ErrorCode                 string
	ResultReason              string
}

func decodeOperationPayloadFacts(event mysql.WorkflowEvent) (operationPayloadFacts, error) {
	if strings.TrimSpace(event.Payload) == "" {
		return operationPayloadFacts{}, nil
	}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(event.Payload), &envelope); err != nil {
		return operationPayloadFacts{}, fmt.Errorf("decode operation payload: %w", err)
	}
	var facts operationPayloadFacts
	if raw := envelope.Data["expected_generation"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &facts.ExpectedGeneration); err != nil {
			return operationPayloadFacts{}, fmt.Errorf("decode expected_generation: %w", err)
		}
	}
	if raw := envelope.Data["execution_generation"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &facts.ExecutionGeneration); err != nil {
			return operationPayloadFacts{}, fmt.Errorf("decode execution_generation: %w", err)
		}
	}
	for key, target := range map[string]*string{
		"expected_compatibility_hash": &facts.ExpectedCompatibilityHash,
		"error_code":                  &facts.ErrorCode,
		"reason_code":                 &facts.ResultReason,
	} {
		if raw := envelope.Data[key]; len(raw) != 0 {
			if err := json.Unmarshal(raw, target); err != nil {
				return operationPayloadFacts{}, fmt.Errorf("decode %s: %w", key, err)
			}
		}
	}
	metadata := &OperationEventMetadata{
		OperationID: eventValue(event.OperationID), CommandAction: eventValue(event.CommandAction),
		ExpectedGeneration: facts.ExpectedGeneration, ExpectedCompatibilityHash: facts.ExpectedCompatibilityHash,
		ExecutionGeneration: facts.ExecutionGeneration,
		ErrorCode:           facts.ErrorCode, ResultReason: facts.ResultReason,
	}
	if event.EventType != EventOperationAccepted {
		metadata.CorrelationSeq = pointerValue(event.CorrelationSeq)
	}
	if event.EventType == EventOperationAccepted {
		metadata.IdempotencyKeyDigest = eventValue(event.IdempotencyKeyDigest)
		metadata.RequestFingerprint = eventValue(event.RequestFingerprint)
		metadata.ActorID = eventValue(event.ActorID)
		metadata.Reason = eventValue(event.ReasonRedacted)
	}
	if err := validateOperationMetadata(event.EventType, metadata); err != nil {
		return operationPayloadFacts{}, err
	}
	return facts, nil
}

func eventValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pointerValue(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *GORMStore) LoadOperation(ctx context.Context, operationID string) (Operation, error) {
	if strings.TrimSpace(operationID) == "" {
		return Operation{}, fmt.Errorf("%w: empty operation_id", ErrInvalidOperationInput)
	}
	var rows []mysql.WorkflowEvent
	if err := s.db.WithContext(ctx).Where("operation_id = ?", operationID).Order("seq ASC").Find(&rows).Error; err != nil {
		return Operation{}, fmt.Errorf("load operation events: %w", err)
	}
	if len(rows) == 0 {
		return Operation{}, gorm.ErrRecordNotFound
	}
	if err := s.authorizeRunScope(ctx, rows[0].RunID); err != nil {
		return Operation{}, err
	}
	return DeriveOperation(rows)
}

func (s *GORMStore) ListActiveOperationsForRun(ctx context.Context, runID string) ([]Operation, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("%w: empty run_id", ErrInvalidOperationInput)
	}
	if err := s.authorizeRunScope(ctx, runID); err != nil {
		return nil, err
	}
	var rows []mysql.WorkflowEvent
	if err := s.db.WithContext(ctx).Where("run_id = ? AND operation_id IS NOT NULL", runID).Order("operation_id ASC, seq ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list operation events: %w", err)
	}
	grouped := make(map[string][]mysql.WorkflowEvent)
	order := make([]string, 0)
	for _, row := range rows {
		if row.OperationID == nil {
			continue
		}
		if _, ok := grouped[*row.OperationID]; !ok {
			order = append(order, *row.OperationID)
		}
		grouped[*row.OperationID] = append(grouped[*row.OperationID], row)
	}
	result := make([]Operation, 0, len(order))
	for _, id := range order {
		operation, err := DeriveOperation(grouped[id])
		if err != nil {
			return nil, err
		}
		if !operation.Terminal {
			result = append(result, operation)
		}
	}
	return result, nil
}

// AcceptRecoveryOperation 在 Run 行锁内串行化幂等检查、active-operation 冲突、
// Recovery 合法性与 operation.accepted 写入。它不执行 Runner、Agent、Tool 或 Effect。
func (s *GORMStore) AcceptRecoveryOperation(ctx context.Context, input AcceptRecoveryOperationInput) (OperationRecord, error) {
	if err := policy.Authorize(ctx, policy.PermissionRecoverRuntime, policy.Resource{}); err != nil {
		return OperationRecord{}, err
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return OperationRecord{}, err
	}
	operationID, keyDigest, err := OperationIdentity(input.RunID, input.IdempotencyKey)
	if err != nil {
		return OperationRecord{}, err
	}
	fingerprint, err := OperationRequestFingerprint(input.OperationRequestInput)
	if err != nil {
		return OperationRecord{}, err
	}

	var accepted OperationRecord
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		result := applyDurableRuntimeContract(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&mysql.WorkflowRun{})).
			Where("id = ?", input.RunID).First(&run)
		if result.Error != nil {
			return result.Error
		}
		if err := policy.Authorize(ctx, policy.PermissionViewScoped, policy.Resource{OwnerID: run.UserID}); err != nil {
			return err
		}
		existing, found, err := loadOperationByKeyDigestTx(tx, run.ID, keyDigest)
		if err != nil {
			return err
		}
		if found {
			if existing.RequestFingerprint != fingerprint {
				return ErrOperationIdempotencyConflict
			}
			accepted = OperationRecord{Operation: existing, IdempotentReplay: true}
			return nil
		}
		if input.ExpectedGeneration != 0 && input.ExpectedGeneration != run.LeaseGeneration {
			return ErrOperationPrecondition
		}

		if input.Action == OperationActionCancel && isTerminalRunStatus(run.Status) {
			accepted = terminalCancelRecord(run, operationID, identity.UserID)
			return nil
		}
		if !isDurableRunStatus(run.Status) || isTerminalRunStatus(run.Status) {
			return ErrOperationPrecondition
		}
		active, err := listActiveOperationsForRunTx(tx, run.ID)
		if err != nil {
			return err
		}
		if len(active) != 0 {
			return ErrOperationConflict
		}
		if input.Action == OperationActionCancel {
			if err := validateRecoveryOperationTx(tx, &run, RecoveryFacts{}, input.OperationRequestInput); err != nil {
				return err
			}
			if run.Status == RunStatusRunning {
				if result := tx.Model(&mysql.WorkflowRun{}).Where("id = ? AND status = ?", run.ID, RunStatusRunning).Update("cancel_requested_at", gorm.Expr("CURRENT_TIMESTAMP(3)")); result.Error != nil {
					return result.Error
				} else if result.RowsAffected != 1 {
					return ErrRunCASConflict
				}
			}
			event, err := (OperationEventInput{
				Type: EventOperationAccepted, OperationID: operationID, Action: input.Action,
				IdempotencyKeyDigest: keyDigest, RequestFingerprint: fingerprint,
				ActorID: identity.UserID, Reason: input.Reason,
				ExpectedGeneration: input.ExpectedGeneration, ExpectedCompatibilityHash: input.ExpectedCompatibilityHash,
			}).WorkflowEventInput()
			if err != nil {
				return err
			}
			return acceptOperationEventTx(tx, &run, event, &accepted)
		}
		facts, err := loadRecoveryFacts(tx, &run)
		if err != nil {
			return err
		}
		if err := validateRecoveryOperationTx(tx, &run, facts, input.OperationRequestInput); err != nil {
			return err
		}

		event, err := (OperationEventInput{
			Type: EventOperationAccepted, OperationID: operationID, Action: input.Action,
			IdempotencyKeyDigest: keyDigest, RequestFingerprint: fingerprint,
			ActorID: identity.UserID, Reason: input.Reason,
			ExpectedGeneration: input.ExpectedGeneration, ExpectedCompatibilityHash: input.ExpectedCompatibilityHash,
		}).WorkflowEventInput()
		if err != nil {
			return err
		}
		return acceptOperationEventTx(tx, &run, event, &accepted)
	})
	return accepted, err
}

func acceptOperationEventTx(tx *gorm.DB, run *mysql.WorkflowRun, event WorkflowEventInput, accepted *OperationRecord) error {
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	seq := run.LastEventSeq + 1
	result := tx.Model(&mysql.WorkflowRun{}).Where("id = ? AND last_event_seq = ?", run.ID, run.LastEventSeq).
		Update("last_event_seq", seq)
	if result.Error != nil {
		return fmt.Errorf("allocate operation accepted Event seq: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrOperationConflict
	}
	if err := insertDurableEvent(tx, run.ID, seq, event, payload); err != nil {
		return err
	}
	var row mysql.WorkflowEvent
	if err := tx.Where("run_id = ? AND seq = ?", run.ID, seq).First(&row).Error; err != nil {
		return fmt.Errorf("reload accepted operation Event: %w", err)
	}
	operation, err := DeriveOperation([]mysql.WorkflowEvent{row})
	if err != nil {
		return err
	}
	*accepted = OperationRecord{Operation: operation}
	return nil
}

func rejectAcceptedOperationTx(tx *gorm.DB, run *mysql.WorkflowRun, operation Operation, reason string) error {
	if run == nil || strings.TrimSpace(operation.OperationID) == "" {
		return ErrInvalidOperationInput
	}
	correlation, err := latestPriorRunEventSeqTx(tx, run.ID)
	if err != nil {
		return err
	}
	// A rejected command is terminal without an operation.started Event: the
	// Worker proved the accepted precondition no longer holds before execution.
	opEvent, err := (OperationEventInput{
		Type: EventOperationRejected, OperationID: operation.OperationID, Action: operation.Action,
		CorrelationSeq: correlation, ResultReason: reason,
	}).WorkflowEventInput()
	if err != nil {
		return err
	}
	payload, err := marshalDurableEvent(opEvent)
	if err != nil {
		return err
	}
	seq, err := allocateEventSeqTx(tx, run.ID)
	if err != nil {
		return err
	}
	return insertDurableEvent(tx, run.ID, seq, opEvent, payload)
}

func loadOperationByKeyDigestTx(tx *gorm.DB, runID, keyDigest string) (Operation, bool, error) {
	var accepted mysql.WorkflowEvent
	result := tx.Where("run_id = ? AND event_type = ? AND idempotency_key_digest = ?", runID, EventOperationAccepted, keyDigest).
		Order("seq ASC").First(&accepted)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return Operation{}, false, nil
	}
	if result.Error != nil {
		return Operation{}, false, fmt.Errorf("load idempotent operation: %w", result.Error)
	}
	if accepted.OperationID == nil {
		return Operation{}, false, fmt.Errorf("%w: accepted operation is missing operation_id", ErrInvalidOperationTransition)
	}
	var rows []mysql.WorkflowEvent
	if err := tx.Where("run_id = ? AND operation_id = ?", runID, *accepted.OperationID).Order("seq ASC").Find(&rows).Error; err != nil {
		return Operation{}, false, fmt.Errorf("load idempotent operation lifecycle: %w", err)
	}
	operation, err := DeriveOperation(rows)
	return operation, true, err
}

func listActiveOperationsForRunTx(tx *gorm.DB, runID string) ([]Operation, error) {
	var rows []mysql.WorkflowEvent
	if err := tx.Where("run_id = ? AND operation_id IS NOT NULL", runID).Order("operation_id ASC, seq ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list active operation events: %w", err)
	}
	grouped := make(map[string][]mysql.WorkflowEvent)
	order := make([]string, 0)
	for _, row := range rows {
		if row.OperationID == nil {
			continue
		}
		if _, exists := grouped[*row.OperationID]; !exists {
			order = append(order, *row.OperationID)
		}
		grouped[*row.OperationID] = append(grouped[*row.OperationID], row)
	}
	active := make([]Operation, 0, len(order))
	for _, id := range order {
		operation, err := DeriveOperation(grouped[id])
		if err != nil {
			return nil, err
		}
		if !operation.Terminal {
			active = append(active, operation)
		}
	}
	return active, nil
}

func validateRecoveryOperationTx(tx *gorm.DB, run *mysql.WorkflowRun, facts RecoveryFacts, input OperationRequestInput) error {
	if run == nil || !isDurableRunStatus(run.Status) || isTerminalRunStatus(run.Status) {
		return fmt.Errorf("%w: Run is terminal", ErrOperationPrecondition)
	}
	if input.ExpectedGeneration != 0 && input.ExpectedGeneration != run.LeaseGeneration {
		return fmt.Errorf("%w: lease generation changed", ErrOperationPrecondition)
	}
	if input.Action != OperationActionCancel && input.ExpectedGeneration != run.LeaseGeneration {
		return fmt.Errorf("%w: expected generation is not current", ErrOperationPrecondition)
	}
	if input.ExpectedCompatibilityHash != "" &&
		(run.RuntimeCompatibilityHash == nil || input.ExpectedCompatibilityHash != *run.RuntimeCompatibilityHash) {
		return fmt.Errorf("%w: runtime compatibility hash changed", ErrOperationPrecondition)
	}
	if input.Action == OperationActionCancel {
		return nil
	}
	return validateRecoveryOperationDependenciesTx(tx, run, facts, input.Action, input.ExpectedCompatibilityHash)
}

// validateRecoveryOperationDependenciesTx is the Worker-side revalidation
// shared by first claim, crash reclaim, and Restore. It intentionally omits
// the original expected-generation comparison because a first claim may fence
// the Run to a new generation before execution starts.
func validateRecoveryOperationDependenciesTx(tx *gorm.DB, run *mysql.WorkflowRun, facts RecoveryFacts, action, expectedCompatibilityHash string) error {
	if run == nil || !isDurableRunStatus(run.Status) || isTerminalRunStatus(run.Status) {
		return fmt.Errorf("%w: Run is terminal", ErrOperationPrecondition)
	}
	if expectedCompatibilityHash != "" &&
		(run.RuntimeCompatibilityHash == nil || expectedCompatibilityHash != *run.RuntimeCompatibilityHash) {
		return fmt.Errorf("%w: runtime compatibility hash changed", ErrOperationPrecondition)
	}
	approvalTarget, err := hasExplicitApprovalResumeTargetTx(tx, run)
	if err != nil {
		return err
	}
	switch action {
	case OperationActionResume:
		// The first claim fences the Run to a new generation after acceptance;
		// the checkpoint itself is still valid when it was committed by the
		// accepted generation. loadRecoveryFacts already proves it belongs to
		// this Run and is not newer than the current generation.
		checkpointCurrent := facts.Checkpoint.State == RecoveryCheckpointValid
		if !checkpointCurrent && !approvalTarget {
			return fmt.Errorf("%w: resume requires a complete checkpoint or explicit Approval target", ErrOperationPrecondition)
		}
	case OperationActionReplay:
		if facts.Checkpoint.State != RecoveryCheckpointMissing || facts.HasPublishedApproval || facts.HasEffect || strings.TrimSpace(facts.ImmutableQuery) == "" {
			return fmt.Errorf("%w: replay dependencies are not clean", ErrOperationPrecondition)
		}
	case OperationActionRestore:
		if run.Status != RunStatusParked || run.ParkReason == nil || *run.ParkReason != ParkReasonRuntimeIncompatible {
			return fmt.Errorf("%w: restore requires runtime_incompatible parked Run", ErrOperationPrecondition)
		}
		if run.RuntimeCompatibilityHash == nil || expectedCompatibilityHash != *run.RuntimeCompatibilityHash {
			return fmt.Errorf("%w: runtime compatibility hash changed", ErrOperationPrecondition)
		}
		resumeValid := facts.Checkpoint.State == RecoveryCheckpointValid || approvalTarget
		replayValid := facts.Checkpoint.State == RecoveryCheckpointMissing && !facts.HasPublishedApproval && !facts.HasEffect && strings.TrimSpace(facts.ImmutableQuery) != ""
		if !resumeValid && !replayValid {
			return fmt.Errorf("%w: restore recovery dependencies are invalid", ErrOperationPrecondition)
		}
	default:
		return ErrInvalidOperationInput
	}
	return nil
}

func hasExplicitApprovalResumeTargetTx(tx *gorm.DB, run *mysql.WorkflowRun) (bool, error) {
	var approval mysql.AgentApproval
	result := tx.Where("run_id = ? AND status IN ?", run.ID, []string{ApprovalStatusPreparing, ApprovalStatusPending, ApprovalStatusApproved}).
		Order("created_at DESC, id DESC").First(&approval)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if result.Error != nil {
		return false, fmt.Errorf("load explicit Approval resume target: %w", result.Error)
	}
	if approval.Status == ApprovalStatusPreparing {
		if approval.CheckpointID == nil || strings.TrimSpace(*approval.CheckpointID) == "" {
			return false, nil
		}
		if _, err := lockCheckpoint(tx, run, *approval.CheckpointID); err != nil {
			if errors.Is(err, ErrApprovalCheckpointMismatch) || errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	if approval.InterruptID == nil || strings.TrimSpace(*approval.InterruptID) == "" {
		return false, nil
	}
	if err := validateApprovalCheckpointBinding(tx, run, &approval); err != nil {
		if errors.Is(err, ErrApprovalCheckpointMismatch) || errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func terminalCancelRecord(run mysql.WorkflowRun, operationID, actorID string) OperationRecord {
	status := OperationStatusCanceled
	if run.Status == RunStatusSucceeded || run.Status == RunStatusSuccess {
		status = OperationStatusSucceeded
	} else if run.Status == RunStatusFailed {
		status = OperationStatusFailed
	}
	finished := run.FinishedAt
	acceptedAt := run.UpdatedAt
	if finished != nil {
		acceptedAt = *finished
	}
	return OperationRecord{Operation: Operation{
		OperationID: operationID, RunID: run.ID, Action: OperationActionCancel,
		Status: status, Terminal: true, ActorID: actorID, AcceptedAt: acceptedAt, FinishedAt: finished,
	}}
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case RunStatusSucceeded, RunStatusSuccess, RunStatusFailed, RunStatusCanceled:
		return true
	default:
		return false
	}
}
