package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestRunDetailUsesCanonicalPhase(t *testing.T) {
	run := mysql.WorkflowRun{ID: "r", Status: workflow.RunStatusRunning, RuntimeMode: workflow.RuntimeModeDurableV1}
	attempt := &mysql.WorkflowAttempt{Status: strp(workflow.RunStatusRunning), Mode: strp("resume"), OperationID: strp("op")}
	if got := CurrentPhaseFromFacts(RunFacts{Status: run.Status, RuntimeMode: run.RuntimeMode}, AttemptFacts{Active: true, Recovery: true, Mode: "resume", OperationActive: true}, EventFacts{}); got != v1.CurrentPhaseRecovering {
		t.Fatalf("phase=%s", got)
	}
	_ = attempt
}

func TestRunDetailDoesNotExposeRawSnapshot(t *testing.T) {
	s := workflow.DurableContextSnapshot{Schema: workflow.DurableContextSnapshotSchema, History: []byte(`[{"secret":"x"}]`), BudgetLimits: []byte(`{"max_model_calls":1}`)}
	c, err := BuildContextDTO(s, 1, 2, false, "owner", policy.WithIdentity(context.Background(), policy.Identity{UserID: "owner", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "owner"}}))
	if err != nil || c.HistoryCount != 1 || c.SummaryHash == "" {
		t.Fatalf("context=%+v err=%v", c, err)
	}
}

func TestContextExpansionRequiresPermission(t *testing.T) {
	s := workflow.DurableContextSnapshot{Schema: workflow.DurableContextSnapshotSchema, DeadlineAt: timeNow()}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "other", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "other"}})
	if _, err := BuildContextDTO(s, 1, 1, true, "owner", ctx); err == nil {
		t.Fatal("expected content expansion denial")
	}
}

func TestContextHistoryParsesSessionStateObjectAndBareArray(t *testing.T) {
	object := runtimeContextTestSnapshot(`{"schema":"fo/session-state/v1","revision":4,"history":[{"role":"user","content":"first"},{"role":"assistant","content":"second"}]}`)
	array := runtimeContextTestSnapshot(`[{"role":"user","content":"first"},{"role":"assistant","content":"second"}]`)
	for name, snap := range map[string]workflow.DurableContextSnapshot{"object": object, "array": array} {
		t.Run(name, func(t *testing.T) {
			c, err := BuildContextDTO(snap, 1, 2, true, "owner", runtimeContextTestIdentity("owner"))
			if err != nil {
				t.Fatalf("include history: %v", err)
			}
			if c.HistoryCount != 2 || len(c.History) != 2 || c.HistoryTruncated || c.RedactionApplied {
				t.Fatalf("context=%+v", c)
			}
			if c.History[0].Role != "user" || c.History[0].Content != "first" || c.History[1].Content != "second" {
				t.Fatalf("history=%+v", c.History)
			}
		})
	}
}

func TestContextHistoryDefaultOmitsMessagesAndExpansionIsBoundedRedacted(t *testing.T) {
	var entries []string
	for index := 0; index < 54; index++ {
		entries = append(entries, fmt.Sprintf(`{"role":"user","content":"message-%02d"}`, index))
	}
	entries = append(entries, `{"role":"assistant","content":"Authorization: Bearer super-secret-token"}`)
	snap := runtimeContextTestSnapshot("[" + strings.Join(entries, ",") + "]")
	ctx := runtimeContextTestIdentity("owner")

	metaOnly, err := BuildContextDTO(snap, 1, 1, false, "owner", ctx)
	if err != nil || metaOnly.HistoryCount != 55 || len(metaOnly.History) != 0 || metaOnly.HistoryTruncated || metaOnly.RedactionApplied {
		t.Fatalf("metadata-only context=%+v err=%v", metaOnly, err)
	}
	expanded, err := BuildContextDTO(snap, 1, 1, true, "owner", ctx)
	if err != nil {
		t.Fatalf("expanded context: %v", err)
	}
	if expanded.HistoryCount != 55 || len(expanded.History) != 50 || !expanded.HistoryTruncated || !expanded.RedactionApplied {
		t.Fatalf("bounded history=%+v", expanded)
	}
	last := expanded.History[len(expanded.History)-1].Content
	if !strings.Contains(last, "[REDACTED]") || strings.Contains(last, "super-secret-token") {
		t.Fatalf("last content was not redacted: %q", last)
	}
}

func TestContextHistoryCapsContentRunes(t *testing.T) {
	longContent := strings.Repeat("界", 9000)
	snap := runtimeContextTestSnapshot(fmt.Sprintf(`[{"role":"user","content":%q}]`, longContent))
	c, err := BuildContextDTO(snap, 1, 1, true, "owner", runtimeContextTestIdentity("owner"))
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(c.History[0].Content)) != 8003 || !strings.HasSuffix(c.History[0].Content, "...") {
		t.Fatalf("content cap len=%d content tail=%q", len([]rune(c.History[0].Content)), c.History[0].Content[len(c.History[0].Content)-3:])
	}
}

func TestContextHistoryMalformedMessageIsLocalDegradation(t *testing.T) {
	snap := runtimeContextTestSnapshot(`[{"role":"user","content":"valid"},{"role":"tool","content":{"blob":"invalid"}},{"role":"assistant","content":"after"}]`)
	c, err := BuildContextDTO(snap, 1, 1, true, "owner", runtimeContextTestIdentity("owner"))
	if err != nil {
		t.Fatal(err)
	}
	if c.HistoryCount != 3 || len(c.History) != 2 || c.History[0].Content != "valid" || c.History[1].Content != "after" {
		t.Fatalf("history=%+v count=%d", c.History, c.HistoryCount)
	}
	if c.Availability != v1.AvailabilityPartial || c.DataQuality != v1.DataQualityUnknown || c.ReasonCode != "invalid_context_history" {
		t.Fatalf("malformed history metadata=%+v", c.ResourceMeta)
	}
}

func TestCompatibilitySeparatesThreeFingerprints(t *testing.T) {
	generation := uint64(7)
	run := mysql.WorkflowRun{ID: "run-compat", RuntimeCompatibilityHash: strp("run")}
	attempt := &mysql.WorkflowAttempt{Attempt: 2, LeaseGeneration: &generation, RunCompatibilityHash: strp("run"), CheckpointCompatibilityHash: strp("run"), ExecutingWorkerFingerprint: strp("worker")}
	worker := &mysql.RuntimeWorkerSnapshot{RuntimeCompatibilityHash: strp("worker")}
	cp := &mysql.WorkflowCheckpoint{RuntimeCompatibilityHash: strp("run")}
	c := BuildCompatibilityDTO(run, attempt, worker, cp)
	if c.AttemptFingerprint != workflow.AttemptFingerprint(run.ID, attempt.Attempt, generation) {
		t.Fatalf("attempt fingerprint=%q", c.AttemptFingerprint)
	}
	if c.ExecutingWorkerFingerprint != "worker" {
		t.Fatalf("executing worker fingerprint=%q", c.ExecutingWorkerFingerprint)
	}
	if !c.RunMatch || !c.CheckpointMatch || !c.WorkerMatch || !c.ExactRestoreAllowed || c.ResourceMeta.Availability != v1.AvailabilityAvailable {
		t.Fatalf("compat=%+v", c)
	}
}

func TestCompatibilityMissingWorkerFingerprintIsPartialNotExact(t *testing.T) {
	generation := uint64(1)
	run := mysql.WorkflowRun{RuntimeCompatibilityHash: strp("run")}
	attempt := &mysql.WorkflowAttempt{LeaseGeneration: &generation, RunCompatibilityHash: strp("run"), CheckpointCompatibilityHash: strp("run")}
	worker := &mysql.RuntimeWorkerSnapshot{RuntimeCompatibilityHash: strp("worker")}
	cp := &mysql.WorkflowCheckpoint{RuntimeCompatibilityHash: strp("run")}
	c := BuildCompatibilityDTO(run, attempt, worker, cp)
	if c.WorkerMatch || c.ExactRestoreAllowed || c.ReasonCode != "worker_fingerprint_not_observed" ||
		c.ResourceMeta.Availability != v1.AvailabilityPartial || c.ExecutingWorkerFingerprint != "" {
		t.Fatalf("legacy attempt looked exact: %+v", c)
	}
}

func TestCompatibilityWorkerFingerprintMismatchNeverExact(t *testing.T) {
	generation := uint64(1)
	run := mysql.WorkflowRun{RuntimeCompatibilityHash: strp("run")}
	attempt := &mysql.WorkflowAttempt{LeaseGeneration: &generation, RunCompatibilityHash: strp("run"), CheckpointCompatibilityHash: strp("run"), ExecutingWorkerFingerprint: strp("worker-a")}
	worker := &mysql.RuntimeWorkerSnapshot{RuntimeCompatibilityHash: strp("worker-b")}
	cp := &mysql.WorkflowCheckpoint{RuntimeCompatibilityHash: strp("run")}
	c := BuildCompatibilityDTO(run, attempt, worker, cp)
	if c.WorkerMatch || c.ExactRestoreAllowed || c.ExecutingWorkerFingerprint != "worker-a" {
		t.Fatalf("mismatched worker was treated exact: %+v", c)
	}
}

func TestCompatibilityCurrentWorkerSnapshotAbsentIsNotObserved(t *testing.T) {
	generation := uint64(1)
	run := mysql.WorkflowRun{RuntimeCompatibilityHash: strp("run")}
	attempt := &mysql.WorkflowAttempt{LeaseGeneration: &generation, RunCompatibilityHash: strp("run"), CheckpointCompatibilityHash: strp("run"), ExecutingWorkerFingerprint: strp("worker")}
	cp := &mysql.WorkflowCheckpoint{RuntimeCompatibilityHash: strp("run")}
	c := BuildCompatibilityDTO(run, attempt, nil, cp)
	if c.WorkerMatch || c.ExactRestoreAllowed || c.ReasonCode != "not_observed" ||
		c.ResourceMeta.Availability != v1.AvailabilityPartial || c.ExecutingWorkerFingerprint != "worker" {
		t.Fatalf("absent worker snapshot looked compatible: %+v", c)
	}
}

func TestCompatibilityCheckpointUsesPersistedCheckpointHash(t *testing.T) {
	generation := uint64(1)
	run := mysql.WorkflowRun{RuntimeCompatibilityHash: strp("run")}
	attempt := &mysql.WorkflowAttempt{LeaseGeneration: &generation, RunCompatibilityHash: strp("run"), CheckpointCompatibilityHash: strp("cp"), ExecutingWorkerFingerprint: strp("worker")}
	worker := &mysql.RuntimeWorkerSnapshot{RuntimeCompatibilityHash: strp("worker")}
	cp := &mysql.WorkflowCheckpoint{RuntimeCompatibilityHash: strp("cp")}
	if got := BuildCompatibilityDTO(run, attempt, worker, cp); !got.ExactRestoreAllowed || !got.CheckpointMatch {
		t.Fatalf("compat=%+v", got)
	}
	// A differing run hash must not invalidate an otherwise matching checkpoint.
	run.RuntimeCompatibilityHash = strp("different-run")
	got := BuildCompatibilityDTO(run, attempt, worker, cp)
	if !got.CheckpointMatch || got.RunMatch {
		t.Fatalf("fingerprints must compare independently: %+v", got)
	}
}

func TestBudgetInvalidJSONIsPartial(t *testing.T) {
	c := BuildBudgetDTO(strp("{"), strp("{}"), strp("{}"))
	if c.Availability != v1.AvailabilityPartial || c.DataQuality != v1.DataQualityUnknown || c.ReasonCode == "" {
		t.Fatalf("budget=%+v", c)
	}
}

func TestShadowGateDoesNotImplyMutationSuccess(t *testing.T) {
	if actions := AllowedRecoveryActions(string(v1.RuntimeStatusParked), v1.RuntimeCompatibilityDTO{ExactRestoreAllowed: false}); len(actions) != 0 {
		t.Fatalf("actions=%v", actions)
	}
}

func TestAllowedRecoveryActionsFollowStatus(t *testing.T) {
	if got := AllowedRecoveryActions(string(v1.RuntimeStatusSucceeded), v1.RuntimeCompatibilityDTO{ExactRestoreAllowed: true}); len(got) != 0 {
		t.Fatalf("terminal actions=%v", got)
	}
}

func strp(s string) *string { return &s }
func timeNow() time.Time    { return time.Now() }

func runtimeContextTestSnapshot(history string) workflow.DurableContextSnapshot {
	return workflow.DurableContextSnapshot{
		Schema: workflow.DurableContextSnapshotSchema, DeadlineAt: timeNow().Add(time.Hour),
		History: []byte(history), BudgetLimits: []byte(`{}`),
	}
}

func runtimeContextTestIdentity(owner string) context.Context {
	return policy.WithIdentity(context.Background(), policy.Identity{
		UserID: owner, Role: policy.RoleOperator, Scope: policy.Scope{UserID: owner},
	})
}
