package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestUnknownEffectExpiredRunningExternalBecomesParked(t *testing.T) {
	db := newP07Database(t, "phase25_expired_running")
	store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "phase25-expired", "webhook_out")
	input := fixture24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{
		"lease_until": past, "heartbeat_at": past,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, ordinaryCtx, ordinaryRun := fixture08CreateRun(t, db, "phase25-reap-limit")
	fixture08MoveToRunning(t, store, ordinaryCtx, ordinaryRun.ID)
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", ordinaryRun.ID).Updates(map[string]any{
		"lease_until": past, "heartbeat_at": past,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := store.ReapExpiredLeases(context.Background(), ReapInput{Limit: 1}); err != nil || count != 1 {
		t.Fatalf("reap external effect lease count=%d err=%v", count, err)
	}
	var effect mysql.AgentEffect
	var got mysql.WorkflowRun
	if err := db.First(&effect, "id = ?", started.Effect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if effect.Status != EffectStatusUnknown || got.Status != RunStatusParked || got.ParkReason == nil || *got.ParkReason != ParkReasonEffectUnknown {
		t.Fatalf("effect=%#v run=%#v", effect, got)
	}
	attempt := readAttempt(t, db, run.ID, run.Attempt)
	if attempt.FinishedAt == nil || attempt.Status == nil || *attempt.Status != RunStatusParked || attempt.CurrentPhase == nil || *attempt.CurrentPhase != "unknown" || attempt.FailureCode == nil || *attempt.FailureCode != ParkReasonEffectUnknown {
		t.Fatalf("expired Effect Attempt=%+v", attempt)
	}
	var ordinary mysql.WorkflowRun
	if err := db.First(&ordinary, "id = ?", ordinaryRun.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ordinary.LeaseOwner == nil {
		t.Fatal("reaper exceeded the total batch limit after reconciling an external Effect")
	}
	if count, err := store.ReapExpiredLeases(context.Background(), ReapInput{Limit: 1}); err != nil || count != 1 {
		t.Fatalf("reap remaining ordinary lease count=%d err=%v", count, err)
	}
}

func TestExpiredExternalEffectAttemptFailureRollsBackRunAndEffect(t *testing.T) {
	db := newP07Database(t, "c02_expired_effect_attempt_rollback")
	store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "c02-expired-effect-rollback", "webhook_out")
	input := fixture24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{Execution: input, EffectStep: EffectStepPrimary, AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{"lease_until": past, "heartbeat_at": past}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_c02_expired_effect_attempt BEFORE UPDATE ON workflow_attempts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject expired Effect Attempt'`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReconcileExpiredRunningEffects(context.Background(), 1); err == nil {
		t.Fatal("expired Effect reconciliation succeeded despite Attempt failure")
	}
	var storedRun mysql.WorkflowRun
	var storedEffect mysql.AgentEffect
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedEffect, "id = ?", started.Effect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != RunStatusRunning || storedRun.LeaseGeneration != lease.Generation || storedEffect.Status != EffectStatusRunning || storedEffect.LeaseGeneration != lease.Generation {
		t.Fatalf("rollback Run=%+v Effect=%+v", storedRun, storedEffect)
	}
	attempt := readAttempt(t, db, run.ID, run.Attempt)
	if attempt.FinishedAt != nil {
		t.Fatalf("rollback Attempt=%+v", attempt)
	}
}

func TestUnknownEffectOnlyReconcilableCanBeClaimed(t *testing.T) {
	db := newP07Database(t, "phase25_claim_filter")
	store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "phase25-claim", "block_ip")
	input := fixture24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version,
		LastErrorRedacted: "unknown", EvidenceRedacted: `{"outcome":"unknown"}`,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{Owner: "phase25-reconciler", LeaseDuration: time.Minute})
	if err != nil || !ok || claimed.Run.ID != run.ID || claimed.Effect.Status != EffectStatusReconciling || claimed.Run.Status != RunStatusReconciling {
		t.Fatalf("claim=%#v ok=%t err=%v", claimed, ok, err)
	}
	if claimed.Token.Generation <= lease.Generation {
		t.Fatalf("generation did not advance: old=%d new=%d", lease.Generation, claimed.Token.Generation)
	}

	// A non-reconcilable effect is not eligible for an automatic claim.
	db2 := newP07Database(t, "phase25_claim_nonreconcilable")
	store2, ctx2, run2, lease2, approval2 := fixture23ApprovedEffectFixture(t, db2, "phase25-claim-nr", "notify_email")
	input2 := fixture24ExternalInput(run2, lease2, approval2)
	if _, err := store2.EnsureExternalEffectDAG(ctx2, input2); err != nil {
		t.Fatal(err)
	}
	started2, err := store2.StartExternalEffect(ctx2, StartExternalEffectInput{Execution: input2, EffectStep: EffectStepPrimary, AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := store2.MarkExternalEffectUnknownAndPark(ctx2, FinishExternalEffectInput{Execution: input2, EffectID: started2.Effect.ID, ExpectedVersion: started2.Effect.Version, LastErrorRedacted: "unknown", EvidenceRedacted: `{"outcome":"unknown"}`}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store2.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{Owner: "phase25-reconciler", LeaseDuration: time.Minute}); err != nil || ok {
		t.Fatalf("non-reconcilable claim ok=%t err=%v", ok, err)
	}
}

func TestRollbackCompatibilityReconciliationClaimsExactRuntimeVersion(t *testing.T) {
	db := newP07Database(t, "phase42_reconciliation_runtime_version")
	store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "phase42-reconcile-version", "block_ip")
	input := fixture24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version,
		LastErrorRedacted: "unknown", EvidenceRedacted: `{"outcome":"unknown"}`,
	}); err != nil {
		t.Fatal(err)
	}
	const compatibilityVersion = `{"app":"compatibility","eino":"v0.9.15","go":"go1.27.0"}`
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Update("runtime_version", compatibilityVersion).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{
		Owner: "phase42-current-reconciler", LeaseDuration: time.Minute, RuntimeVersion: *run.RuntimeVersion,
	}); err != nil || ok || claimed != nil {
		t.Fatalf("mismatched reconciliation claim=%#v ok=%t err=%v", claimed, ok, err)
	}
	claimed, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{
		Owner: "phase42-compatibility-reconciler", LeaseDuration: time.Minute, RuntimeVersion: compatibilityVersion,
	})
	if err != nil || !ok || claimed.Run.ID != run.ID {
		t.Fatalf("matching reconciliation claim=%#v ok=%t err=%v", claimed, ok, err)
	}
}

func TestReconciliationResolutionsUseEffectCASAndRunTransitions(t *testing.T) {
	tests := []struct {
		name       string
		resolution string
		effectWant string
		runWant    string
	}{
		{"executed", EffectResolutionExecuted, EffectStatusSucceeded, RunStatusPending},
		{"not-executed", EffectResolutionNotExecuted, EffectStatusPending, RunStatusPending},
		{"still-unknown", EffectResolutionStillUnknown, EffectStatusUnknown, RunStatusParked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newP07Database(t, "phase25_resolution_"+strings.ReplaceAll(tc.name, "-", "_"))
			store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "phase25-"+strings.ReplaceAll(tc.name, "-", ""), "block_ip")
			input := fixture24ExternalInput(run, lease, approval)
			if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
				t.Fatal(err)
			}
			started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{Execution: input, EffectStep: EffectStepPrimary, AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version, LastErrorRedacted: "unknown", EvidenceRedacted: `{"outcome":"unknown"}`}); err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{Owner: "phase25-reconciler", LeaseDuration: time.Minute})
			if err != nil || !ok {
				t.Fatalf("claim=%#v ok=%t err=%v", claimed, ok, err)
			}
			if claimed.Run.Attempt != run.Attempt {
				t.Fatalf("reconciliation claim consumed Run attempt: before=%d after=%d", run.Attempt, claimed.Run.Attempt)
			}
			if err := store.ResolveEffect(ctx, ResolveEffectInput{Token: claimed.Token, EffectID: started.Effect.ID, ExpectedVersion: claimed.Effect.Version, Resolution: tc.resolution, EvidenceRedacted: `{"operator":"test","state":"` + tc.name + `"}`, ResolvedBy: "admin-test"}); err != nil {
				t.Fatal(err)
			}
			var effect mysql.AgentEffect
			var got mysql.WorkflowRun
			if err := db.First(&effect, "id = ?", started.Effect.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if effect.Status != tc.effectWant || got.Status != tc.runWant {
				t.Fatalf("effect=%#v run=%#v", effect, got)
			}
			if effect.ResolutionEvidenceRedacted == nil || effect.ResolvedBy == nil {
				t.Fatalf("resolution evidence/operator missing: %#v", effect)
			}
			if tc.resolution == EffectResolutionExecuted {
				response, err := decodeEffectResponse(effect.ResponseRedacted)
				if err != nil || response != "" {
					t.Fatalf("executed reconciliation response=%q err=%v", response, err)
				}
			}
			if tc.resolution == EffectResolutionStillUnknown {
				if effect.NextReconcileAt == nil || !effect.NextReconcileAt.After(time.Now()) {
					t.Fatalf("still-unknown resolution did not set future backoff: %#v", effect)
				}
				if _, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{Owner: "phase25-reconciler-2", LeaseDuration: time.Minute}); err != nil || ok {
					t.Fatalf("backoff ignored: ok=%t err=%v", ok, err)
				}
				adminCtx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "phase25-backoff-admin", Role: policy.RoleAdmin, Scope: policy.Scope{All: true}})
				if err := store.AdminResolveEffect(adminCtx, AdminResolveEffectInput{EffectID: started.Effect.ID, Resolution: EffectResolutionExecuted, EvidenceRedacted: `{"source":"admin"}`, Owner: "phase25-backoff-admin", LeaseDuration: time.Minute}); err != nil {
					t.Fatalf("admin resolution blocked by automatic backoff: %v", err)
				}
			}
		})
	}
}

func TestReconciliationClaimRejectsStaleEffectGeneration(t *testing.T) {
	db := newP07Database(t, "phase25_stale_effect_generation")
	store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "phase25-stale-generation", "block_ip")
	input := fixture24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{Execution: input, EffectStep: EffectStepPrimary, AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version, LastErrorRedacted: "unknown", EvidenceRedacted: `{"outcome":"unknown"}`}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.AgentEffect{}).Where("id = ?", started.Effect.ID).Update("lease_generation", gorm.Expr("lease_generation - 1")).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{Owner: "phase25-reconciler", LeaseDuration: time.Minute}); err != nil || ok || claimed != nil {
		t.Fatalf("stale generation claim=%#v ok=%t err=%v", claimed, ok, err)
	}
}

func TestReconciliationClaimCarriesDerivedPrimaryResponse(t *testing.T) {
	db := newP07Database(t, "phase25_derived_primary_response")
	store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "phase25-derived-primary", "save_intelligence")
	primaryInput := fixture23EffectInput(run, lease, approval, "call-phase25-derived-primary")
	primaryInput.Derived = []DerivedEffectInput{{Step: "milvus_index", EffectType: string(policy.EffectReconcilable)}}
	const primaryResponse = `{"id":"intel-phase25-derived"}`
	if _, err := store.TransitionEffectWithEvent(ctx, primaryInput, func(context.Context) (string, error) {
		return primaryResponse, nil
	}); err != nil {
		t.Fatal(err)
	}
	execution := fixture24ExternalInput(run, lease, approval)
	execution.TargetHash = primaryInput.TargetHash
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: execution, EffectStep: "milvus_index",
		AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{
		Execution: execution, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version,
		LastErrorRedacted: "unknown", EvidenceRedacted: `{"outcome":"unknown"}`,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{
		Owner: "phase25-derived-reconciler", LeaseDuration: time.Minute,
	})
	if err != nil || !ok || claimed.PrimaryResponse != primaryResponse {
		t.Fatalf("claim=%#v ok=%t err=%v", claimed, ok, err)
	}
}

func TestAdminResolutionAcceptUnknownCancelsThroughCompletionPrimitive(t *testing.T) {
	db := newP07Database(t, "phase25_admin_accept")
	store, ctx, run, lease, approval := fixture23ApprovedEffectFixture(t, db, "phase25-admin", "notify_email")
	input := fixture24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{Execution: input, EffectStep: EffectStepPrimary, AttemptDeadline: time.Now().Add(time.Hour), LeaseSafetyMargin: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version, LastErrorRedacted: "unknown", EvidenceRedacted: `{"outcome":"unknown"}`}); err != nil {
		t.Fatal(err)
	}
	adminCtx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "phase25-admin", Role: policy.RoleAdmin, Scope: policy.Scope{All: true}})
	if err := store.AdminAcceptUnknownAndCancel(adminCtx, AdminAcceptUnknownInput{RunID: run.ID, EffectID: started.Effect.ID, Owner: "phase25-admin-resolver", LeaseDuration: time.Minute, EvidenceRedacted: `{"accepted":true}`, Reason: "provider has no query API"}); err != nil {
		t.Fatal(err)
	}
	var effect mysql.AgentEffect
	var got mysql.WorkflowRun
	if err := db.First(&effect, "id = ?", started.Effect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if effect.Status != EffectStatusUnknown || effect.Resolution == nil || *effect.Resolution != EffectResolutionAcceptedUnknown || got.Status != RunStatusCanceled || got.ActiveSessionKey != nil || got.LeaseOwner != nil || got.LeaseUntil != nil {
		t.Fatalf("effect=%#v run=%#v", effect, got)
	}
	if err := store.AdminAcceptUnknownAndCancel(ctx, AdminAcceptUnknownInput{RunID: run.ID, EffectID: started.Effect.ID, Owner: "not-admin", LeaseDuration: time.Minute}); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("non-admin accept err=%v", err)
	}
}

func TestParkReasonNonEffectNeverEntersReconciliation(t *testing.T) {
	db := newP07Database(t, "phase25_park_reason")
	store, ctx, run := fixture08CreateRun(t, db, "phase25-park-reason")
	fixture08MoveToRunning(t, store, ctx, run.ID)
	lease := fixture08CurrentLease(t, db, run.ID)
	if err := store.TransitionRunWithEvent(ctx, RunTransition{RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusParked, ParkReason: ParkReasonRuntimeIncompatible, Lease: lease, Event: WorkflowEventInput{Type: EventRunParked}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimEffectReconciliation(context.Background(), ReconciliationClaimInput{Owner: "phase25-reconciler", LeaseDuration: time.Minute}); err != nil || ok {
		t.Fatalf("runtime_incompatible claim ok=%t err=%v", ok, err)
	}
}
