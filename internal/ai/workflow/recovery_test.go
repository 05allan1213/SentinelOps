package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"
)

func TestRecoverySelectorFactsComeOnlyFromDurableTruth(t *testing.T) {
	db := newP07Database(t, "phase12_facts")
	store, token := fixture09ClaimRun(t, db, "phase12-facts", time.Hour)
	ctx := fixture12WorkerContext(t, token, "user-phase09-phase12-facts")

	facts, err := store.LoadRecoveryFacts(ctx, token)
	if err != nil {
		t.Fatalf("load initial recovery facts: %v", err)
	}
	if facts.RunID != token.RunID || facts.ImmutableQuery != "current task only" ||
		facts.Checkpoint.State != RecoveryCheckpointMissing || facts.HasPublishedApproval || facts.HasEffect {
		t.Fatalf("initial recovery facts = %+v", facts)
	}

	checkpointID := fixture10CheckpointID(t, token.RunID)
	payload := []byte("opaque-phase12")
	if err := store.Set(ctx, checkpointID, payload); err != nil {
		t.Fatalf("set checkpoint: %v", err)
	}
	facts, err = store.LoadRecoveryFacts(ctx, token)
	if err != nil || facts.Checkpoint.State != RecoveryCheckpointValid || facts.Checkpoint.ID != checkpointID {
		t.Fatalf("valid checkpoint facts = %+v err=%v", facts, err)
	}

	if err := db.Model(&mysql.WorkflowCheckpoint{}).Where("eino_checkpoint_id = ?", checkpointID).
		Update("payload_sha256", strings.Repeat("f", 64)).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = store.LoadRecoveryFacts(ctx, token)
	if err != nil || facts.Checkpoint.State != RecoveryCheckpointCorrupt {
		t.Fatalf("corrupt checkpoint facts = %+v err=%v", facts, err)
	}
	fixture09InsertGuardedTruthFixtures(t, db, token.RunID, token.Generation)
	if err := db.Table("agent_approvals").Where("id = ?", "phase09-approval").Updates(map[string]any{
		"status": "pending", "published_at": time.Now(), "checkpoint_id": checkpointID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	facts, err = store.LoadRecoveryFacts(ctx, token)
	if err != nil || !facts.HasPublishedApproval || !facts.HasEffect {
		t.Fatalf("published dependency facts = %+v err=%v", facts, err)
	}
}

func TestRecoverySelectorPersistsFencedResumeReplayAndParkedEvents(t *testing.T) {
	db := newP07Database(t, "phase12_events")
	store, token := fixture09ClaimRun(t, db, "phase12-events", time.Hour)
	ctx := fixture12WorkerContext(t, token, "user-phase09-phase12-events")
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	attemptTrace := "trace-recovery-attempt"

	if err := store.RecordRecoverySelection(ctx, RecoverySelectionRecord{
		Lease: token, Mode: RecoveryModeReplay, Attempt: run.Attempt, TraceID: attemptTrace,
		RuntimeVersion: *run.RuntimeVersion,
	}); err != nil {
		t.Fatalf("record replay: %v", err)
	}
	if err := store.RecordRecoverySelection(ctx, RecoverySelectionRecord{
		Lease: token, Mode: RecoveryModeResume, Attempt: run.Attempt, TraceID: attemptTrace,
		RuntimeVersion: *run.RuntimeVersion,
	}); err != nil {
		t.Fatalf("record resume: %v", err)
	}
	if err := store.ParkRecovery(ctx, RecoveryParkInput{
		Lease: token, ExpectedStatus: RunStatusRunning, Reason: ParkReasonCheckpointCorrupt,
		Attempt: run.Attempt, TraceID: attemptTrace, RuntimeVersion: *run.RuntimeVersion,
	}); err != nil {
		t.Fatalf("park recovery: %v", err)
	}

	var stored mysql.WorkflowRun
	if err := db.First(&stored, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != RunStatusParked || stored.ParkReason == nil || *stored.ParkReason != ParkReasonCheckpointCorrupt || stored.LastEventSeq != 5 {
		t.Fatalf("parked run = %+v", stored)
	}
	var events []mysql.WorkflowEvent
	if err := db.Where("run_id = ?", token.RunID).Order("seq ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{EventRunCreated, EventRunClaimed, EventRunReplayed, EventRunResumed, EventRunParked}
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d", len(events), len(want))
	}
	for index := range want {
		if events[index].EventType != want[index] {
			t.Fatalf("event[%d] = %s, want %s", index, events[index].EventType, want[index])
		}
	}
	var replayEnvelope eventEnvelope
	if err := json.Unmarshal([]byte(events[2].Payload), &replayEnvelope); err != nil {
		t.Fatal(err)
	}
	if events[2].TraceID != attemptTrace || replayEnvelope.Data["mode"] != string(RecoveryModeReplay) ||
		replayEnvelope.Data["attempt"] != float64(run.Attempt) || replayEnvelope.Data["lease_generation"] != float64(token.Generation) ||
		replayEnvelope.Data["runtime_version"] != *run.RuntimeVersion {
		t.Fatalf("structured replay event = trace %q payload %+v", events[2].TraceID, replayEnvelope)
	}

	stale := token
	stale.Generation--
	if err := store.RecordRecoverySelection(ctx, RecoverySelectionRecord{
		Lease: stale, Mode: RecoveryModeReplay, Attempt: run.Attempt, TraceID: "trace-stale", RuntimeVersion: *run.RuntimeVersion,
	}); err == nil {
		t.Fatal("stale generation recorded recovery event")
	}
}

func TestRecoverySelectorRuntimeRestoreRequiresExactHashAndValidDependency(t *testing.T) {
	db := newP07Database(t, "phase12_restore")
	store, token := fixture09ClaimRun(t, db, "phase12-restore", time.Hour)
	ctx := fixture12WorkerContext(t, token, "user-phase09-phase12-restore")
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}

	if err := store.ParkRecovery(ctx, RecoveryParkInput{
		Lease: token, ExpectedStatus: RunStatusRunning, Reason: ParkReasonRuntimeIncompatible,
		Attempt: run.Attempt, TraceID: "trace-incompatible", RuntimeVersion: *run.RuntimeVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreRuntimeCompatible(ctx, RecoveryRestoreInput{
		Lease: token, CurrentCompatibilityHash: strings.Repeat("f", 64), Mode: RecoveryModeReplay,
		Attempt: run.Attempt, TraceID: "trace-wrong", RuntimeVersion: *run.RuntimeVersion,
	}); err == nil {
		t.Fatal("non-exact runtime hash unlocked parked run")
	}
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", token.RunID).
		Update("park_reason", ParkReasonEffectUnknown).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreRuntimeCompatible(ctx, RecoveryRestoreInput{
		Lease: token, CurrentCompatibilityHash: *run.RuntimeCompatibilityHash, Mode: RecoveryModeReplay,
		Attempt: run.Attempt, TraceID: "trace-effect", RuntimeVersion: *run.RuntimeVersion,
	}); err == nil {
		t.Fatal("effect_unknown was unlocked by recovery selector")
	}
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", token.RunID).
		Update("park_reason", ParkReasonRuntimeIncompatible).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Millisecond)
	if err := db.Exec(`INSERT INTO agent_approvals
		(id, run_id, tool_name, tool_revision, tool_schema_hash, risk_level, proposal_json_redacted, proposal_hash,
		 policy_hash, runtime_compatibility_hash, requested_by, status, version, preparing_at, published_at, created_at)
		VALUES ('phase12-restore-approval', ?, 'tool', 'v1', ?, 'L1', JSON_OBJECT('safe', TRUE), ?, ?, ?,
		 'user', 'pending', 1, ?, ?, ?)`, token.RunID, fixture09Hash("phase12-schema"), fixture09Hash("phase12-proposal"),
		fixture09Hash("phase12-policy"), *run.RuntimeCompatibilityHash, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreRuntimeCompatible(ctx, RecoveryRestoreInput{
		Lease: token, CurrentCompatibilityHash: *run.RuntimeCompatibilityHash, Mode: RecoveryModeReplay,
		Attempt: run.Attempt, TraceID: "trace-dependency", RuntimeVersion: *run.RuntimeVersion,
	}); err == nil {
		t.Fatal("published Approval without checkpoint unlocked runtime_incompatible Run")
	}
	if err := db.Exec("DELETE FROM agent_approvals WHERE id = 'phase12-restore-approval'").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreRuntimeCompatible(ctx, RecoveryRestoreInput{
		Lease: token, CurrentCompatibilityHash: *run.RuntimeCompatibilityHash, Mode: RecoveryModeReplay,
		Attempt: run.Attempt, TraceID: "trace-restored", RuntimeVersion: *run.RuntimeVersion,
	}); err != nil {
		t.Fatalf("restore exact compatible replay: %v", err)
	}
	var restored mysql.WorkflowRun
	if err := db.First(&restored, "id = ?", token.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.Status != RunStatusPending || restored.ParkReason != nil || restored.LeaseOwner != nil || restored.LeaseUntil != nil {
		t.Fatalf("restored Run did not return to claimable pending: %+v", restored)
	}
}

func TestRecursiveCancelHandoffRequiresCurrentGenerationCheckpoint(t *testing.T) {
	db := newP07Database(t, "phase12_handoff")
	store, token := fixture09ClaimRun(t, db, "phase12-handoff", time.Hour)
	ctx := fixture12WorkerContext(t, token, "user-phase09-phase12-handoff")
	if err := store.RequireCommittedCheckpoint(ctx, token); err == nil {
		t.Fatal("handoff succeeded without fenced checkpoint")
	}
	payload := []byte("opaque-handoff")
	if err := store.Set(ctx, fixture10CheckpointID(t, token.RunID), payload); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireCommittedCheckpoint(ctx, token); err != nil {
		t.Fatalf("current generation checkpoint rejected: %v", err)
	}

	fixture09ExpireLease(t, db, token.RunID)
	if err := store.RequireCommittedCheckpoint(ctx, token); err == nil {
		t.Fatal("expired lease handed off checkpoint")
	}
	if err := store.Set(ctx, fixture10CheckpointID(t, token.RunID), []byte("stale")); err == nil {
		t.Fatal("lost lease wrote checkpoint after immediate cancel boundary")
	}
}

func fixture12WorkerContext(t *testing.T, token LeaseToken, userID string) context.Context {
	t.Helper()
	ctx, err := ContextWithLeaseToken(fixture08UserContext(userID), token)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
