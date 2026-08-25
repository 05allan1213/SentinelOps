package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"
)

func TestDerivedEffectRowsKeepStableParentAndIndependentKeys(t *testing.T) {
	db := newP07Database(t, "p24_derived_identity")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-derived", "block_ip")
	input := p24ExternalInput(run, lease, approval)
	rows, err := store.EnsureExternalEffectDAG(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%#v", rows)
	}
	primaryKey, _ := policy.EffectKey(run.ID, approval.ProposalHash, EffectStepPrimary)
	derivedKey, _ := policy.EffectKey(run.ID, approval.ProposalHash, "nginx_reload")
	if rows[0].ID != primaryKey || rows[0].EffectRole != EffectRolePrimary || rows[0].ParentEffectID != nil {
		t.Fatalf("primary=%#v", rows[0])
	}
	if rows[1].ID != derivedKey || rows[1].ID == rows[0].ID || rows[1].EffectRole != EffectRoleDerived || rows[1].ParentEffectID == nil || *rows[1].ParentEffectID != rows[0].ID {
		t.Fatalf("derived=%#v primary=%#v", rows[1], rows[0])
	}
	replayed := input
	replayed.ToolCallIDObserved = "call-after-replay"
	rowsAgain, err := store.EnsureExternalEffectDAG(ctx, replayed)
	if err != nil || len(rowsAgain) != 2 || rowsAgain[0].ID != rows[0].ID || rowsAgain[1].ID != rows[1].ID {
		t.Fatalf("replayed rows=%#v err=%v", rowsAgain, err)
	}
	var count int64
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ?", run.ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("effect count=%d err=%v", count, err)
	}
}

func TestExternalEffectRejectsUnredactedSecretBeforeLedger(t *testing.T) {
	db := newP07Database(t, "p24_unredacted_secret")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-secret", "webhook_out")
	input := p24ExternalInput(run, lease, approval)
	input.RequestRedacted = `{"password":"plaintext-p24"}`
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err == nil {
		t.Fatal("unredacted external request entered Effect Ledger")
	}
	var count int64
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ?", run.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("effect count=%d err=%v", count, err)
	}
}

func TestExternalEffectSucceededIsReusedAfterToolReturnCrash(t *testing.T) {
	db := newP07Database(t, "p24_succeeded_reuse")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-reuse", "webhook_out")
	input := p24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(30 * time.Minute), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := store.FinishExternalEffectSucceeded(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version,
		ResponseRedacted: `{"status_code":"200"}`, ExternalReference: "provider-p24-reuse",
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed := input
	replayed.ToolCallIDObserved = "call-after-tool-return-crash"
	reused, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: replayed, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(30 * time.Minute), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reused.Reused || reused.Effect.ID != committed.Effect.ID || reused.Response != committed.Response || reused.Effect.ExternalReference == nil || *reused.Effect.ExternalReference != "provider-p24-reuse" {
		t.Fatalf("committed=%#v reused=%#v", committed, reused)
	}
}

func TestDerivedEffectTransactionalPrimaryCanStartMilvusChild(t *testing.T) {
	db := newP07Database(t, "p24_transactional_derived")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-transactional-derived", "save_intelligence")
	primaryInput := p23EffectInput(run, lease, approval, "call-p24-transactional")
	primaryInput.Derived = []DerivedEffectInput{{Step: "milvus_index", EffectType: string(policy.EffectReconcilable)}}
	if _, err := store.TransitionEffectWithEvent(ctx, primaryInput, func(context.Context) (string, error) {
		return `{"id":"intel-p24-derived"}`, nil
	}); err != nil {
		t.Fatal(err)
	}
	execution := p24ExternalInput(run, lease, approval)
	execution.TargetHash = primaryInput.TargetHash
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: execution, EffectStep: "milvus_index",
		AttemptDeadline: time.Now().Add(30 * time.Minute), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Effect.EffectRole != EffectRoleDerived || started.Effect.EffectType != string(policy.EffectReconcilable) || started.Effect.Status != EffectStatusRunning {
		t.Fatalf("started derived=%#v", started.Effect)
	}
}

func TestEffectDeadlineIsBoundedByCurrentLease(t *testing.T) {
	db := newP07Database(t, "p24_deadline")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-deadline", "block_ip")
	input := p24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(2 * time.Hour), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.LeaseUntil == nil || !started.CallDeadline.Before(*run.LeaseUntil) || started.CallDeadline.After(run.LeaseUntil.Add(-10*time.Second)) {
		t.Fatalf("call_deadline=%s lease_until=%v", started.CallDeadline, run.LeaseUntil)
	}
}

func TestUnknownWindowParksRunAndKeepsSessionLockAtomically(t *testing.T) {
	db := newP07Database(t, "p24_unknown_window")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-unknown", "webhook_out")
	input := p24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(30 * time.Minute), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version,
		LastErrorRedacted: "provider result unknown", EvidenceRedacted: `{"sent":"unknown"}`,
	}); err != nil {
		t.Fatal(err)
	}
	var storedEffect mysql.AgentEffect
	if err := db.First(&storedEffect, "id = ?", started.Effect.ID).Error; err != nil {
		t.Fatal(err)
	}
	var storedRun mysql.WorkflowRun
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedEffect.Status != EffectStatusUnknown || storedRun.Status != RunStatusParked || storedRun.ParkReason == nil || *storedRun.ParkReason != ParkReasonEffectUnknown || storedRun.ActiveSessionKey == nil {
		t.Fatalf("effect=%#v run=%#v", storedEffect, storedRun)
	}
	var eventTypes []string
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type IN ?", run.ID, []string{EventEffectStarted, EventEffectUnknown, EventRunParked}).Order("seq").Pluck("event_type", &eventTypes).Error; err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(eventTypes) != fmt.Sprint([]string{EventEffectStarted, EventEffectUnknown, EventRunParked}) {
		t.Fatalf("events=%v", eventTypes)
	}
}

func TestUnknownWindowAfterSucceededEndpointLedgerFailureParksRun(t *testing.T) {
	db := newP07Database(t, "p24_success_commit_window")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-success-window", "webhook_out")
	input := p24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(30 * time.Minute), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.FinishExternalEffectSucceeded(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version + 1,
		ResponseRedacted: `{"status_code":"200"}`,
	})
	if !errors.Is(err, ErrEffectStateConflict) {
		t.Fatalf("injected ledger CAS failure=%v", err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version,
		LastErrorRedacted: "external endpoint succeeded but ledger commit is unknown",
		EvidenceRedacted:  `{"endpoint":"succeeded","ledger_commit":"unknown"}`,
	}); err != nil {
		t.Fatal(err)
	}
	var effect mysql.AgentEffect
	var storedRun mysql.WorkflowRun
	if err := db.First(&effect, "id = ?", started.Effect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if effect.Status != EffectStatusUnknown || storedRun.Status != RunStatusParked || storedRun.ActiveSessionKey == nil {
		t.Fatalf("effect=%#v run=%#v", effect, storedRun)
	}
}

func TestDerivedEffectPartialSuccessCannotBecomeOverallSuccess(t *testing.T) {
	db := newP07Database(t, "p24_partial_derived")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "p24-partial", "block_ip")
	input := p24ExternalInput(run, lease, approval)
	if _, err := store.EnsureExternalEffectDAG(ctx, input); err != nil {
		t.Fatal(err)
	}
	primary, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: EffectStepPrimary,
		AttemptDeadline: time.Now().Add(30 * time.Minute), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishExternalEffectSucceeded(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: primary.Effect.ID, ExpectedVersion: primary.Effect.Version,
		ResponseRedacted: `{"database":"confirmed","blocklist_file":"confirmed","nginx_reload":"pending"}`,
	}); err != nil {
		t.Fatal(err)
	}
	derived, err := store.StartExternalEffect(ctx, StartExternalEffectInput{
		Execution: input, EffectStep: "nginx_reload",
		AttemptDeadline: time.Now().Add(30 * time.Minute), LeaseSafetyMargin: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkExternalEffectUnknownAndPark(ctx, FinishExternalEffectInput{
		Execution: input, EffectID: derived.Effect.ID, ExpectedVersion: derived.Effect.Version,
		LastErrorRedacted: "nginx reload result unknown",
		EvidenceRedacted:  `{"database":"confirmed","blocklist_file":"confirmed","nginx_reload":"unknown"}`,
	}); err != nil {
		t.Fatal(err)
	}
	var effects []mysql.AgentEffect
	if err := db.Where("run_id = ?", run.ID).Order("effect_role DESC").Find(&effects).Error; err != nil {
		t.Fatal(err)
	}
	var storedRun mysql.WorkflowRun
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(effects) != 2 || effects[0].Status != EffectStatusSucceeded || effects[1].Status != EffectStatusUnknown || storedRun.Status != RunStatusParked {
		t.Fatalf("effects=%#v run=%#v", effects, storedRun)
	}
}

func p24ExternalInput(run *mysql.WorkflowRun, lease LeaseToken, approval *mysql.AgentApproval) ExternalEffectExecutionInput {
	entry, err := policy.LookupCatalog(approval.ToolName)
	if err != nil {
		panic(err)
	}
	steps := make([]DerivedEffectInput, 0, len(entry.EffectSteps)-1)
	for _, step := range entry.EffectSteps[1:] {
		steps = append(steps, DerivedEffectInput{Step: step, EffectType: string(policy.EffectReconcilable)})
	}
	return ExternalEffectExecutionInput{
		Lease: lease, ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ToolCallIDObserved: "call-p24", ToolName: approval.ToolName, ToolRevision: approval.ToolRevision,
		ToolSchemaHash: approval.ToolSchemaHash, TargetHash: strings.Repeat("4", 64),
		RequestRedacted: approval.ProposalJSONRedacted, PolicyHash: approval.PolicyHash,
		RuntimeCompatibilityHash: approval.RuntimeCompatibilityHash, EffectType: string(entry.EffectType),
		Attempt: run.Attempt, TraceID: "trace-p24-" + run.ID, GateAllowed: true, Derived: steps,
	}
}
