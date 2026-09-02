package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

// fixtureC06StartedOperation leaves an accepted command at the Worker
// safe-point boundary.  Completion tests can therefore exercise the exact
// operation.started -> CompleteRunAndCommitSession path.
func fixtureC06StartedOperation(t *testing.T, db *gorm.DB, suffix string) (*GORMStore, context.Context, RecoveryOperationClaim) {
	t.Helper()
	store, token := fixture09ClaimRun(t, db, "c06-"+suffix, time.Hour)
	ctx := fixture08UserContext("user-phase09-c06-" + suffix)
	accepted, err := store.AcceptRecoveryOperation(fixtureOperationAdminContext("admin-c06"), fixtureAcceptRecoveryInput(token.RunID, OperationActionReplay, token.Generation, "c06-key-"+suffix+"-0001"))
	if err != nil {
		t.Fatalf("accept recovery operation: %v", err)
	}
	claim, ok, err := store.ClaimNextRecoveryOperation(context.Background(), token.Owner, time.Hour, "")
	if err != nil || !ok || claim == nil {
		t.Fatalf("claim recovery operation: claim=%#v ok=%v err=%v", claim, ok, err)
	}
	if claim.Operation.OperationID != accepted.OperationID {
		t.Fatalf("claimed operation = %q, want %q", claim.Operation.OperationID, accepted.OperationID)
	}
	if err := store.StartRecoveryOperation(ctx, *claim); err != nil {
		t.Fatalf("start recovery operation: %v", err)
	}
	return store, ctx, *claim
}

func TestC06FinishRecoveryOperationCommitsTerminalCorrelation(t *testing.T) {
	db := newP07Database(t, "c06_completion_success")
	store, ctx, claim := fixtureC06StartedOperation(t, db, "success")
	if err := store.FinishRecoveryOperationWithResult(ctx, claim, FinishRecoveryOperationResult{
		Status:            OperationStatusSucceeded,
		OutputPayload:     `{"answer":"replayed"}`,
		RevisionStateJSON: json.RawMessage(`{"schema":"fo/session-state/v1","summary":"replayed"}`),
		TraceQuality:      TraceQualityComplete,
		TraceID:           "trace-c06-success",
	}); err != nil {
		t.Fatalf("finish recovery operation: %v", err)
	}

	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", claim.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSucceeded || run.ActiveSessionKey != nil || run.LeaseOwner != nil {
		t.Fatalf("terminal run = status=%q active=%v owner=%v", run.Status, run.ActiveSessionKey, run.LeaseOwner)
	}
	var terminal mysql.WorkflowEvent
	if err := db.Where("run_id = ? AND operation_id = ? AND event_type = ?", run.ID, claim.Operation.OperationID, EventOperationSucceeded).First(&terminal).Error; err != nil {
		t.Fatalf("read operation terminal event: %v", err)
	}
	if terminal.CorrelationSeq == nil || *terminal.CorrelationSeq == 0 {
		t.Fatal("operation terminal event has no correlation_seq")
	}
	var correlated mysql.WorkflowEvent
	if err := db.Where("run_id = ? AND seq = ?", run.ID, *terminal.CorrelationSeq).First(&correlated).Error; err != nil {
		t.Fatal(err)
	}
	if correlated.EventType != EventRunCompleted || correlated.Seq >= terminal.Seq {
		t.Fatalf("correlation event = %q seq=%d terminal_seq=%d", correlated.EventType, correlated.Seq, terminal.Seq)
	}
	var revisions int64
	if err := db.Model(&mysql.SessionStateRevision{}).Where("session_id = ?", run.SessionID).Count(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if revisions != 2 {
		t.Fatalf("session revisions=%d, want bootstrap plus committed revision", revisions)
	}
	var attempt mysql.WorkflowAttempt
	if err := db.Where("run_id = ? AND attempt = ?", run.ID, run.Attempt).First(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if attempt.FinishedAt == nil || valueOrEmpty(attempt.Status) != RunStatusSucceeded {
		t.Fatalf("attempt projection = finished=%v status=%q", attempt.FinishedAt, valueOrEmpty(attempt.Status))
	}
}

func TestC06FinishRecoveryOperationRollbackOnTerminalEventFailure(t *testing.T) {
	db := newP07Database(t, "c06_completion_rollback")
	store, ctx, claim := fixtureC06StartedOperation(t, db, "rollback")
	if err := db.Exec(`CREATE TRIGGER reject_c06_operation_terminal BEFORE INSERT ON workflow_events
		FOR EACH ROW BEGIN
			IF NEW.event_type = 'operation.failed' THEN
				SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject c06 operation terminal';
			END IF;
		END`).Error; err != nil {
		t.Fatalf("create terminal failure trigger: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_c06_operation_terminal") })

	if err := store.FinishRecoveryOperationWithResult(ctx, claim, FinishRecoveryOperationResult{
		Status:    OperationStatusFailed,
		ErrorCode: "recovery_failed",
		Reason:    "terminal_write_rejected",
	}); err == nil {
		t.Fatal("finish succeeded despite operation terminal insert failure")
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", claim.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusRunning || run.ActiveSessionKey == nil || run.FinishedAt != nil {
		t.Fatalf("run partially committed: status=%q active=%v finished=%v", run.Status, run.ActiveSessionKey, run.FinishedAt)
	}
	var terminalCount int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type IN ?", run.ID, []string{EventRunFailed, EventOperationFailed}).Count(&terminalCount).Error; err != nil {
		t.Fatal(err)
	}
	if terminalCount != 0 {
		t.Fatalf("terminal events after rollback=%d", terminalCount)
	}
	var attempt mysql.WorkflowAttempt
	if err := db.Where("run_id = ? AND attempt = ?", run.ID, run.Attempt).First(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if attempt.FinishedAt != nil {
		t.Fatalf("attempt finished despite rollback: %v", attempt.FinishedAt)
	}
}

func TestC06FinishRecoveryOperationNonRunningCancelWithoutAttempt(t *testing.T) {
	db := newP07Database(t, "c06_completion_cancel_pending")
	store, ctx, run := fixture08CreateRun(t, db, "c06-cancel-pending")
	accepted, err := store.AcceptRecoveryOperation(fixtureOperationAdminContext("admin-c06"), fixtureAcceptRecoveryInput(run.ID, OperationActionCancel, 0, "c06-cancel-pending-0001"))
	if err != nil {
		t.Fatalf("accept pending cancel: %v", err)
	}
	claim, ok, err := store.ClaimNextRecoveryOperation(context.Background(), "worker-c06-cancel-pending", time.Hour, "")
	if err != nil || !ok || claim == nil {
		t.Fatalf("claim pending cancel: claim=%#v ok=%v err=%v", claim, ok, err)
	}
	if claim.Operation.OperationID != accepted.OperationID || claim.Run.Attempt != 0 {
		t.Fatalf("claim = op=%q attempt=%d", claim.Operation.OperationID, claim.Run.Attempt)
	}
	if err := store.StartRecoveryOperation(ctx, *claim); err != nil {
		t.Fatalf("start pending cancel: %v", err)
	}
	if err := store.FinishRecoveryOperationWithResult(ctx, *claim, FinishRecoveryOperationResult{
		Status: OperationStatusCanceled, Reason: "operator_canceled",
	}); err != nil {
		t.Fatalf("finish pending cancel: %v", err)
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != RunStatusCanceled || got.ActiveSessionKey != nil || got.LeaseOwner != nil {
		t.Fatalf("pending cancel run = status=%q active=%v owner=%v", got.Status, got.ActiveSessionKey, got.LeaseOwner)
	}
	var terminal mysql.WorkflowEvent
	if err := db.Where("run_id = ? AND operation_id = ? AND event_type = ?", run.ID, accepted.OperationID, EventOperationCanceled).First(&terminal).Error; err != nil {
		t.Fatalf("read pending cancel terminal: %v", err)
	}
}

func TestC06RecoveryClaimStartsOnceAndReclaimKeepsOneAttempt(t *testing.T) {
	db := newP07Database(t, "c06_reclaim_one_attempt")
	store, token := fixture09ClaimRun(t, db, "c06-reclaim-one-attempt", time.Hour)
	ctx := fixtureOperationAdminContext("admin-c06")
	accepted, err := store.AcceptRecoveryOperation(ctx, fixtureAcceptRecoveryInput(token.RunID, OperationActionReplay, token.Generation, "c06-reclaim-one-attempt-key"))
	if err != nil {
		t.Fatalf("accept recovery: %v", err)
	}
	first, ok, err := store.ClaimNextRecoveryOperation(context.Background(), token.Owner, time.Hour, "")
	if err != nil || !ok || first == nil {
		t.Fatalf("first claim=%#v ok=%v err=%v", first, ok, err)
	}
	if first.Operation.Status != OperationStatusRunning || first.Operation.OperationID != accepted.OperationID {
		t.Fatalf("first operation=%#v", first.Operation)
	}
	var afterFirst mysql.WorkflowRun
	if err := db.First(&afterFirst, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	var starts int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND operation_id = ? AND event_type = ?", token.RunID, accepted.OperationID, EventOperationStarted).Count(&starts).Error; err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("operation.started count=%d", starts)
	}
	var attempts int64
	if err := db.Model(&mysql.WorkflowAttempt{}).Where("run_id = ?", token.RunID).Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempt rows=%d, want original plus one recovery attempt", attempts)
	}
	fixture09ExpireLease(t, db, token.RunID)
	reclaimed, ok, err := store.ClaimNextRecoveryOperation(context.Background(), "worker-c06-second", time.Hour, "")
	if err != nil || !ok || reclaimed == nil {
		t.Fatalf("reclaim=%#v ok=%v err=%v", reclaimed, ok, err)
	}
	if reclaimed.Run.Attempt != afterFirst.Attempt || reclaimed.Operation.OperationID != accepted.OperationID {
		t.Fatalf("reclaim changed operation identity: run=%#v op=%#v", reclaimed.Run, reclaimed.Operation)
	}
	if err := db.Model(&mysql.WorkflowAttempt{}).Where("run_id = ?", token.RunID).Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("reclaim created another attempt: %d", attempts)
	}
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND operation_id = ? AND event_type = ?", token.RunID, accepted.OperationID, EventOperationStarted).Count(&starts).Error; err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("reclaim wrote duplicate operation.started: %d", starts)
	}
}

func TestC06RecoveryFirstClaimPersistsExecutingWorkerFingerprint(t *testing.T) {
	db := newP07Database(t, "c06_recovery_fingerprint")
	store, token := fixture09ClaimRun(t, db, "c06-recovery-fingerprint", time.Hour)
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	fingerprint := *run.RuntimeCompatibilityHash
	accepted, err := store.AcceptRecoveryOperation(fixtureOperationAdminContext("admin-c06"), fixtureAcceptRecoveryInput(run.ID, OperationActionReplay, token.Generation, "c06-recovery-fingerprint-key"))
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := store.ClaimNextRecoveryOperation(context.Background(), token.Owner, time.Hour, fingerprint)
	if err != nil || !ok || claim == nil || claim.Operation.OperationID != accepted.OperationID {
		t.Fatalf("recovery claim=%#v ok=%v err=%v", claim, ok, err)
	}
	recoveryAttempt := readAttempt(t, db, run.ID, run.Attempt+1)
	if recoveryAttempt.ExecutingWorkerFingerprint == nil || *recoveryAttempt.ExecutingWorkerFingerprint != fingerprint {
		t.Fatalf("recovery attempt fingerprint=%+v, want %q", recoveryAttempt.ExecutingWorkerFingerprint, fingerprint)
	}
}

func TestC06RecoveryFirstClaimRejectsMismatchedWorkerFingerprint(t *testing.T) {
	db := newP07Database(t, "c06_recovery_fingerprint_mismatch")
	store, token := fixture09ClaimRun(t, db, "c06-recovery-fingerprint-mismatch", time.Hour)
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptRecoveryOperation(fixtureOperationAdminContext("admin-c06"), fixtureAcceptRecoveryInput(run.ID, OperationActionReplay, token.Generation, "c06-recovery-mismatch-key")); err != nil {
		t.Fatal(err)
	}
	mismatch := strings.Repeat("b", 64)
	if *run.RuntimeCompatibilityHash == mismatch {
		t.Fatalf("fixture fingerprint collision: %q", mismatch)
	}
	claim, ok, err := store.ClaimNextRecoveryOperation(context.Background(), token.Owner, time.Hour, mismatch)
	if err == nil || ok || claim != nil || !errors.Is(err, ErrOperationPrecondition) {
		t.Fatalf("mismatched recovery claim=%#v ok=%v err=%v, want ErrOperationPrecondition", claim, ok, err)
	}
}

func TestC06RestoreRevalidatesDependenciesBeforeUnlock(t *testing.T) {
	db := newP07Database(t, "c06_restore_dependencies")
	store, _, run := fixture08CreateRun(t, db, "c06-restore-dependencies")
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status": RunStatusParked, "park_reason": ParkReasonRuntimeIncompatible, "lease_generation": 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	input := fixtureAcceptRecoveryInput(run.ID, OperationActionRestore, 1, "c06-restore-dependencies-key")
	input.ExpectedCompatibilityHash = *run.RuntimeCompatibilityHash
	accepted, err := store.AcceptRecoveryOperation(fixtureOperationAdminContext("admin-c06"), input)
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := store.ClaimNextRecoveryOperation(context.Background(), "worker-c06-restore", time.Hour, "")
	if err != nil || !ok || claim == nil {
		t.Fatalf("claim=%#v ok=%v err=%v", claim, ok, err)
	}
	if claim.Operation.OperationID != accepted.OperationID {
		t.Fatalf("claim operation=%q", claim.Operation.OperationID)
	}
	if err := db.Exec(`INSERT INTO agent_effects
		(id, run_id, idempotency_key, effect_role, effect_step, proposal_hash, tool_name, tool_revision, tool_schema_hash, target_hash, effect_type, status, version, lease_generation, attempt, created_at, updated_at)
		VALUES (?, ?, ?, 'primary', 'restore-check', ?, 'tool', 'v1', ?, ?, 'mutation', 'pending', 1, 1, 1, CURRENT_TIMESTAMP(3), CURRENT_TIMESTAMP(3))`,
		"effect-c06-restore", run.ID, "c06-restore-idempotency", strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreRecoveryOperation(fixture08UserContext("user-c06-restore-dependencies"), *claim); !errors.Is(err, ErrOperationPrecondition) {
		t.Fatalf("restore with changed dependency error=%v", err)
	}
	var stored mysql.WorkflowRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != RunStatusParked || stored.ParkReason == nil || *stored.ParkReason != ParkReasonRuntimeIncompatible {
		t.Fatalf("restore bypassed dependency check: %#v", stored)
	}
}
