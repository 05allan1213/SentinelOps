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

	"gorm.io/gorm"
)

func TestEffectIdentityIgnoresToolCallID(t *testing.T) {
	db := newP07Database(t, "p23_identity")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "identity", "create_report")
	calls := 0
	first := p23EffectInput(run, lease, approval, "call-before-replay")
	result, err := store.TransitionEffectWithEvent(ctx, first, func(callbackCtx context.Context) (string, error) {
		calls++
		bound, err := mysql.DB(callbackCtx)
		if err != nil {
			return "", err
		}
		return `{"report_id":"report-p23-identity"}`, bound.Create(&mysql.Report{
			ID: "report-p23-identity", Title: "identity", Type: "custom",
		}).Error
	})
	if err != nil {
		t.Fatalf("first Effect execution: %v", err)
	}
	replayed := first
	replayed.ToolCallIDObserved = "call-after-replay"
	reused, err := store.TransitionEffectWithEvent(ctx, replayed, func(context.Context) (string, error) {
		calls++
		return "unexpected", nil
	})
	if err != nil {
		t.Fatalf("replay Effect execution: %v", err)
	}
	if calls != 1 || !reused.Reused || result.Effect.ID != reused.Effect.ID || result.Response != reused.Response {
		t.Fatalf("identity/reuse mismatch: calls=%d first=%#v replay=%#v", calls, result, reused)
	}
	want, err := policy.EffectKey(run.ID, approval.ProposalHash, EffectStepPrimary)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect.ID != want || result.Effect.IdempotencyKey != want {
		t.Fatalf("Effect identity id=%q key=%q want=%q", result.Effect.ID, result.Effect.IdempotencyKey, want)
	}
	if result.Effect.RequestRedacted == nil || *result.Effect.RequestRedacted != `{"target":"p23"}` {
		if result.Effect.RequestRedacted == nil {
			t.Fatal("Effect request_redacted is nil")
		}
		canonical, err := policy.CanonicalToolArgumentsJSON([]byte(*result.Effect.RequestRedacted))
		if err != nil || string(canonical) != `{"target":"p23"}` {
			t.Fatalf("Effect request_redacted=%q, canonical=%q, err=%v", *result.Effect.RequestRedacted, canonical, err)
		}
	}
}

func TestTransactionalEffectCommitsLedgerDomainEventsAndDerived(t *testing.T) {
	db := newP07Database(t, "p23_atomic_success")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "atomic-success", "save_intelligence")
	input := p23EffectInput(run, lease, approval, "call-atomic-success")
	input.Derived = []DerivedEffectInput{{Step: "milvus_index", EffectType: string(policy.EffectReconcilable)}}
	result, err := store.TransitionEffectWithEvent(ctx, input, func(callbackCtx context.Context) (string, error) {
		bound, err := mysql.DB(callbackCtx)
		if err != nil {
			return "", err
		}
		return `{"id":"intel-p23"}`, bound.Create(&mysql.Event{
			ID: "intel-p23", Title: "atomic intelligence", EventType: "web", Status: "new", Metadata: `{}`,
		}).Error
	})
	if err != nil {
		t.Fatalf("transactional Effect: %v", err)
	}
	if result.Reused || result.Effect.Status != EffectStatusSucceeded || result.Effect.Version != 3 || result.Effect.LeaseGeneration != lease.Generation {
		t.Fatalf("Primary Effect result=%#v", result)
	}
	var domainCount, primaryCount, derivedCount int64
	if err := db.Model(&mysql.Event{}).Where("id = ?", "intel-p23").Count(&domainCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ? AND effect_role = ? AND effect_step = ?", run.ID, EffectRolePrimary, EffectStepPrimary).Count(&primaryCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ? AND effect_role = ? AND effect_step = ? AND parent_effect_id = ?", run.ID, EffectRoleDerived, "milvus_index", result.Effect.ID).Count(&derivedCount).Error; err != nil {
		t.Fatal(err)
	}
	var eventTypes []string
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type LIKE ?", run.ID, "effect.%").Order("seq").Pluck("event_type", &eventTypes).Error; err != nil {
		t.Fatal(err)
	}
	if domainCount != 1 || primaryCount != 1 || derivedCount != 1 || fmt.Sprint(eventTypes) != fmt.Sprint([]string{EventEffectStarted, EventEffectSucceeded}) {
		t.Fatalf("atomic truth domain=%d primary=%d derived=%d events=%v", domainCount, primaryCount, derivedCount, eventTypes)
	}
}

func TestEffectRollbackLeavesNoPartialTruthAndNeverUnknown(t *testing.T) {
	db := newP07Database(t, "p23_rollback")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "rollback", "create_report")
	injected := errors.New("injected transactional domain failure")
	_, err := store.TransitionEffectWithEvent(ctx, p23EffectInput(run, lease, approval, "call-rollback"), func(callbackCtx context.Context) (string, error) {
		bound, bindErr := mysql.DB(callbackCtx)
		if bindErr != nil {
			return "", bindErr
		}
		if createErr := bound.Create(&mysql.Report{ID: "report-p23-rollback", Title: "rollback", Type: "custom"}).Error; createErr != nil {
			return "", createErr
		}
		return "", injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("rollback error=%v, want injected failure", err)
	}
	var reportCount, effectCount, unknownCount, effectEventCount int64
	_ = db.Model(&mysql.Report{}).Where("id = ?", "report-p23-rollback").Count(&reportCount).Error
	_ = db.Model(&mysql.AgentEffect{}).Where("run_id = ?", run.ID).Count(&effectCount).Error
	_ = db.Model(&mysql.AgentEffect{}).Where("run_id = ? AND status = ?", run.ID, EffectStatusUnknown).Count(&unknownCount).Error
	_ = db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type LIKE ?", run.ID, "effect.%").Count(&effectEventCount).Error
	if reportCount != 0 || effectCount != 0 || unknownCount != 0 || effectEventCount != 0 {
		t.Fatalf("rollback leaked report=%d effects=%d unknown=%d events=%d", reportCount, effectCount, unknownCount, effectEventCount)
	}
}

func TestReuseSucceededSkipsDomainWriteAndRejectsStaleGuards(t *testing.T) {
	db := newP07Database(t, "p23_reuse_guards")
	store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, "reuse-guards", "update_event_status")
	input := p23EffectInput(run, lease, approval, "call-reuse")
	calls := 0
	if _, err := store.TransitionEffectWithEvent(ctx, input, func(context.Context) (string, error) {
		calls++
		return `{"status":"resolved"}`, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionEffectWithEvent(ctx, input, func(context.Context) (string, error) {
		calls++
		return "unexpected", nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("succeeded replay endpoint calls=%d, want 1", calls)
	}
	stale := input
	stale.Lease.Generation++
	if _, err := store.TransitionEffectWithEvent(ctx, stale, func(context.Context) (string, error) { return "", nil }); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale generation error=%v, want ErrLeaseLost", err)
	}
	gateClosed := input
	gateClosed.GateAllowed = false
	if _, err := store.TransitionEffectWithEvent(ctx, gateClosed, func(context.Context) (string, error) { return "", nil }); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("closed Gate error=%v, want policy.ErrForbidden", err)
	}
}

func TestTransactionalEffectRejectsUnredactedRequest(t *testing.T) {
	for name, request := range map[string]string{
		"unredacted":     `{"password":"plaintext"}`,
		"embeddedsecret": `{"note":"Authorization: Bearer plaintext-token"}`,
	} {
		t.Run(name, func(t *testing.T) {
			db := newP07Database(t, "p23_request_validation_"+name)
			store, ctx, run, lease, approval := p23ApprovedEffectFixture(t, db, name, "create_report")
			input := p23EffectInput(run, lease, approval, "call-"+name)
			input.RequestRedacted = request
			callbackCalled := false
			_, err := store.TransitionEffectWithEvent(ctx, input, func(context.Context) (string, error) {
				callbackCalled = true
				return "unexpected", nil
			})
			if err == nil || callbackCalled {
				t.Fatalf("request validation error=%v callbackCalled=%t", err, callbackCalled)
			}
		})
	}
}

func p23ApprovedEffectFixture(t *testing.T, db *gorm.DB, suffix, toolName string) (*GORMStore, context.Context, *mysql.WorkflowRun, LeaseToken, *mysql.AgentApproval) {
	t.Helper()
	store, requesterCtx, run, initialLease := p22RunningRun(t, db, suffix)
	entry, err := policy.LookupCatalog(toolName)
	if err != nil {
		t.Fatal(err)
	}
	proposalHash := fmt.Sprintf("%064x", len(run.ID)+len(toolName)+23)
	approval, err := store.PrepareApproval(requesterCtx, PrepareApprovalInput{
		Lease: initialLease, ToolCallIDObserved: "call-prepare-" + suffix,
		ToolName: entry.Name, ToolRevision: entry.Revision, ToolSchemaHash: entry.SchemaHash,
		RiskLevel: entry.Risk, ProposalJSONRedacted: `{"target":"p23"}`,
		ProposalHash: proposalHash, PolicyHash: strings.Repeat("d", 64),
		RuntimeCompatibilityHash: *run.RuntimeCompatibilityHash, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("prepare P23 Approval: %v", err)
	}
	p22Publish(t, store, requesterCtx, run.ID, initialLease, approval)
	approverCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "approver-p23-" + suffix, Role: policy.RoleApprover,
		Scope: policy.Scope{UserID: "approver-p23-" + suffix},
	})
	approval, err = store.DecideApprovalAndWakeRun(approverCtx, DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
	})
	if err != nil {
		t.Fatalf("approve P23 proposal: %v", err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-p23-" + suffix, LeaseDuration: time.Hour,
	})
	if err != nil || !ok || claimed.Run.ID != run.ID {
		t.Fatalf("claim approved P23 Run: claimed=%#v ok=%t err=%v", claimed, ok, err)
	}
	ctx, err := ContextWithLeaseToken(requesterCtx, claimed.Token)
	if err != nil {
		t.Fatal(err)
	}
	run = &claimed.Run
	return store, ctx, run, claimed.Token, approval
}

func p23EffectInput(run *mysql.WorkflowRun, lease LeaseToken, approval *mysql.AgentApproval, toolCallID string) TransitionEffectInput {
	return TransitionEffectInput{
		Lease: lease, ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ToolCallIDObserved: toolCallID, ToolName: approval.ToolName, ToolRevision: approval.ToolRevision,
		ToolSchemaHash: approval.ToolSchemaHash, TargetHash: strings.Repeat("3", 64),
		RequestRedacted: approval.ProposalJSONRedacted, PolicyHash: approval.PolicyHash,
		RuntimeCompatibilityHash: approval.RuntimeCompatibilityHash, EffectType: string(policy.EffectTransactionalDB),
		Attempt: run.Attempt, TraceID: "trace-p23-" + run.ID, GateAllowed: true,
	}
}
