package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestClaimTwoWorkersOnlyOneOwnerGeneration(t *testing.T) {
	db := newP07Database(t, "phase09_claim_compete")
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-phase09-claim")
	if _, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput("run-phase09-claim", "session-phase09-claim")); err != nil {
		t.Fatalf("create claim fixture: %v", err)
	}

	start := make(chan struct{})
	results := make(chan fixture09ClaimResult, 2)
	var wait sync.WaitGroup
	for _, owner := range []string{"worker-a", "worker-b"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			claim, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
				Owner: owner, LeaseDuration: time.Minute,
			})
			results <- fixture09ClaimResult{claim: claim, ok: ok, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var claimed, empty int
	var winner *ClaimedRun
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent claim: %v", result.err)
		}
		if result.ok {
			claimed++
			winner = result.claim
		} else {
			empty++
		}
	}
	if claimed != 1 || empty != 1 {
		t.Fatalf("claim results = claimed %d empty %d", claimed, empty)
	}
	if winner == nil || winner.Token.Generation != 1 || winner.Token.Owner == "" {
		t.Fatalf("winning claim = %#v", winner)
	}

	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", "run-phase09-claim").Error; err != nil {
		t.Fatalf("read claimed run: %v", err)
	}
	if run.Status != RunStatusRunning || run.LeaseOwner == nil || *run.LeaseOwner != winner.Token.Owner || run.LeaseGeneration != 1 || run.Attempt != 1 {
		t.Fatalf("claimed truth = status %q owner %v generation %d attempt %d", run.Status, run.LeaseOwner, run.LeaseGeneration, run.Attempt)
	}
	var claimedEvents int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", run.ID, EventRunClaimed).Count(&claimedEvents).Error; err != nil {
		t.Fatalf("count run.claimed: %v", err)
	}
	if claimedEvents != 1 || run.LastEventSeq != 2 {
		t.Fatalf("claim events = %d last seq %d", claimedEvents, run.LastEventSeq)
	}
}

func TestClaimEventFailureRollsBackLease(t *testing.T) {
	db := newP07Database(t, "phase09_claim_atomic")
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-phase09-claim-atomic")
	if _, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput("run-phase09-claim-atomic", "session-phase09-claim-atomic")); err != nil {
		t.Fatalf("create atomic claim fixture: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_phase09_claim BEFORE INSERT ON workflow_events
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject run.claimed'`).Error; err != nil {
		t.Fatalf("create claim event failure trigger: %v", err)
	}

	claim, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-atomic", LeaseDuration: time.Minute})
	if err == nil || ok || claim != nil {
		t.Fatalf("claim with rejected event = claim %#v ok %v err %v", claim, ok, err)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", "run-phase09-claim-atomic").Error; err != nil {
		t.Fatalf("read rolled-back claim: %v", err)
	}
	if run.Status != RunStatusPending || run.LeaseOwner != nil || run.LeaseUntil != nil || run.HeartbeatAt != nil || run.LeaseGeneration != 0 || run.Attempt != 0 || run.LastEventSeq != 1 {
		t.Fatalf("claim partially committed: status=%q owner=%v until=%v heartbeat=%v generation=%d attempt=%d seq=%d",
			run.Status, run.LeaseOwner, run.LeaseUntil, run.HeartbeatAt, run.LeaseGeneration, run.Attempt, run.LastEventSeq)
	}
}

func TestHeartbeatRequiresCurrentOwnerGeneration(t *testing.T) {
	db := newP07Database(t, "phase09_heartbeat")
	store, token := fixture09ClaimRun(t, db, "heartbeat", time.Minute)

	if err := store.HeartbeatLease(context.Background(), token, time.Minute); err != nil {
		t.Fatalf("heartbeat current lease: %v", err)
	}
	for name, stale := range map[string]LeaseToken{
		"owner":      {RunID: token.RunID, Owner: "other-worker", Generation: token.Generation},
		"generation": {RunID: token.RunID, Owner: token.Owner, Generation: token.Generation + 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.HeartbeatLease(context.Background(), stale, time.Minute); !errors.Is(err, ErrLeaseLost) {
				t.Fatalf("stale heartbeat error = %v, want ErrLeaseLost", err)
			}
		})
	}
}

func TestGenerationIncreasesAfterLeaseExpiry(t *testing.T) {
	db := newP07Database(t, "phase09_generation")
	store, first := fixture09ClaimRun(t, db, "generation", time.Minute)
	fixture09ExpireLease(t, db, first.RunID)

	second, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-second", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("reclaim expired lease: %v", err)
	}
	if !ok || second == nil {
		t.Fatal("expired running lease was not reclaimed")
	}
	if second.Token.Generation <= first.Generation || second.Token.Owner == first.Owner {
		t.Fatalf("reclaimed token = %#v, first = %#v", second.Token, first)
	}
	if err := store.HeartbeatLease(context.Background(), first, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old generation heartbeat error = %v, want ErrLeaseLost", err)
	}
}

func TestClaimPredicateExcludesIncompleteAndUnavailableRuns(t *testing.T) {
	db := newP07Database(t, "phase09_claim_predicate")
	store := NewGORMStore(db)
	now := time.Now().Truncate(time.Millisecond)
	validJSON, version, hash := `{}`, "runtime-v1", fixture09Hash("a")
	future := now.Add(time.Hour)
	runs := []mysql.WorkflowRun{
		{ID: "legacy", WorkflowKey: "phase09", RuntimeMode: RuntimeModeLegacy, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: &version, RuntimeCompatibilityHash: &hash, AvailableAt: now, StartedAt: now},
		{ID: "null-input", WorkflowKey: "phase09", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, RuntimeVersion: &version, RuntimeCompatibilityHash: &hash, AvailableAt: now, StartedAt: now},
		{ID: "null-version", WorkflowKey: "phase09", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeCompatibilityHash: &hash, AvailableAt: now, StartedAt: now},
		{ID: "null-hash", WorkflowKey: "phase09", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: &version, AvailableAt: now, StartedAt: now},
		{ID: "future", WorkflowKey: "phase09", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: &version, RuntimeCompatibilityHash: &hash, AvailableAt: future, StartedAt: now},
		{ID: "eligible", WorkflowKey: "phase09", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: &version, RuntimeCompatibilityHash: &hash, AvailableAt: now, StartedAt: now},
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatalf("create claim predicate fixtures: %v", err)
	}

	claim, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-predicate", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("claim eligible run: %v", err)
	}
	if !ok || claim.Token.RunID != "eligible" {
		t.Fatalf("claim = %#v ok=%v, want eligible", claim, ok)
	}
	if next, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-predicate", LeaseDuration: time.Minute}); err != nil || ok || next != nil {
		t.Fatalf("second claim = %#v ok=%v err=%v, want empty", next, ok, err)
	}
}

func TestLeaseReapFencesExpiredOwner(t *testing.T) {
	db := newP07Database(t, "phase09_reap")
	store, token := fixture09ClaimRun(t, db, "reap", time.Minute)
	fixture09ExpireLease(t, db, token.RunID)

	count, err := store.ReapExpiredLeases(context.Background(), ReapInput{Limit: 10})
	if err != nil {
		t.Fatalf("reap expired lease: %v", err)
	}
	if count != 1 {
		t.Fatalf("reaped leases = %d, want 1", count)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatalf("read reaped run: %v", err)
	}
	if run.LeaseOwner != nil || run.LeaseUntil != nil || run.HeartbeatAt != nil || run.LeaseGeneration <= token.Generation {
		t.Fatalf("reaped lease = owner %v until %v heartbeat %v generation %d", run.LeaseOwner, run.LeaseUntil, run.HeartbeatAt, run.LeaseGeneration)
	}
	if err := store.HeartbeatLease(context.Background(), token, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("reaped owner heartbeat error = %v, want ErrLeaseLost", err)
	}
}

func TestStaleGenerationCannotWriteTruth(t *testing.T) {
	db := newP07Database(t, "phase09_stale_truth")
	store, stale := fixture09ClaimRun(t, db, "stale", time.Minute)
	fixture09ExpireLease(t, db, stale.RunID)
	current, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-current", LeaseDuration: time.Hour,
	})
	if err != nil || !ok {
		t.Fatalf("reclaim for stale test: claim=%#v ok=%v err=%v", current, ok, err)
	}

	before := fixture09ReadTruth(t, db, stale.RunID)
	transitionErr := store.TransitionRunWithEvent(fixture08UserContext("user-phase09-stale"), RunTransition{
		RunID: stale.RunID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusWaitingApproval,
		Lease: stale, Event: WorkflowEventInput{Type: EventApprovalRequested},
	})
	if !errors.Is(transitionErr, ErrLeaseLost) {
		t.Fatalf("stale transition error = %v, want ErrLeaseLost", transitionErr)
	}
	completeErr := store.CompleteRunAndCommitSession(fixture08UserContext("user-phase09-stale"), CompleteRunInput{
		RunID: stale.RunID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusFailed,
		Lease: stale,
	})
	if !errors.Is(completeErr, ErrLeaseLost) {
		t.Fatalf("stale completion error = %v, want ErrLeaseLost", completeErr)
	}
	after := fixture09ReadTruth(t, db, stale.RunID)
	if before != after {
		t.Fatalf("stale Run/Event/terminal write changed truth: before=%s after=%s", before, after)
	}

	fixture09InsertGuardedTruthFixtures(t, db, stale.RunID, current.Token.Generation)
	var callbackCalls atomic.Int32
	err = store.withFencedRunTransaction(context.Background(), stale, RunStatusRunning, func(tx *gorm.DB, _ *mysql.WorkflowRun) error {
		callbackCalls.Add(1)
		statements := []struct {
			query string
			args  []any
		}{
			{`UPDATE workflow_runs SET budget_usage_json = JSON_OBJECT('stale', TRUE) WHERE id = ?`, []any{stale.RunID}},
			{`UPDATE workflow_checkpoints SET snapshot_json = JSON_OBJECT('state', 'stale') WHERE id = 'phase09-checkpoint'`, nil},
			{`UPDATE agent_approvals SET decision_reason = 'stale' WHERE id = 'phase09-approval'`, nil},
			{`UPDATE agent_effects SET last_error = 'stale' WHERE id = 'phase09-effect'`, nil},
		}
		for _, statement := range statements {
			if err := tx.Exec(statement.query, statement.args...).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if !errors.Is(err, ErrLeaseLost) || callbackCalls.Load() != 0 {
		t.Fatalf("stale generic fence = calls %d err %v", callbackCalls.Load(), err)
	}
	fixture09AssertGuardedTruthUnchanged(t, db, stale.RunID)
}

type fixture09ClaimResult struct {
	claim *ClaimedRun
	ok    bool
	err   error
}

func fixture09ClaimRun(t *testing.T, db *gorm.DB, suffix string, duration time.Duration) (*GORMStore, LeaseToken) {
	t.Helper()
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-phase09-" + suffix)
	if _, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput("run-phase09-"+suffix, "session-phase09-"+suffix)); err != nil {
		t.Fatalf("create phase09 run: %v", err)
	}
	claim, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-" + suffix, LeaseDuration: duration,
	})
	if err != nil || !ok {
		t.Fatalf("claim phase09 run: claim=%#v ok=%v err=%v", claim, ok, err)
	}
	return store, claim.Token
}

func fixture09ExpireLease(t *testing.T, db *gorm.DB, runID string) {
	t.Helper()
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", runID).
		Update("lease_until", gorm.Expr("DATE_SUB(CURRENT_TIMESTAMP(3), INTERVAL 1 SECOND)")).Error; err != nil {
		t.Fatalf("expire phase09 lease: %v", err)
	}
}

func fixture09ReadTruth(t *testing.T, db *gorm.DB, runID string) string {
	t.Helper()
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatalf("read phase09 run truth: %v", err)
	}
	var events int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", runID).Count(&events).Error; err != nil {
		t.Fatalf("count phase09 events: %v", err)
	}
	return fmt.Sprintf("%s/%d/%d/%d/%v", run.Status, run.LastEventSeq, run.LeaseGeneration, events, run.FinishedAt)
}

func fixture09InsertGuardedTruthFixtures(t *testing.T, db *gorm.DB, runID string, generation uint64) {
	t.Helper()
	now := time.Now().Truncate(time.Millisecond)
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE workflow_runs SET budget_usage_json = JSON_OBJECT('settled', 0) WHERE id = ?`, []any{runID}},
		{`INSERT INTO workflow_checkpoints (id, run_id, checkpoint_key, snapshot_json, lease_generation, created_at, updated_at)
			VALUES ('phase09-checkpoint', ?, 'phase09', JSON_OBJECT('state', 'original'), ?, ?, ?)`, []any{runID, generation, now, now}},
		{`INSERT INTO agent_approvals
			(id, run_id, tool_name, tool_revision, tool_schema_hash, risk_level, proposal_json_redacted, proposal_hash,
			 policy_hash, runtime_compatibility_hash, requested_by, status, version, preparing_at, created_at)
			VALUES ('phase09-approval', ?, 'tool', 'v1', ?, 'L1', JSON_OBJECT('safe', TRUE), ?, ?, ?, 'user', 'preparing', 1, ?, ?)`,
			[]any{runID, fixture09Hash("1"), fixture09Hash("2"), fixture09Hash("3"), fixture09Hash("4"), now, now}},
		{`INSERT INTO agent_effects
			(id, run_id, idempotency_key, effect_role, effect_step, proposal_hash, tool_name, tool_revision, tool_schema_hash,
			 target_hash, effect_type, status, version, lease_generation, attempt, reconciliation_attempts, created_at, updated_at)
			VALUES ('phase09-effect', ?, 'phase09-idempotency', 'primary', 'primary', ?, 'tool', 'v1', ?, ?,
			 'transactional_db', 'pending', 1, ?, 0, 0, ?, ?)`,
			[]any{runID, fixture09Hash("5"), fixture09Hash("6"), fixture09Hash("7"), generation, now, now}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("insert guarded truth fixture: %v", err)
		}
	}
}

func fixture09AssertGuardedTruthUnchanged(t *testing.T, db *gorm.DB, runID string) {
	t.Helper()
	var budget string
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", runID).Pluck("budget_usage_json", &budget).Error; err != nil {
		t.Fatalf("read guarded budget: %v", err)
	}
	if budget != `{"settled": 0}` && budget != `{"settled": 0.0}` {
		t.Fatalf("budget truth = %s", budget)
	}
	var checkpoint, approval, effect int64
	if err := db.Table("workflow_checkpoints").Where("id = ? AND JSON_UNQUOTE(JSON_EXTRACT(snapshot_json, '$.state')) = 'original'", "phase09-checkpoint").Count(&checkpoint).Error; err != nil {
		t.Fatalf("read guarded checkpoint: %v", err)
	}
	if err := db.Table("agent_approvals").Where("id = ? AND decision_reason IS NULL", "phase09-approval").Count(&approval).Error; err != nil {
		t.Fatalf("read guarded approval: %v", err)
	}
	if err := db.Table("agent_effects").Where("id = ? AND last_error IS NULL", "phase09-effect").Count(&effect).Error; err != nil {
		t.Fatalf("read guarded effect: %v", err)
	}
	if checkpoint != 1 || approval != 1 || effect != 1 {
		t.Fatalf("guarded truth changed: checkpoint=%d approval=%d effect=%d", checkpoint, approval, effect)
	}
}

func fixture09Hash(char string) string {
	result := ""
	for len(result) < 64 {
		result += char
	}
	return result[:64]
}
