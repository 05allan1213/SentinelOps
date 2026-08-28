package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestReconciliationWorkerAutomaticallyResolvesTargetState(t *testing.T) {
	tests := []struct {
		name       string
		state      effects.TargetState
		queryErr   error
		effectWant string
		runWant    string
	}{
		{name: "applied", state: effects.TargetState{Known: true, Applied: true, Response: `{"blocked":"true"}`, Evidence: map[string]any{"target": "present"}}, effectWant: workflow.EffectStatusSucceeded, runWant: workflow.RunStatusPending},
		{name: "not_applied", state: effects.TargetState{Known: true, Applied: false, Evidence: map[string]any{"target": "absent"}}, effectWant: workflow.EffectStatusPending, runWant: workflow.RunStatusPending},
		{name: "query_error", queryErr: errors.New("target query unavailable"), effectWant: workflow.EffectStatusUnknown, runWant: workflow.RunStatusParked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := fixture12NewRuntimeDatabase(t, "phase25_worker_"+tc.name)
			store, runCtx, runID, runAttempt := fixture25UnknownEffectFixture(t, db, tc.name, policy.EffectReconcilable)
			queryCalls := 0
			worker, err := NewWorker(store, WorkerConfig{
				Owner: "worker-phase25-" + tc.name, LeaseDuration: time.Minute,
				MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
				ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return nil, false, nil },
				Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
					return RunExecutionResult{}, nil
				},
				QueryEffectTargetState: func(ctx context.Context, target effects.ReconciliationTarget) (effects.TargetState, error) {
					queryCalls++
					metadata, metadataErr := effects.ExecutionMetadataFromContext(ctx)
					if metadataErr != nil || metadata.EffectKey != target.EffectKey || target.Parameters["ip"] != "192.0.2.25" {
						t.Fatalf("target=%#v metadata=%#v err=%v", target, metadata, metadataErr)
					}
					return tc.state, tc.queryErr
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			worked, err := worker.RunOnce(runCtx)
			if err != nil || !worked || queryCalls != 1 {
				t.Fatalf("RunOnce worked=%t query_calls=%d err=%v", worked, queryCalls, err)
			}
			var run mysql.WorkflowRun
			var effect mysql.AgentEffect
			if err := db.First(&run, "id = ?", runID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&effect, "run_id = ?", runID).Error; err != nil {
				t.Fatal(err)
			}
			if run.Status != tc.runWant || effect.Status != tc.effectWant || run.Attempt != runAttempt {
				t.Fatalf("run=%#v effect=%#v initial_attempt=%d", run, effect, runAttempt)
			}
			if tc.queryErr != nil && (effect.NextReconcileAt == nil || !effect.NextReconcileAt.After(time.Now())) {
				t.Fatalf("query error did not persist bounded backoff: %#v", effect)
			}
		})
	}
}

func TestReconciliationWorkerNeverClaimsNonReconcilableEffect(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase25_worker_nonreconcilable")
	store, runCtx, runID, _ := fixture25UnknownEffectFixture(t, db, "nonreconcilable", policy.EffectNonReconciliableExternal)
	queryCalls := 0
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-phase25-nonreconcilable", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return nil, false, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, nil
		},
		QueryEffectTargetState: func(context.Context, effects.ReconciliationTarget) (effects.TargetState, error) {
			queryCalls++
			return effects.TargetState{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(runCtx)
	if err != nil || worked || queryCalls != 0 {
		t.Fatalf("RunOnce worked=%t query_calls=%d err=%v", worked, queryCalls, err)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != workflow.RunStatusParked {
		t.Fatalf("non-reconcilable Run status=%s", run.Status)
	}
}

func fixture25UnknownEffectFixture(t *testing.T, db *gorm.DB, suffix string, effectType policy.EffectType) (*workflow.GORMStore, context.Context, string, uint) {
	t.Helper()
	toolName := "block_ip"
	request := `{"ip":"192.0.2.25","reason":"phase25 reconciliation"}`
	if effectType == policy.EffectNonReconciliableExternal {
		toolName = "notify_email"
		request = `{"to":"ops@example.test","subject":"phase25","body":"reconciliation"}`
	}
	store, ctx := fixture22RuntimeContext(t, db, "phase25-"+suffix, toolName, policy.RiskL2)
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := policy.LookupCatalog(toolName)
	if err != nil {
		t.Fatal(err)
	}
	proposalHash := strings.Repeat("a", 64)
	effectID, err := policy.EffectKey(attempt.Run.ID, proposalHash, workflow.EffectStepPrimary)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Create(&mysql.AgentEffect{
		ID: effectID, RunID: attempt.Run.ID, IdempotencyKey: effectID,
		EffectRole: workflow.EffectRolePrimary, EffectStep: workflow.EffectStepPrimary,
		ProposalHash: proposalHash, ToolName: entry.Name, ToolRevision: entry.Revision,
		ToolSchemaHash: entry.SchemaHash, TargetHash: strings.Repeat("b", 64),
		RequestRedacted: &request, EffectType: string(effectType), Status: workflow.EffectStatusUnknown,
		Version: 2, LeaseGeneration: attempt.Lease.Generation, Attempt: attempt.Run.Attempt,
		StartedAt: &now, FinishedAt: &now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.TransitionRunWithEvent(ctx, workflow.RunTransition{
		RunID: attempt.Run.ID, ExpectedStatus: workflow.RunStatusRunning,
		TargetStatus: workflow.RunStatusParked, ParkReason: workflow.ParkReasonEffectUnknown,
		Lease: attempt.Lease, Event: workflow.WorkflowEventInput{Type: workflow.EventRunParked},
	}); err != nil {
		t.Fatal(err)
	}
	return store, ctx, attempt.Run.ID, attempt.Run.Attempt
}
