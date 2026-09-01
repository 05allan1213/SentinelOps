package runtime

import (
	"context"
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

func TestCompatibilitySeparatesThreeFingerprints(t *testing.T) {
	run := mysql.WorkflowRun{RuntimeCompatibilityHash: strp("run")}
	attempt := &mysql.WorkflowAttempt{RunCompatibilityHash: strp("run"), CheckpointCompatibilityHash: strp("run"), ExecutingWorkerFingerprint: strp("worker")}
	worker := &mysql.RuntimeWorkerSnapshot{RuntimeCompatibilityHash: strp("worker")}
	c := BuildCompatibilityDTO(run, attempt, worker)
	if !c.RunMatch || !c.CheckpointMatch || !c.WorkerMatch || !c.ExactRestoreAllowed {
		t.Fatalf("compat=%+v", c)
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
