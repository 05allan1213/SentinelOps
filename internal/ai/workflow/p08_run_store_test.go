package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestCreateRunWithSessionLockIsAtomic(t *testing.T) {
	db := newP07Database(t, "p08_create_atomic")
	store := NewGORMStore(db)
	ctx := p08UserContext("user-create")

	run, err := store.CreateRunWithSessionLock(ctx, p08CreateInput("run-create", "session-create"))
	if err != nil {
		t.Fatalf("create durable run: %v", err)
	}
	if run.Status != RunStatusPending || run.RuntimeMode != RuntimeModeDurableV1 {
		t.Fatalf("created run status/runtime = %q/%q", run.Status, run.RuntimeMode)
	}
	if run.ActiveSessionKey == nil || *run.ActiveSessionKey != "session-create" {
		t.Fatalf("active session key = %v", run.ActiveSessionKey)
	}
	if run.SessionRevision == nil || *run.SessionRevision != 0 || run.ContextSnapshotJSON == nil {
		t.Fatalf("session snapshot = revision %v context %v", run.SessionRevision, run.ContextSnapshotJSON)
	}
	if run.LastEventSeq != 1 {
		t.Fatalf("last event seq = %d, want 1", run.LastEventSeq)
	}

	var revisionCount, eventCount int64
	if err := db.Model(&mysql.SessionStateRevision{}).Where("session_id = ? AND revision = 0", "session-create").Count(&revisionCount).Error; err != nil {
		t.Fatalf("count Revision 0: %v", err)
	}
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND seq = 1 AND event_type = ?", run.ID, EventRunCreated).Count(&eventCount).Error; err != nil {
		t.Fatalf("count run.created: %v", err)
	}
	if revisionCount != 1 || eventCount != 1 {
		t.Fatalf("atomic rows = revision %d event %d", revisionCount, eventCount)
	}
}

func TestCreateRunSameSessionReturnsConflict(t *testing.T) {
	db := newP07Database(t, "p08_session_conflict")
	store := NewGORMStore(db)
	ctx := p08UserContext("user-conflict")
	if _, err := store.CreateRunWithSessionLock(ctx, p08CreateInput("run-first", "session-conflict")); err != nil {
		t.Fatalf("create first run: %v", err)
	}
	_, err := store.CreateRunWithSessionLock(ctx, p08CreateInput("run-second", "session-conflict"))
	if !errors.Is(err, ErrSessionRunActive) {
		t.Fatalf("second create error = %v, want ErrSessionRunActive", err)
	}

	var count int64
	if err := db.Model(&mysql.WorkflowRun{}).Where("session_id = ?", "session-conflict").Count(&count).Error; err != nil {
		t.Fatalf("count session runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("session run count = %d, want 1", count)
	}
}

func TestCreateRunSameSessionConcurrentOnlyOneSucceeds(t *testing.T) {
	db := newP07Database(t, "p08_session_concurrent")
	store := NewGORMStore(db)
	ctx := p08UserContext("user-concurrent")

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for index := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.CreateRunWithSessionLock(ctx, p08CreateInput(fmt.Sprintf("run-concurrent-%d", index), "session-concurrent"))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var success, conflict int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrSessionRunActive):
			conflict++
		default:
			t.Errorf("concurrent create error = %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent results = success %d conflict %d", success, conflict)
	}
}

func TestCreateRunBootstrapsRevisionZeroAtomic(t *testing.T) {
	db := newP07Database(t, "p08_bootstrap_atomic")
	store := NewGORMStore(db)
	ctx := p08UserContext("user-bootstrap")
	if err := db.Exec(`CREATE TRIGGER reject_p08_created BEFORE INSERT ON workflow_events
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject run.created'`).Error; err != nil {
		t.Fatalf("create event failure trigger: %v", err)
	}

	_, err := store.CreateRunWithSessionLock(ctx, p08CreateInput("run-bootstrap", "session-bootstrap"))
	if err == nil {
		t.Fatal("create succeeded despite run.created failure")
	}
	for table, where := range map[string]string{
		"workflow_runs":           "id = 'run-bootstrap'",
		"workflow_events":         "run_id = 'run-bootstrap'",
		"session_state_revisions": "session_id = 'session-bootstrap'",
	} {
		var count int64
		if err := db.Table(table).Where(where).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s contains %d partial rows", table, count)
		}
	}
}

func TestTransitionRunWithEventIsAtomic(t *testing.T) {
	db := newP07Database(t, "p08_transition_atomic")
	store, ctx, run := p08CreateRun(t, db, "transition")
	if err := db.Exec(`CREATE TRIGGER reject_p08_transition BEFORE INSERT ON workflow_events
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject transition event'`).Error; err != nil {
		t.Fatalf("create transition failure trigger: %v", err)
	}

	err := store.TransitionRunWithEvent(ctx, RunTransition{
		RunID:          run.ID,
		ExpectedStatus: RunStatusPending,
		TargetStatus:   RunStatusRunning,
		Event: WorkflowEventInput{
			Type:    EventRunClaimed,
			Payload: EventPayload{Attributes: map[string]any{"source": "test"}},
		},
	})
	if err == nil {
		t.Fatal("transition succeeded despite event failure")
	}

	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read rolled-back run: %v", err)
	}
	if got.Status != RunStatusPending || got.LastEventSeq != 1 {
		t.Fatalf("transition partially committed: status %q seq %d", got.Status, got.LastEventSeq)
	}
}

func TestEventSequenceAllocatedByDatabase(t *testing.T) {
	db := newP07Database(t, "p08_event_sequence")
	store, ctx, run := p08CreateRun(t, db, "sequence")
	transitions := []RunTransition{
		{RunID: run.ID, ExpectedStatus: RunStatusPending, TargetStatus: RunStatusRunning, Event: WorkflowEventInput{Type: EventRunClaimed}},
		{RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusWaitingApproval, Event: WorkflowEventInput{Type: EventApprovalRequested}},
		{RunID: run.ID, ExpectedStatus: RunStatusWaitingApproval, TargetStatus: RunStatusPending, Intent: TransitionIntentApprovalDecided, Event: WorkflowEventInput{Type: EventApprovalDecided}},
	}
	for _, transition := range transitions {
		if err := store.TransitionRunWithEvent(ctx, transition); err != nil {
			t.Fatalf("transition %s -> %s: %v", transition.ExpectedStatus, transition.TargetStatus, err)
		}
	}

	var sequences []uint64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", run.ID).Order("seq").Pluck("seq", &sequences).Error; err != nil {
		t.Fatalf("read event sequences: %v", err)
	}
	want := []uint64{1, 2, 3, 4}
	if fmt.Sprint(sequences) != fmt.Sprint(want) {
		t.Fatalf("event sequences = %v, want %v", sequences, want)
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read run sequence: %v", err)
	}
	if got.LastEventSeq != 4 {
		t.Fatalf("last event seq = %d, want 4", got.LastEventSeq)
	}
}

func TestRunTransitionMatrix(t *testing.T) {
	allowed := []struct {
		from, to, intent, parkReason string
	}{
		{RunStatusPending, RunStatusRunning, TransitionIntentDefault, ""},
		{RunStatusRunning, RunStatusWaitingApproval, TransitionIntentDefault, ""},
		{RunStatusRunning, RunStatusRetryableFailed, TransitionIntentDefault, ""},
		{RunStatusWaitingApproval, RunStatusPending, TransitionIntentApprovalDecided, ""},
		{RunStatusRetryableFailed, RunStatusPending, TransitionIntentRetryReady, ""},
		{RunStatusRunning, RunStatusParked, TransitionIntentDefault, ParkReasonRuntimeIncompatible},
		{RunStatusWaitingApproval, RunStatusParked, TransitionIntentDefault, ParkReasonCheckpointMissing},
		{RunStatusRetryableFailed, RunStatusParked, TransitionIntentDefault, ParkReasonCheckpointCorrupt},
		{RunStatusParked, RunStatusPending, TransitionIntentRuntimeRestored, ParkReasonRuntimeIncompatible},
		{RunStatusParked, RunStatusReconciling, TransitionIntentEffectReconciliation, ParkReasonEffectUnknown},
		{RunStatusReconciling, RunStatusPending, TransitionIntentEffectResolved, ParkReasonEffectUnknown},
		{RunStatusReconciling, RunStatusParked, TransitionIntentEffectStillUnknown, ParkReasonEffectUnknown},
		{RunStatusRunning, RunStatusSucceeded, TransitionIntentDefault, ""},
	}
	for _, test := range allowed {
		if err := ValidateRunTransition(test.from, test.to, test.intent, test.parkReason); err != nil {
			t.Errorf("allowed %s -> %s (%s/%s): %v", test.from, test.to, test.intent, test.parkReason, err)
		}
	}
	for _, from := range []string{RunStatusPending, RunStatusRunning, RunStatusWaitingApproval, RunStatusRetryableFailed, RunStatusParked, RunStatusReconciling} {
		for _, to := range []string{RunStatusFailed, RunStatusCanceled} {
			if err := ValidateRunTransition(from, to, TransitionIntentDefault, ""); err != nil {
				t.Errorf("terminal %s -> %s rejected: %v", from, to, err)
			}
		}
	}

	denied := []struct {
		from, to, intent, parkReason string
	}{
		{RunStatusSucceeded, RunStatusPending, TransitionIntentDefault, ""},
		{RunStatusFailed, RunStatusRunning, TransitionIntentDefault, ""},
		{RunStatusCanceled, RunStatusPending, TransitionIntentDefault, ""},
		{RunStatusPending, RunStatusSucceeded, TransitionIntentDefault, ""},
		{RunStatusParked, RunStatusPending, TransitionIntentDefault, ParkReasonRuntimeIncompatible},
		{RunStatusParked, RunStatusPending, TransitionIntentRuntimeRestored, ParkReasonEffectUnknown},
		{RunStatusParked, RunStatusReconciling, TransitionIntentDefault, ParkReasonEffectUnknown},
		{RunStatusParked, RunStatusReconciling, TransitionIntentEffectReconciliation, ParkReasonCheckpointMissing},
		{RunStatusRunning, RunStatusParked, TransitionIntentDefault, ""},
		{"future", RunStatusRunning, TransitionIntentDefault, ""},
	}
	for _, test := range denied {
		if err := ValidateRunTransition(test.from, test.to, test.intent, test.parkReason); err == nil {
			t.Errorf("denied transition passed: %s -> %s (%s/%s)", test.from, test.to, test.intent, test.parkReason)
		}
	}

	allowedPairs := make(map[string]bool)
	for _, test := range allowed {
		allowedPairs[test.from+">"+test.to] = true
	}
	for _, from := range []string{RunStatusPending, RunStatusRunning, RunStatusWaitingApproval, RunStatusRetryableFailed, RunStatusParked, RunStatusReconciling} {
		for _, to := range []string{RunStatusFailed, RunStatusCanceled} {
			allowedPairs[from+">"+to] = true
		}
	}
	statuses := []string{
		RunStatusPending, RunStatusRunning, RunStatusWaitingApproval, RunStatusRetryableFailed,
		RunStatusParked, RunStatusReconciling, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			if allowedPairs[from+">"+to] {
				continue
			}
			if err := ValidateRunTransition(from, to, TransitionIntentDefault, ""); err == nil {
				t.Errorf("unlisted transition passed: %s -> %s", from, to)
			}
		}
	}
}

func TestVersionedEventCatalogIsComplete(t *testing.T) {
	want := []string{
		"run.created", "run.claimed", "run.resumed", "run.replayed", "run.reconciling", "run.completed", "run.failed", "run.parked",
		"agent.plan", "agent.replan", "agent.tool_call", "agent.tool_result", "agent.interrupted",
		"approval.preparing", "approval.requested", "approval.decided", "approval.expired", "approval.invalidated",
		"effect.started", "effect.succeeded", "effect.failed", "effect.unknown", "effect.reconciling", "effect.resolved",
		"budget.reserved", "budget.settled", "budget.exhausted", "budget.usage_unknown",
		"evidence.retrieved", "evidence.cited", "trace.flushed", "trace.incomplete",
	}
	got := VersionedEventCatalog()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("event catalog = %v, want %v", got, want)
	}
	if EventPayloadVersion != 1 || EventEnvelopeSchema != "sentinelops/workflow-event/v1" {
		t.Fatalf("event envelope version/schema = %d/%q", EventPayloadVersion, EventEnvelopeSchema)
	}
}

func TestTransitionRunEventPayloadIsRedactedAndBounded(t *testing.T) {
	db := newP07Database(t, "p08_event_payload")
	store, ctx, run := p08CreateRun(t, db, "payload")
	secret := "sk-1234567890abcdefghijklmnop"
	err := store.TransitionRunWithEvent(ctx, RunTransition{
		RunID: run.ID, ExpectedStatus: RunStatusPending, TargetStatus: RunStatusRunning,
		Event: WorkflowEventInput{Type: EventRunClaimed, TraceID: "trace-p08", Payload: EventPayload{Attributes: map[string]any{
			"api_key": secret,
			"note":    "safe",
		}}},
	})
	if err != nil {
		t.Fatalf("write redacted event: %v", err)
	}
	var event mysql.WorkflowEvent
	if err := db.First(&event, "run_id = ? AND seq = 2", run.ID).Error; err != nil {
		t.Fatalf("read redacted event: %v", err)
	}
	if event.PayloadVersion != EventPayloadVersion || event.TraceID != "trace-p08" || strings.Contains(event.Payload, secret) || !strings.Contains(event.Payload, "[REDACTED]") || !strings.Contains(event.Payload, "safe") {
		t.Fatalf("persisted event envelope did not redact or version correctly: version=%d trace=%q payload=%s", event.PayloadVersion, event.TraceID, event.Payload)
	}

	large := strings.Repeat("prompt body ", MaxEventTextBytes)
	err = store.TransitionRunWithEvent(ctx, RunTransition{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusWaitingApproval,
		Event: WorkflowEventInput{Type: EventApprovalRequested, Payload: EventPayload{Attributes: map[string]any{"prompt": large}}},
	})
	if !errors.Is(err, ErrEventPayloadTooLarge) {
		t.Fatalf("large event error = %v, want ErrEventPayloadTooLarge", err)
	}
	err = store.TransitionRunWithEvent(ctx, RunTransition{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusWaitingApproval,
		Event: WorkflowEventInput{Type: EventApprovalRequested, TraceID: secret},
	})
	if err == nil {
		t.Fatal("event accepted sensitive material as trace_id")
	}
}

func TestCompleteRunAndCommitSessionSucceededAtomic(t *testing.T) {
	db := newP07Database(t, "p08_complete_success")
	store, ctx, run := p08CreateRun(t, db, "complete_success")
	p08MoveToRunning(t, store, ctx, run.ID)
	revisionJSON := json.RawMessage(`{"schema":"fo/session-state/v1","summary":"completed"}`)
	secret := "sk-1234567890abcdefghijklmnop"

	if err := store.CompleteRunAndCommitSession(ctx, CompleteRunInput{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusSucceeded,
		OutputPayload: `{"answer_ref":"trace-p08","api_key":"` + secret + `"}`, TraceQuality: "complete", RevisionStateJSON: revisionJSON,
	}); err != nil {
		t.Fatalf("complete successful run: %v", err)
	}

	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read completed run: %v", err)
	}
	if got.Status != RunStatusSucceeded || got.ActiveSessionKey != nil || got.FinishedAt == nil || got.LastEventSeq != 3 || got.TraceQuality != "complete" {
		t.Fatalf("completed run = status %q active %v finished %v seq %d trace %q", got.Status, got.ActiveSessionKey, got.FinishedAt, got.LastEventSeq, got.TraceQuality)
	}
	if strings.Contains(got.OutputPayload, secret) || !strings.Contains(got.OutputPayload, "[REDACTED]") {
		t.Fatalf("completed output was not redacted: %s", got.OutputPayload)
	}
	var revision mysql.SessionStateRevision
	if err := db.First(&revision, "session_id = ? AND revision = 1", run.SessionID).Error; err != nil {
		t.Fatalf("read Revision 1: %v", err)
	}
	if !p08JSONEqual(revision.StateJSON, string(revisionJSON)) {
		t.Fatalf("Revision 1 = %s", revision.StateJSON)
	}
	var event mysql.WorkflowEvent
	if err := db.First(&event, "run_id = ? AND seq = 3", run.ID).Error; err != nil {
		t.Fatalf("read completion event: %v", err)
	}
	if event.EventType != EventRunCompleted {
		t.Fatalf("completion event type = %q", event.EventType)
	}
}

func TestCompleteRunFailureDoesNotCreateRevision(t *testing.T) {
	for _, source := range []string{RunStatusPending, RunStatusRunning, RunStatusWaitingApproval, RunStatusRetryableFailed, RunStatusParked, RunStatusReconciling} {
		for _, target := range []string{RunStatusFailed, RunStatusCanceled} {
			t.Run(source+"_to_"+target, func(t *testing.T) {
				db := newP07Database(t, "p08_complete_"+strings.ReplaceAll(source+"_"+target, "-", "_"))
				store, ctx, run := p08CreateRun(t, db, source+target)
				p08SetRunState(t, db, run.ID, source, p08ParkReason(source))

				err := store.CompleteRunAndCommitSession(ctx, CompleteRunInput{
					RunID: run.ID, ExpectedStatus: source, TargetStatus: target,
					ErrorMessage: "redacted failure",
				})
				if err != nil {
					t.Fatalf("complete %s -> %s: %v", source, target, err)
				}
				var got mysql.WorkflowRun
				if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
					t.Fatalf("read terminal run: %v", err)
				}
				if got.Status != target || got.ActiveSessionKey != nil || got.FinishedAt == nil {
					t.Fatalf("terminal run = status %q active %v finished %v", got.Status, got.ActiveSessionKey, got.FinishedAt)
				}
				var revisions int64
				if err := db.Model(&mysql.SessionStateRevision{}).Where("session_id = ?", run.SessionID).Count(&revisions).Error; err != nil {
					t.Fatalf("count revisions: %v", err)
				}
				if revisions != 1 {
					t.Fatalf("failed/canceled completion wrote revision: count %d", revisions)
				}
				var terminalEvent mysql.WorkflowEvent
				if err := db.First(&terminalEvent, "run_id = ? AND seq = 2", run.ID).Error; err != nil {
					t.Fatalf("read failed/canceled terminal event: %v", err)
				}
				if terminalEvent.EventType != EventRunFailed || terminalEvent.PayloadVersion != EventPayloadVersion {
					t.Fatalf("terminal event = type %q version %d", terminalEvent.EventType, terminalEvent.PayloadVersion)
				}
			})
		}
	}
}

func TestCompleteRunTransactionFailureRollsBackEverything(t *testing.T) {
	db := newP07Database(t, "p08_complete_rollback")
	store, ctx, run := p08CreateRun(t, db, "complete_rollback")
	p08MoveToRunning(t, store, ctx, run.ID)
	if err := db.Exec(`CREATE TRIGGER reject_p08_revision BEFORE INSERT ON session_state_revisions
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject completion revision'`).Error; err != nil {
		t.Fatalf("create revision failure trigger: %v", err)
	}

	err := store.CompleteRunAndCommitSession(ctx, CompleteRunInput{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusSucceeded,
		RevisionStateJSON: json.RawMessage(`{"schema":"fo/session-state/v1"}`),
	})
	if err == nil {
		t.Fatal("completion succeeded despite revision failure")
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read rolled-back completion: %v", err)
	}
	if got.Status != RunStatusRunning || got.ActiveSessionKey == nil || got.FinishedAt != nil || got.LastEventSeq != 2 {
		t.Fatalf("completion partially committed: status %q active %v finished %v seq %d", got.Status, got.ActiveSessionKey, got.FinishedAt, got.LastEventSeq)
	}
	var eventCount int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", run.ID).Count(&eventCount).Error; err != nil {
		t.Fatalf("count rolled-back events: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("completion event partially committed: count %d", eventCount)
	}
}

func TestLegacyFinishRunCannotTerminateDurableRun(t *testing.T) {
	db := newP07Database(t, "p08_legacy_finish_guard")
	store, ctx, run := p08CreateRun(t, db, "legacy_guard")
	if err := store.AppendEvent(ctx, StreamEvent{ID: 2, RunID: run.ID, Type: "legacy.event"}); !errors.Is(err, ErrDurablePrimitiveRequired) {
		t.Fatalf("legacy AppendEvent error = %v, want ErrDurablePrimitiveRequired", err)
	}
	if err := store.FinishRun(ctx, run.ID, RunStatusSucceeded, "", ""); !errors.Is(err, ErrDurablePrimitiveRequired) {
		t.Fatalf("legacy FinishRun error = %v, want ErrDurablePrimitiveRequired", err)
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read guarded run: %v", err)
	}
	if got.Status != RunStatusPending || got.ActiveSessionKey == nil {
		t.Fatalf("legacy FinishRun changed durable truth: status %q active %v", got.Status, got.ActiveSessionKey)
	}
}

func TestTransitionRunCannotCompleteDurableRun(t *testing.T) {
	db := newP07Database(t, "p08_transition_terminal_guard")
	store, ctx, run := p08CreateRun(t, db, "transition_terminal_guard")
	p08MoveToRunning(t, store, ctx, run.ID)
	err := store.TransitionRunWithEvent(ctx, RunTransition{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusSucceeded,
		Event: WorkflowEventInput{Type: EventRunCompleted},
	})
	if !errors.Is(err, ErrDurablePrimitiveRequired) {
		t.Fatalf("terminal TransitionRunWithEvent error = %v, want ErrDurablePrimitiveRequired", err)
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read guarded transition: %v", err)
	}
	if got.Status != RunStatusRunning || got.ActiveSessionKey == nil || got.LastEventSeq != 2 {
		t.Fatalf("terminal transition bypassed completion primitive: status=%q active=%v seq=%d", got.Status, got.ActiveSessionKey, got.LastEventSeq)
	}
}

func p08UserContext(userID string) context.Context {
	return policy.WithIdentity(context.Background(), policy.Identity{
		UserID: userID,
		Role:   policy.RoleViewer,
		Scope:  policy.Scope{UserID: userID},
	})
}

func p08CreateInput(runID, sessionID string) CreateRunInput {
	return CreateRunInput{
		ID:                       runID,
		WorkflowKey:              "p08-test",
		SessionID:                sessionID,
		QueryText:                "current task only",
		ImmutableInputJSON:       json.RawMessage(`{"query":"current task only"}`),
		RuntimeVersion:           "sentinelops-test-v1",
		RuntimeCompatibilityHash: strings.Repeat("a", 64),
		CreatedEvent: WorkflowEventInput{
			Type:    EventRunCreated,
			Payload: EventPayload{Attributes: map[string]any{"source": "p08-test"}},
		},
	}
}

func p08CreateRun(t *testing.T, db *gorm.DB, suffix string) (*GORMStore, context.Context, *mysql.WorkflowRun) {
	t.Helper()
	store := NewGORMStore(db)
	ctx := p08UserContext("user-" + suffix)
	run, err := store.CreateRunWithSessionLock(ctx, p08CreateInput("run-"+suffix, "session-"+suffix))
	if err != nil {
		t.Fatalf("create P08 run: %v", err)
	}
	return store, ctx, run
}

func p08MoveToRunning(t *testing.T, store *GORMStore, ctx context.Context, runID string) {
	t.Helper()
	if err := store.TransitionRunWithEvent(ctx, RunTransition{
		RunID: runID, ExpectedStatus: RunStatusPending, TargetStatus: RunStatusRunning,
		Event: WorkflowEventInput{Type: EventRunClaimed},
	}); err != nil {
		t.Fatalf("move run to running: %v", err)
	}
}

func p08SetRunState(t *testing.T, db *gorm.DB, runID, status, parkReason string) {
	t.Helper()
	updates := map[string]any{"status": status}
	if parkReason != "" {
		updates["park_reason"] = parkReason
	}
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", runID).Updates(updates).Error; err != nil {
		t.Fatalf("set fixture state %s: %v", status, err)
	}
}

func p08ParkReason(status string) string {
	if status == RunStatusParked || status == RunStatusReconciling {
		return ParkReasonEffectUnknown
	}
	return ""
}

func TestCompleteRunRejectsSuccessfulRevisionForFailure(t *testing.T) {
	db := newP07Database(t, "p08_failure_revision_reject")
	store, ctx, run := p08CreateRun(t, db, "failure_revision_reject")
	err := store.CompleteRunAndCommitSession(ctx, CompleteRunInput{
		RunID: run.ID, ExpectedStatus: RunStatusPending, TargetStatus: RunStatusFailed,
		RevisionStateJSON: json.RawMessage(`{"must_not":"persist"}`),
	})
	if err == nil {
		t.Fatal("failed completion accepted a success revision")
	}
}

func TestCreateRunRejectsInvalidDurableContract(t *testing.T) {
	db := newP07Database(t, "p08_invalid_contract")
	input := p08CreateInput("run-invalid", "session-invalid")
	input.RuntimeCompatibilityHash = "short"
	_, err := NewGORMStore(db).CreateRunWithSessionLock(p08UserContext("user-invalid"), input)
	if err == nil {
		t.Fatal("create accepted an invalid runtime compatibility hash")
	}
}

func TestTransitionRunCASRejectsStaleExpectedState(t *testing.T) {
	db := newP07Database(t, "p08_stale_cas")
	store, ctx, run := p08CreateRun(t, db, "stale_cas")
	err := store.TransitionRunWithEvent(ctx, RunTransition{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusWaitingApproval,
		Event: WorkflowEventInput{Type: EventApprovalRequested},
	})
	if !errors.Is(err, ErrRunCASConflict) {
		t.Fatalf("stale transition error = %v, want ErrRunCASConflict", err)
	}
	var count int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", run.ID).Count(&count).Error; err != nil {
		t.Fatalf("count events after stale CAS: %v", err)
	}
	if count != 1 {
		t.Fatalf("stale CAS wrote event: count %d", count)
	}
}

func TestCompleteRunCASRejectsStaleExpectedState(t *testing.T) {
	db := newP07Database(t, "p08_complete_stale")
	store, ctx, run := p08CreateRun(t, db, "complete_stale")
	err := store.CompleteRunAndCommitSession(ctx, CompleteRunInput{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusFailed,
	})
	if !errors.Is(err, ErrRunCASConflict) {
		t.Fatalf("stale completion error = %v, want ErrRunCASConflict", err)
	}
}

func TestCreateRunUsesLatestSessionRevisionSnapshot(t *testing.T) {
	db := newP07Database(t, "p08_latest_revision")
	latest := mysql.SessionStateRevision{SessionID: "session-latest", Revision: 4, StateJSON: `{"summary":"latest durable mysql"}`}
	if err := db.Create(&latest).Error; err != nil {
		t.Fatalf("create latest revision fixture: %v", err)
	}
	run, err := NewGORMStore(db).CreateRunWithSessionLock(p08UserContext("user-latest"), p08CreateInput("run-latest", "session-latest"))
	if err != nil {
		t.Fatalf("create from latest revision: %v", err)
	}
	if run.SessionRevision == nil || *run.SessionRevision != 4 || run.ContextSnapshotJSON == nil || !p08JSONEqual(*run.ContextSnapshotJSON, latest.StateJSON) {
		t.Fatalf("frozen latest snapshot = revision %v context %v", run.SessionRevision, run.ContextSnapshotJSON)
	}
}

func p08JSONEqual(left, right string) bool {
	var leftValue, rightValue any
	if json.Unmarshal([]byte(left), &leftValue) != nil || json.Unmarshal([]byte(right), &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func TestEventEnvelopeIsStructuredJSON(t *testing.T) {
	db := newP07Database(t, "p08_envelope")
	_, _, run := p08CreateRun(t, db, "envelope")
	var event mysql.WorkflowEvent
	if err := db.First(&event, "run_id = ? AND seq = 1", run.ID).Error; err != nil {
		t.Fatalf("read created event: %v", err)
	}
	var envelope struct {
		Schema string         `json:"schema"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(event.Payload), &envelope); err != nil {
		t.Fatalf("decode event envelope: %v", err)
	}
	if envelope.Schema != EventEnvelopeSchema || envelope.Data["source"] != "p08-test" {
		t.Fatalf("event envelope = %+v", envelope)
	}
}

func TestCompleteRunDoesNotAllowLegacySuccessSpelling(t *testing.T) {
	if err := ValidateRunTransition(RunStatusRunning, RunStatusSuccess, TransitionIntentDefault, ""); err == nil {
		t.Fatal("durable transition accepted legacy success spelling")
	}
}

func TestCompleteRunFinishedTimestampIsNotBeforeStart(t *testing.T) {
	db := newP07Database(t, "p08_finished_time")
	store, ctx, run := p08CreateRun(t, db, "finished_time")
	p08MoveToRunning(t, store, ctx, run.ID)
	if err := store.CompleteRunAndCommitSession(ctx, CompleteRunInput{
		RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusFailed,
	}); err != nil {
		t.Fatalf("complete failed run: %v", err)
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read finished time: %v", err)
	}
	if got.FinishedAt == nil || got.FinishedAt.Before(got.StartedAt.Add(-time.Millisecond)) || got.DurationMs < 0 {
		t.Fatalf("invalid completion timing: start=%s finish=%v duration=%d", got.StartedAt, got.FinishedAt, got.DurationMs)
	}
}
