package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestApprovalStableIdentityAndRunProposalUnique(t *testing.T) {
	db := newP07Database(t, "p21_identity_unique")
	proposalHash := strings.Repeat("1", 64)
	approvalID, err := policy.ApprovalID("run-p21-unique", proposalHash)
	if err != nil {
		t.Fatal(err)
	}
	first := p21ApprovalRow(approvalID, "run-p21-unique", proposalHash, "requester", ApprovalStatusPreparing, time.Now().Add(time.Hour))
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create stable Approval: %v", err)
	}
	duplicate := p21ApprovalRow("different-id", first.RunID, proposalHash, "requester", ApprovalStatusPreparing, time.Now().Add(time.Hour))
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("run_id + proposal_hash accepted a duplicate Approval")
	}
	var stored mysql.AgentApproval
	if err := db.First(&stored, "id = ?", approvalID).Error; err != nil || stored.ID != approvalID {
		t.Fatalf("stable Approval identity = %#v err=%v", stored, err)
	}
}

func TestApprovalDecisionCASIsIdempotentAndWakesRun(t *testing.T) {
	db := newP07Database(t, "p21_decision_idempotent")
	store, approval := p21SeedApproval(t, db, "idempotent", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	ctx := p21Identity("approver", policy.RoleApprover)
	input := DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved, Reason: "approved by reviewer",
	}

	decided, err := store.DecideApprovalAndWakeRun(ctx, input)
	if err != nil {
		t.Fatalf("approve pending Approval: %v", err)
	}
	if decided.Status != ApprovalStatusApproved || decided.Version != approval.Version+1 || decided.DecidedBy == nil || *decided.DecidedBy != "approver" {
		t.Fatalf("decided Approval = %#v", decided)
	}
	repeated, err := store.DecideApprovalAndWakeRun(ctx, input)
	if err != nil || repeated.Status != ApprovalStatusApproved || repeated.Version != decided.Version {
		t.Fatalf("repeat same decision = %#v err=%v", repeated, err)
	}

	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", approval.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusPending || run.LastEventSeq != 4 || run.LeaseOwner != nil || run.LeaseUntil != nil {
		t.Fatalf("woken Run = status=%q seq=%d owner=%v until=%v", run.Status, run.LastEventSeq, run.LeaseOwner, run.LeaseUntil)
	}
	var eventCount int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", approval.RunID, EventApprovalDecided).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("approval.decided count=%d, want 1", eventCount)
	}

	conflict := input
	conflict.Decision = ApprovalStatusRejected
	if _, err := store.DecideApprovalAndWakeRun(ctx, conflict); !errors.Is(err, ErrApprovalAlreadyDecided) {
		t.Fatalf("conflicting decision error=%v, want ErrApprovalAlreadyDecided", err)
	}
}

func TestApprovalDecisionParksRunWhenAttemptsExhausted(t *testing.T) {
	db := newP07Database(t, "p21_attempts_exhausted")
	store, approval := p21SeedApproval(t, db, "attempts-exhausted", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", approval.RunID).Update("attempt", 3).Error; err != nil {
		t.Fatalf("raise fixture Run attempt: %v", err)
	}
	ctx := p21Identity("approver", policy.RoleApprover)
	decided, err := store.DecideApprovalAndWakeRun(ctx, DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved, Reason: "final decision",
	})
	if err != nil {
		t.Fatalf("decide final Approval: %v", err)
	}
	if decided.Status != ApprovalStatusApproved {
		t.Fatalf("decided Approval = %#v", decided)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", approval.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusParked || run.ParkReason == nil || *run.ParkReason != ParkReasonApprovalAttemptsExhausted {
		t.Fatalf("exhausted Run = status=%q park_reason=%v, want parked %q", run.Status, run.ParkReason, ParkReasonApprovalAttemptsExhausted)
	}
	if run.LeaseOwner != nil || run.LeaseUntil != nil {
		t.Fatalf("exhausted Run still holds a lease: owner=%v until=%v", run.LeaseOwner, run.LeaseUntil)
	}
	var parkedCount int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", approval.RunID, EventRunParked).Count(&parkedCount).Error; err != nil {
		t.Fatal(err)
	}
	if parkedCount != 1 {
		t.Fatalf("run.parked count=%d, want 1", parkedCount)
	}
}

func TestApprovalDecisionExpiredReturnsStableConflict(t *testing.T) {
	db := newP07Database(t, "p21_expired")
	store, approval := p21SeedApproval(t, db, "expired", ApprovalStatusPending, "requester", time.Now().Add(-time.Minute))
	ctx := p21Identity("approver", policy.RoleApprover)
	approve := DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
	}
	if _, err := store.DecideApprovalAndWakeRun(ctx, approve); !errors.Is(err, ErrApprovalExpired) {
		t.Fatalf("approve expired proposal error=%v, want ErrApprovalExpired", err)
	}
	expire := approve
	expire.Decision = ApprovalStatusExpired
	expired, err := store.DecideApprovalAndWakeRun(ctx, expire)
	if err != nil || expired.Status != ApprovalStatusExpired {
		t.Fatalf("expire due Approval = %#v err=%v", expired, err)
	}
	repeated, err := store.DecideApprovalAndWakeRun(ctx, expire)
	if err != nil || repeated.Status != ApprovalStatusExpired || repeated.Version != expired.Version {
		t.Fatalf("repeat expiry = %#v err=%v", repeated, err)
	}
	var eventCount int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", approval.RunID, EventApprovalExpired).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("approval.expired count=%d, want 1", eventCount)
	}
}

func TestPreparingApprovalIsInvisibleAndUndecidable(t *testing.T) {
	db := newP07Database(t, "p21_preparing")
	store, approval := p21SeedApproval(t, db, "preparing", ApprovalStatusPreparing, "requester", time.Now().Add(time.Hour))
	ctx := p21Identity("approver", policy.RoleApprover)
	items, err := store.ListPendingApprovals(ctx, 20)
	if err != nil || len(items) != 0 {
		t.Fatalf("pending list exposed preparing: items=%#v err=%v", items, err)
	}
	if _, err := store.GetPendingApproval(ctx, approval.ID); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("preparing detail error=%v, want ErrApprovalNotFound", err)
	}
	_, err = store.DecideApprovalAndWakeRun(ctx, DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
	})
	if !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("preparing decision error=%v, want ErrApprovalNotFound", err)
	}
}

func TestApprovalListAndDetailReturnOnlyActionableOthers(t *testing.T) {
	db := newP07Database(t, "p21_list_detail")
	store, other := p21SeedApproval(t, db, "list-other", ApprovalStatusPending, "other", time.Now().Add(time.Hour))
	p21SeedApproval(t, db, "list-self", ApprovalStatusPending, "approver", time.Now().Add(time.Hour))
	p21SeedApproval(t, db, "list-preparing", ApprovalStatusPreparing, "other-two", time.Now().Add(time.Hour))
	ctx := p21Identity("approver", policy.RoleApprover)
	items, err := store.ListPendingApprovals(ctx, 20)
	if err != nil || len(items) != 1 || items[0].ID != other.ID || items[0].ProposalHash == "" {
		t.Fatalf("actionable Approval list=%#v err=%v", items, err)
	}
	detail, err := store.GetPendingApproval(ctx, other.ID)
	if err != nil || detail.ID != other.ID || detail.ProposalHash != other.ProposalHash {
		t.Fatalf("pending detail=%#v err=%v", detail, err)
	}
}

func TestApprovalRoleAndSelfApprovalAreRejected(t *testing.T) {
	for _, test := range []struct {
		name      string
		actorID   string
		role      policy.Role
		requester string
	}{
		{name: "viewer", actorID: "viewer", role: policy.RoleViewer, requester: "other"},
		{name: "operator", actorID: "operator", role: policy.RoleOperator, requester: "other"},
		{name: "approver_self_L2", actorID: "same", role: policy.RoleApprover, requester: "same"},
		{name: "admin_self_L2", actorID: "same-admin", role: policy.RoleAdmin, requester: "same-admin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newP07Database(t, "p21_role_"+strings.ToLower(test.name))
			store, approval := p21SeedApproval(t, db, "role-"+test.name, ApprovalStatusPending, test.requester, time.Now().Add(time.Hour))
			_, err := store.DecideApprovalAndWakeRun(p21Identity(test.actorID, test.role), DecideApprovalInput{
				ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
				ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
			})
			if !errors.Is(err, policy.ErrForbidden) {
				t.Fatalf("decision error=%v, want policy.ErrForbidden", err)
			}
		})
	}

	for _, role := range []policy.Role{policy.RoleApprover, policy.RoleAdmin} {
		t.Run("other_"+string(role), func(t *testing.T) {
			db := newP07Database(t, "p21_other_"+string(role))
			store, approval := p21SeedApproval(t, db, "other-"+string(role), ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
			if _, err := store.DecideApprovalAndWakeRun(p21Identity("reviewer-"+string(role), role), DecideApprovalInput{
				ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
				ExpectedVersion: approval.Version, Decision: ApprovalStatusRejected,
			}); err != nil {
				t.Fatalf("%s could not reject another user's proposal: %v", role, err)
			}
		})
	}
}

func TestApprovalDecisionRejectsProposalHashMismatch(t *testing.T) {
	db := newP07Database(t, "p21_hash_mismatch")
	store, approval := p21SeedApproval(t, db, "hash-mismatch", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	_, err := store.DecideApprovalAndWakeRun(p21Identity("approver", policy.RoleApprover), DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: strings.Repeat("f", 64),
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
	})
	if !errors.Is(err, ErrApprovalProposalMismatch) {
		t.Fatalf("proposal hash mismatch error=%v", err)
	}
	var stored mysql.AgentApproval
	if err := db.First(&stored, "id = ?", approval.ID).Error; err != nil || stored.Status != ApprovalStatusPending || stored.Version != approval.Version {
		t.Fatalf("mismatch changed Approval=%#v err=%v", stored, err)
	}
}

func TestApprovalDecisionRejectsUnstableIdentity(t *testing.T) {
	db := newP07Database(t, "p21_unstable_identity")
	store, approval := p21SeedApproval(t, db, "unstable-identity", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	unstableID := "unstable-approval-id"
	if err := db.Model(&mysql.AgentApproval{}).Where("id = ?", approval.ID).Update("id", unstableID).Error; err != nil {
		t.Fatal(err)
	}
	_, err := store.DecideApprovalAndWakeRun(p21Identity("approver", policy.RoleApprover), DecideApprovalInput{
		ApprovalID: unstableID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
	})
	if !errors.Is(err, ErrApprovalIdentityMismatch) {
		t.Fatalf("unstable Approval identity error=%v", err)
	}
}

func TestApprovalTerminalStatusesAreImmutable(t *testing.T) {
	for _, terminal := range []string{ApprovalStatusRejected, ApprovalStatusInvalidated} {
		t.Run(terminal, func(t *testing.T) {
			db := newP07Database(t, "p21_terminal_"+terminal)
			store, approval := p21SeedApproval(t, db, "terminal-"+terminal, terminal, "requester", time.Now().Add(time.Hour))
			_, err := store.DecideApprovalAndWakeRun(p21Identity("approver", policy.RoleApprover), DecideApprovalInput{
				ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
				ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
			})
			if !errors.Is(err, ErrApprovalAlreadyDecided) {
				t.Fatalf("terminal %s transition error=%v", terminal, err)
			}
			var stored mysql.AgentApproval
			if err := db.First(&stored, "id = ?", approval.ID).Error; err != nil || stored.Status != terminal || stored.Version != approval.Version {
				t.Fatalf("terminal Approval changed=%#v err=%v", stored, err)
			}
		})
	}
}

func TestApprovalAuthDisabledIsReadOnly(t *testing.T) {
	db := newP07Database(t, "p21_auth_disabled")
	store, approval := p21SeedApproval(t, db, "auth-disabled", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	ctx := policy.WithIdentity(context.Background(), policy.DisabledIdentity())
	if _, err := store.ListPendingApprovals(ctx, 20); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("auth-disabled list error=%v", err)
	}
	_, err := store.DecideApprovalAndWakeRun(ctx, DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
	})
	if !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("auth-disabled decision error=%v", err)
	}
}

func TestApprovalDecisionReasonUsesUnifiedRedactor(t *testing.T) {
	db := newP07Database(t, "p21_reason_redaction")
	store, approval := p21SeedApproval(t, db, "reason-redaction", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	decided, err := store.DecideApprovalAndWakeRun(p21Identity("approver", policy.RoleApprover), DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusRejected,
		Reason: "Authorization: Bearer approval-plaintext",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decided.DecisionReason == nil || strings.Contains(*decided.DecisionReason, "approval-plaintext") || !strings.Contains(*decided.DecisionReason, "[REDACTED]") {
		t.Fatalf("decision reason was not safely redacted: %v", decided.DecisionReason)
	}
}

func TestApprovalDecisionTransactionRollsBackEveryWrite(t *testing.T) {
	db := newP07Database(t, "p21_atomic")
	store, approval := p21SeedApproval(t, db, "atomic", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	if err := db.Exec(`CREATE TRIGGER reject_p21_event BEFORE INSERT ON workflow_events
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject approval event'`).Error; err != nil {
		t.Fatalf("create Event failure trigger: %v", err)
	}
	_, err := store.DecideApprovalAndWakeRun(p21Identity("approver", policy.RoleApprover), DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: ApprovalStatusApproved,
	})
	if err == nil {
		t.Fatal("decision succeeded despite Event insert failure")
	}
	var stored mysql.AgentApproval
	if err := db.First(&stored, "id = ?", approval.ID).Error; err != nil {
		t.Fatal(err)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", approval.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != ApprovalStatusPending || stored.Version != approval.Version || stored.DecidedAt != nil || run.Status != RunStatusWaitingApproval || run.LastEventSeq != 3 {
		t.Fatalf("transaction partially committed: approval=%#v run_status=%q seq=%d", stored, run.Status, run.LastEventSeq)
	}
}

func TestApprovalConcurrentDecisionCASHasOneEvent(t *testing.T) {
	db := newP07Database(t, "p21_concurrent")
	store, approval := p21SeedApproval(t, db, "concurrent", ApprovalStatusPending, "requester", time.Now().Add(time.Hour))
	ctx := p21Identity("approver", policy.RoleApprover)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, decision := range []string{ApprovalStatusApproved, ApprovalStatusRejected} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.DecideApprovalAndWakeRun(ctx, DecideApprovalInput{
				ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
				ExpectedVersion: approval.Version, Decision: decision,
			})
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrApprovalAlreadyDecided):
			conflict++
		default:
			t.Fatalf("concurrent decision error=%v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent results success=%d conflict=%d", success, conflict)
	}
	var count int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ? AND event_type = ?", approval.RunID, EventApprovalDecided).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("decision Event count=%d err=%v", count, err)
	}
}

func p21SeedApproval(t *testing.T, db *gorm.DB, suffix, status, requestedBy string, expiresAt time.Time) (*GORMStore, mysql.AgentApproval) {
	t.Helper()
	store := NewGORMStore(db)
	ownerCtx := p08UserContext(requestedBy)
	runID := "run-p21-" + suffix
	run, err := store.CreateRunWithSessionLock(ownerCtx, p08CreateInput(runID, "session-p21-"+suffix))
	if err != nil {
		t.Fatalf("create Approval fixture Run: %v", err)
	}
	p08MoveToRunning(t, store, ownerCtx, run.ID)
	if status != ApprovalStatusPreparing {
		if err := store.TransitionRunWithEvent(ownerCtx, RunTransition{
			RunID: run.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusWaitingApproval,
			Lease: p08CurrentLease(t, db, run.ID), Event: WorkflowEventInput{Type: EventApprovalRequested},
		}); err != nil {
			t.Fatalf("publish fixture Approval: %v", err)
		}
	}
	proposalHash := fmt.Sprintf("%064x", len(suffix)+1)
	approvalID, err := policy.ApprovalID(run.ID, proposalHash)
	if err != nil {
		t.Fatal(err)
	}
	approval := p21ApprovalRow(approvalID, run.ID, proposalHash, requestedBy, status, expiresAt)
	if err := db.Create(&approval).Error; err != nil {
		t.Fatalf("create Approval fixture: %v", err)
	}
	return store, approval
}

func p21ApprovalRow(id, runID, proposalHash, requestedBy, status string, expiresAt time.Time) mysql.AgentApproval {
	now := time.Now().UTC().Truncate(time.Millisecond)
	approval := mysql.AgentApproval{
		ID: id, RunID: runID, ToolName: "block_ip", ToolRevision: "v1",
		ToolSchemaHash: strings.Repeat("2", 64), RiskLevel: string(policy.RiskL2),
		ProposalJSONRedacted: `{"ip":"192.0.2.1"}`, ProposalHash: proposalHash,
		PolicyHash: strings.Repeat("3", 64), RuntimeCompatibilityHash: strings.Repeat("4", 64),
		RequestedBy: requestedBy, Status: status, Version: 1,
		PreparingAt: now, CreatedAt: now, ExpiresAt: &expiresAt,
	}
	if status != ApprovalStatusPreparing {
		approval.PublishedAt = &now
	}
	return approval
}

func p21Identity(userID string, role policy.Role) context.Context {
	identity := policy.Identity{UserID: userID, Role: role, Scope: policy.Scope{UserID: userID}}
	if role == policy.RoleAdmin {
		identity.Scope.All = true
	}
	return policy.WithIdentity(context.Background(), identity)
}
