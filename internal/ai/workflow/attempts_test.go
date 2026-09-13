package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestAttemptProjectionOnClaimRecoveryCompletion(t *testing.T) {
	db := newP07Database(t, "c02_attempt_lifecycle")
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-c02-attempt")
	created, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput("run-c02-attempt", "session-c02-attempt"))
	if err != nil {
		t.Fatalf("create Run: %v", err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-c02", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("claim Run: claimed=%#v ok=%v err=%v", claimed, ok, err)
	}
	row := readAttempt(t, db, created.ID, 1)
	if row.Mode == nil || *row.Mode != "fresh" || row.Status == nil || *row.Status != RunStatusRunning ||
		row.WorkerID == nil || *row.WorkerID != "worker-c02" || row.LeaseGeneration == nil || *row.LeaseGeneration != claimed.Token.Generation {
		t.Fatalf("claimed Attempt = %+v", row)
	}
	if row.ExecutingWorkerFingerprint != nil || row.TraceID != nil {
		t.Fatalf("claim fabricated observation facts: %+v", row)
	}

	if err := store.RecordRecoverySelection(ctx, RecoverySelectionRecord{Lease: claimed.Token, Mode: RecoveryModeReplay, Attempt: 1, TraceID: "trace-c02", RuntimeVersion: *claimed.Run.RuntimeVersion}); err != nil {
		t.Fatalf("record recovery: %v", err)
	}
	row = readAttempt(t, db, created.ID, 1)
	if row.Mode == nil || *row.Mode != string(RecoveryModeReplay) {
		t.Fatalf("recovery mode = %v", row.Mode)
	}

	if err := store.AttachAttemptTraceTx(ctx, claimed.Token, "trace-c02"); err != nil {
		t.Fatalf("attach trace: %v", err)
	}
	if err := store.CompleteRunAndCommitSession(ctx, CompleteRunInput{RunID: created.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusSucceeded, Lease: claimed.Token, RevisionStateJSON: []byte(`{"messages":[]}`), TraceID: "trace-c02", TraceQuality: TraceQualityComplete}); err != nil {
		t.Fatalf("complete Run: %v", err)
	}
	row = readAttempt(t, db, created.ID, 1)
	if row.Status == nil || *row.Status != RunStatusSucceeded || row.CurrentPhase == nil || *row.CurrentPhase != "completed" ||
		row.TraceID == nil || *row.TraceID != "trace-c02" || row.TraceQuality == nil || *row.TraceQuality != TraceQualityComplete || row.FinishedAt == nil {
		t.Fatalf("finished Attempt = %+v", row)
	}
}

func TestAttemptProjectionRejectsStaleGeneration(t *testing.T) {
	db := newP07Database(t, "c02_attempt_stale")
	store, stale := fixture09ClaimRun(t, db, "c02-attempt-stale", time.Minute)
	before := readAttempt(t, db, stale.RunID, 1)
	fixture09ExpireLease(t, db, stale.RunID)
	current, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-c02-current", LeaseDuration: time.Hour})
	if err != nil || !ok {
		t.Fatalf("reclaim: %#v %v %v", current, ok, err)
	}
	if err := store.AttachAttemptTraceTx(fixture08UserContext("user-phase09-c02-attempt-stale"), stale, "stale-trace"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale trace error = %v", err)
	}
	after := readAttempt(t, db, stale.RunID, 1)
	if before.TraceID != nil || after.TraceID != nil || before.LeaseGeneration == nil || after.LeaseGeneration == nil || *before.LeaseGeneration != *after.LeaseGeneration {
		t.Fatalf("stale generation changed Attempt: before=%+v after=%+v", before, after)
	}
}

func TestAttemptPersistsExecutingWorkerFingerprintOnClaim(t *testing.T) {
	db := newP07Database(t, "c02_attempt_fingerprint")
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-c02-attempt-fingerprint")
	created, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput("run-c02-attempt-fingerprint", "session-c02-attempt-fingerprint"))
	if err != nil {
		t.Fatalf("create Run: %v", err)
	}
	fingerprint := *created.RuntimeCompatibilityHash
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-c02-fingerprint", LeaseDuration: time.Minute, ExecutingWorkerFingerprint: fingerprint,
	})
	if err != nil || !ok {
		t.Fatalf("claim with fingerprint: claimed=%#v ok=%v err=%v", claimed, ok, err)
	}
	row := readAttempt(t, db, created.ID, 1)
	if row.ExecutingWorkerFingerprint == nil || *row.ExecutingWorkerFingerprint != fingerprint {
		t.Fatalf("executing worker fingerprint was not persisted: %+v", row)
	}
}

func TestAttemptClaimSkipsMismatchedWorkerFingerprint(t *testing.T) {
	db := newP07Database(t, "c02_attempt_fingerprint_mismatch")
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-c02-attempt-fingerprint-mismatch")
	created, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput("run-c02-fingerprint-mismatch", "session-c02-fingerprint-mismatch"))
	if err != nil {
		t.Fatalf("create Run: %v", err)
	}
	mismatch := strings.Repeat("b", 64)
	if *created.RuntimeCompatibilityHash == mismatch {
		t.Fatalf("fixture fingerprint collision: %q", mismatch)
	}
	// 不兼容的 Worker 必须跳过而不是抛错：抛错会让该 Run 每次都被选中并回滚，
	// 永久阻塞排在它后面的可执行 Run（head-of-line starvation）。
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-c02-mismatch", LeaseDuration: time.Minute, ExecutingWorkerFingerprint: mismatch,
	})
	if err != nil || ok || claimed != nil {
		t.Fatalf("mismatched claim=%#v ok=%v err=%v, want skipped without error", claimed, ok, err)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.LeaseOwner != nil || run.Status != RunStatusPending {
		t.Fatalf("mismatched claim mutated Run=%+v", run)
	}
	var attempts int64
	if err := db.Model(&mysql.WorkflowAttempt{}).Where("run_id = ?", created.ID).Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("mismatched claim created %d Attempt rows", attempts)
	}
	// 指纹匹配的 Worker 仍必须能够认领同一个 Run。
	matched, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-c02-matched", LeaseDuration: time.Minute, ExecutingWorkerFingerprint: *created.RuntimeCompatibilityHash,
	})
	if err != nil || !ok || matched == nil || matched.Run.ID != created.ID {
		t.Fatalf("matched claim=%#v ok=%v err=%v, want the same Run", matched, ok, err)
	}
}

func TestAttemptTraceAttachIsIdempotentAndImmutable(t *testing.T) {
	db := newP07Database(t, "c02_attempt_trace_idempotent")
	store, lease := fixture09ClaimRun(t, db, "c02-attempt-trace-idempotent", time.Minute)
	if err := db.Exec(`CREATE TRIGGER preserve_c02_attempt_updated_at BEFORE UPDATE ON workflow_attempts FOR EACH ROW SET NEW.updated_at = OLD.updated_at`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS preserve_c02_attempt_updated_at") })
	ctx := fixture08UserContext("user-phase09-c02-attempt-trace")
	if err := store.AttachAttemptTrace(ctx, lease, "trace-c02-idempotent"); err != nil {
		t.Fatalf("first trace attach: %v", err)
	}
	if err := store.AttachAttemptTrace(ctx, lease, "trace-c02-idempotent"); err != nil {
		t.Fatalf("idempotent trace attach: %v", err)
	}
	if err := store.AttachAttemptTrace(ctx, lease, "trace-c02-conflict"); !errors.Is(err, ErrRunCASConflict) {
		t.Fatalf("conflicting trace attach error=%v", err)
	}
	row := readAttempt(t, db, lease.RunID, 1)
	if row.TraceID == nil || *row.TraceID != "trace-c02-idempotent" {
		t.Fatalf("immutable trace projection=%+v", row)
	}
}

func TestAttemptProjectionRetryAndFailoverRemainSameRun(t *testing.T) {
	db := newP07Database(t, "c02_attempt_reliability")
	store, ctx, run := fixture08CreateRun(t, db, "c02-attempt-reliability")
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-c02-retry", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("claim: %#v %v %v", claimed, ok, err)
	}
	if err := store.TransitionRunWithEvent(ctx, RunTransition{RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusRetryableFailed, Lease: claimed.Token, Event: WorkflowEventInput{Type: EventRunFailed, Payload: EventPayload{Attributes: map[string]any{"retryable": true, "attempt": 1}}}}); err != nil {
		t.Fatalf("retryable transition: %v", err)
	}
	first := readAttempt(t, db, run.ID, 1)
	if first.RetryCount != nil || first.FailoverCount != nil {
		t.Fatalf("physical retry/failover was guessed: %+v", first)
	}
	second, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-c02-retry", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("reclaim: %#v %v %v", second, ok, err)
	}
	if second.Run.ID != run.ID || second.Run.Attempt != 2 {
		t.Fatalf("retry created another Run: %+v", second.Run)
	}
	var runCount int64
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Count(&runCount).Error; err != nil || runCount != 1 {
		t.Fatalf("Run count=%d err=%v", runCount, err)
	}
}

func TestAttemptReconstructionMarksMissingFactsPartial(t *testing.T) {
	db := newP07Database(t, "c02_attempt_rebuild")
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-c02-rebuild")
	run, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput("run-c02-rebuild", "session-c02-rebuild"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-c02-rebuild", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("claim: %#v %v %v", claimed, ok, err)
	}
	if err := db.Where("run_id = ?", run.ID).Delete(&mysql.WorkflowAttempt{}).Error; err != nil {
		t.Fatalf("delete projection: %v", err)
	}
	rows, quality, err := store.RebuildAttemptsFromEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(rows) != 1 || quality != "partial" {
		t.Fatalf("rebuild rows=%+v quality=%q", rows, quality)
	}
	if rows[0].WorkerID == nil || *rows[0].WorkerID != "worker-c02-rebuild" || rows[0].ExecutingWorkerFingerprint != nil || rows[0].TraceID != nil {
		t.Fatalf("rebuild fabricated/missed facts: %+v", rows[0])
	}
}

func TestAttemptReliabilityCountsOnlyCanonicalBudgetFacts(t *testing.T) {
	db := newP07Database(t, "c02_attempt_reliability_facts")
	store, ctx, lease := fixture14BudgetRun(t, db, "c02-reliability-facts", BaseBudgetLimits{MaxModelCalls: 4, MaxL0ToolCalls: 1, MaxRetryCalls: 2, MaxFailoverCalls: 2, MaxDurationMS: int64(time.Hour / time.Millisecond)}, time.Now().Add(time.Hour))
	for index, kind := range []BaseBudgetKind{BaseBudgetKindRetry, BaseBudgetKindFailover} {
		if _, err := store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{Lease: lease, Identity: fmt.Sprintf("reliability-%d", index), Kind: kind, Subject: string(kind), TraceID: fmt.Sprintf("trace-reliability-%d", index), Metadata: BaseBudgetMetadata{Phase: string(kind)}}); err != nil {
			t.Fatalf("reserve %s: %v", kind, err)
		}
	}
	model := fixture14ModelBudgetMetadata("provider/chat", "provider", "model")
	if _, err := store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{Lease: lease, Identity: "ordinary-model", Kind: BaseBudgetKindModelCall, Subject: model.CatalogRef, TraceID: "trace-model", Metadata: model}); err != nil {
		t.Fatal(err)
	}
	row := readAttempt(t, db, lease.RunID, 1)
	if row.RetryCount == nil || *row.RetryCount != 1 || row.FailoverCount == nil || *row.FailoverCount != 1 {
		t.Fatalf("canonical reliability counts = retry=%v failover=%v", row.RetryCount, row.FailoverCount)
	}
	if err := db.Where("run_id = ?", lease.RunID).Delete(&mysql.WorkflowAttempt{}).Error; err != nil {
		t.Fatal(err)
	}
	rows, quality, err := store.RebuildAttemptsFromEvents(ctx, lease.RunID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rebuild rows=%+v quality=%q err=%v", rows, quality, err)
	}
	if rows[0].RetryCount == nil || *rows[0].RetryCount != 1 || rows[0].FailoverCount == nil || *rows[0].FailoverCount != 1 {
		t.Fatalf("reconstructed reliability counts = retry=%v failover=%v", rows[0].RetryCount, rows[0].FailoverCount)
	}
}

func TestAttemptReliabilityIdempotentAndExhaustedDoNotDoubleCount(t *testing.T) {
	db := newP07Database(t, "c02_attempt_reliability_idempotent")
	store, ctx, lease := fixture14BudgetRun(t, db, "c02-reliability-idempotent", BaseBudgetLimits{MaxModelCalls: 1, MaxL0ToolCalls: 1, MaxRetryCalls: 1, MaxFailoverCalls: 1, MaxDurationMS: int64(time.Hour / time.Millisecond)}, time.Now().Add(time.Hour))
	input := ReserveBaseBudgetInput{Lease: lease, Identity: "retry-once", Kind: BaseBudgetKindRetry, Subject: "retry", Metadata: BaseBudgetMetadata{Phase: string(BaseBudgetKindRetry)}}
	if _, err := store.ReserveBaseBudget(ctx, input); err != nil {
		t.Fatalf("reserve retry: %v", err)
	}
	if _, err := store.ReserveBaseBudget(ctx, input); err != nil {
		t.Fatalf("idempotent retry reserve: %v", err)
	}
	if _, err := store.ReserveBaseBudget(ctx, ReserveBaseBudgetInput{Lease: lease, Identity: "retry-rejected", Kind: BaseBudgetKindRetry, Subject: "retry", Metadata: BaseBudgetMetadata{Phase: string(BaseBudgetKindRetry)}}); !errors.Is(err, ErrBaseBudgetExhausted) {
		t.Fatalf("exhausted retry error=%v", err)
	}
	row := readAttempt(t, db, lease.RunID, 1)
	if row.RetryCount == nil || *row.RetryCount != 1 {
		t.Fatalf("retry count=%v", row.RetryCount)
	}
}

func TestAttemptReaperCloseIsFencedAndRollsBack(t *testing.T) {
	db := newP07Database(t, "c02_attempt_reaper_rollback")
	store, token := fixture09ClaimRun(t, db, "c02-reaper-rollback", time.Minute)
	fixture09ExpireLease(t, db, token.RunID)
	if err := db.Exec(`CREATE TRIGGER reject_c02_attempt_reaper BEFORE UPDATE ON workflow_attempts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject c02 attempt reaper'`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_c02_attempt_reaper") })
	if _, err := store.ReapExpiredLeases(context.Background(), ReapInput{Limit: 1}); err == nil {
		t.Fatal("reaper succeeded despite Attempt close failure")
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.LeaseGeneration != token.Generation || run.LeaseOwner == nil {
		t.Fatalf("reaper partially committed Run=%+v", run)
	}
	row := readAttempt(t, db, token.RunID, 1)
	if row.FinishedAt != nil {
		t.Fatalf("reaper rollback left finished Attempt=%+v", row)
	}
}

func TestExpiredAttemptHelperRejectsGenerationMismatch(t *testing.T) {
	db := newP07Database(t, "c02_attempt_reaper_stale")
	_, token := fixture09ClaimRun(t, db, "c02-reaper-stale", time.Minute)
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	run.LeaseGeneration++
	err := db.Transaction(func(tx *gorm.DB) error {
		return finishAttemptAfterLeaseExpiryTx(tx, &run, token.Generation, RunStatusFailed, "failed", "lease_expired", "stale")
	})
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale helper error=%v", err)
	}
	row := readAttempt(t, db, token.RunID, 1)
	if row.FinishedAt != nil {
		t.Fatalf("stale helper changed Attempt=%+v", row)
	}
}

func readAttempt(t *testing.T, db *gorm.DB, runID string, attempt uint) mysql.WorkflowAttempt {
	t.Helper()
	var row mysql.WorkflowAttempt
	if err := db.Where("run_id = ? AND attempt = ?", runID, attempt).First(&row).Error; err != nil {
		t.Fatalf("read Attempt %s/%d: %v", runID, attempt, err)
	}
	return row
}
