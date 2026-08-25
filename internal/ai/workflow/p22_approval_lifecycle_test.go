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

func TestTwoPhaseApprovalPublishesOnlyExactCheckpoint(t *testing.T) {
	db := newP07Database(t, "p22_two_phase")
	store, ctx, run, lease := p22RunningRun(t, db, "two-phase")
	prepared := p22Prepare(t, store, ctx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	if prepared.Status != ApprovalStatusPreparing || prepared.CheckpointID != nil || prepared.PublishedAt != nil {
		t.Fatalf("prepared Approval = %#v", prepared)
	}

	checkpointID, err := EinoCheckpointID(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkpointContext, err := ContextWithLeaseToken(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(checkpointContext, checkpointID, []byte("opaque-p22-checkpoint")); err != nil {
		t.Fatalf("set exact Checkpoint: %v", err)
	}
	fingerprint, err := store.LoadCheckpointFingerprint(ctx, lease, checkpointID)
	if err != nil {
		t.Fatalf("load exact Checkpoint fingerprint: %v", err)
	}
	published, err := store.PublishApprovalAndWait(ctx, PublishApprovalInput{
		Lease: lease, ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash,
		InterruptID: "interrupt-p22-root", InterruptAddress: "agent:root;tool:block-ip",
		CheckpointID: checkpointID, CheckpointPayloadSHA256: fingerprint.PayloadSHA256,
		CheckpointLeaseGeneration: fingerprint.LeaseGeneration, TraceID: "trace-p22-publish",
	})
	if err != nil {
		t.Fatalf("publish exact Approval: %v", err)
	}
	if published.Status != ApprovalStatusPending || published.CheckpointID == nil || *published.CheckpointID != checkpointID ||
		published.CheckpointPayloadSHA256 == nil || *published.CheckpointPayloadSHA256 != fingerprint.PayloadSHA256 ||
		published.CheckpointLeaseGeneration == nil || *published.CheckpointLeaseGeneration != lease.Generation {
		t.Fatalf("published Approval = %#v", published)
	}
	var storedRun mysql.WorkflowRun
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != RunStatusWaitingApproval || storedRun.LeaseOwner != nil || storedRun.LastEventSeq != 5 {
		t.Fatalf("waiting Approval Run = status=%q owner=%v seq=%d", storedRun.Status, storedRun.LeaseOwner, storedRun.LastEventSeq)
	}
	var eventTypes []string
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", run.ID).Order("seq").Pluck("event_type", &eventTypes).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{EventRunCreated, EventRunClaimed, EventApprovalPreparing, EventAgentInterrupted, EventApprovalRequested}
	if fmt.Sprint(eventTypes) != fmt.Sprint(want) {
		t.Fatalf("two-phase Events = %v, want %v", eventTypes, want)
	}
}

func TestCheckpointFingerprintMismatchNeverPublishes(t *testing.T) {
	db := newP07Database(t, "p22_fingerprint_mismatch")
	store, ctx, run, lease := p22RunningRun(t, db, "fingerprint-mismatch")
	prepared := p22Prepare(t, store, ctx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	checkpointID, _ := EinoCheckpointID(run.ID)
	checkpointContext, _ := ContextWithLeaseToken(ctx, lease)
	if err := store.Set(checkpointContext, checkpointID, []byte("opaque-p22-exact")); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := store.LoadCheckpointFingerprint(ctx, lease, checkpointID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PublishApprovalAndWait(ctx, PublishApprovalInput{
		Lease: lease, ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash,
		InterruptID: "interrupt-mismatch", InterruptAddress: "tool:block-ip", CheckpointID: checkpointID,
		CheckpointPayloadSHA256: strings.Repeat("f", 64), CheckpointLeaseGeneration: fingerprint.LeaseGeneration,
	})
	if !errors.Is(err, ErrApprovalCheckpointMismatch) {
		t.Fatalf("mismatched publish error=%v, want ErrApprovalCheckpointMismatch", err)
	}
	var approval mysql.AgentApproval
	var storedRun mysql.WorkflowRun
	if err := db.First(&approval, "id = ?", prepared.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if approval.Status != ApprovalStatusPreparing || approval.PublishedAt != nil || storedRun.Status != RunStatusRunning || storedRun.LastEventSeq != 3 {
		t.Fatalf("mismatch partially published: approval=%#v run=%#v", approval, storedRun)
	}
}

func TestTwoPhaseApprovalPublicationRollsBackIfEitherEventFails(t *testing.T) {
	for _, rejectedEvent := range []string{EventAgentInterrupted, EventApprovalRequested} {
		t.Run(rejectedEvent, func(t *testing.T) {
			db := newP07Database(t, "p22_publish_rollback_"+strings.ReplaceAll(rejectedEvent, ".", "_"))
			store, ctx, run, lease := p22RunningRun(t, db, "publish-rollback-"+strings.ReplaceAll(rejectedEvent, ".", "-"))
			prepared := p22Prepare(t, store, ctx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
			checkpointID, _ := EinoCheckpointID(run.ID)
			checkpointContext, _ := ContextWithLeaseToken(ctx, lease)
			if err := store.Set(checkpointContext, checkpointID, []byte("opaque-publish-rollback")); err != nil {
				t.Fatal(err)
			}
			fingerprint, err := store.LoadCheckpointFingerprint(ctx, lease, checkpointID)
			if err != nil {
				t.Fatal(err)
			}
			trigger := fmt.Sprintf(`CREATE TRIGGER reject_p22_publish BEFORE INSERT ON workflow_events
				FOR EACH ROW BEGIN
					IF NEW.event_type = '%s' THEN
						SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject P22 publish event';
					END IF;
				END`, rejectedEvent)
			if err := db.Exec(trigger).Error; err != nil {
				t.Fatalf("create P22 publish failure trigger: %v", err)
			}

			_, err = store.PublishApprovalAndWait(ctx, PublishApprovalInput{
				Lease: lease, ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash,
				InterruptID: "interrupt-p22-rollback", InterruptAddress: "agent:root;tool:block-ip",
				CheckpointID: checkpointID, CheckpointPayloadSHA256: fingerprint.PayloadSHA256,
				CheckpointLeaseGeneration: fingerprint.LeaseGeneration,
			})
			if err == nil {
				t.Fatal("Approval publication succeeded despite Event insertion failure")
			}
			var approval mysql.AgentApproval
			var storedRun mysql.WorkflowRun
			if err := db.First(&approval, "id = ?", prepared.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			var eventCount int64
			if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", run.ID).Count(&eventCount).Error; err != nil {
				t.Fatal(err)
			}
			if approval.Status != ApprovalStatusPreparing || approval.PublishedAt != nil ||
				storedRun.Status != RunStatusRunning || storedRun.LastEventSeq != 3 || eventCount != 3 {
				t.Fatalf("publication rollback leaked truth: approval=%#v run=%#v events=%d", approval, storedRun, eventCount)
			}
		})
	}
}

func TestTwoPhaseApprovalPreparingIsGenerationFencedAndNeverResetsTerminal(t *testing.T) {
	db := newP07Database(t, "p22_prepare_fence")
	store, ctx, run, lease := p22RunningRun(t, db, "prepare-fence")
	prepared := p22Prepare(t, store, ctx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))

	stale := lease
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Updates(map[string]any{
		"lease_owner": "worker-p22-next", "lease_generation": lease.Generation + 1,
		"lease_until": time.Now().Add(time.Hour), "heartbeat_at": time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	input := p22PrepareInput(run, stale, prepared.ProposalHash, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	if _, err := store.PrepareApproval(ctx, input); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale preparing refresh error=%v, want ErrLeaseLost", err)
	}
	if err := db.Model(&mysql.AgentApproval{}).Where("id = ?", prepared.ID).Updates(map[string]any{
		"status": ApprovalStatusRejected, "version": 2, "decided_at": time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	current := stale
	current.Owner = "worker-p22-next"
	current.Generation++
	got, err := store.PrepareApproval(ctx, p22PrepareInput(run, current, prepared.ProposalHash, "block_ip", policy.RiskL2, time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("read terminal Approval through stable prepare: %v", err)
	}
	if got.Status != ApprovalStatusRejected || got.Version != 2 {
		t.Fatalf("terminal Approval was reset: %#v", got)
	}
}

func TestTwoPhaseApprovalIncompatiblePreparingIsInvalidatedAndParked(t *testing.T) {
	db := newP07Database(t, "p22_prepare_incompatible")
	store, ctx, run, lease := p22RunningRun(t, db, "prepare-incompatible")
	prepared := p22Prepare(t, store, ctx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	if err := db.Model(&mysql.AgentApproval{}).Where("id = ?", prepared.ID).Update("tool_revision", "tampered-revision").Error; err != nil {
		t.Fatal(err)
	}
	_, err := store.PrepareApproval(ctx, p22PrepareInput(run, lease, prepared.ProposalHash, "block_ip", policy.RiskL2, time.Now().Add(time.Hour)))
	if !errors.Is(err, ErrApprovalInvalidated) {
		t.Fatalf("incompatible preparing error=%v, want ErrApprovalInvalidated", err)
	}
	var approval mysql.AgentApproval
	var storedRun mysql.WorkflowRun
	if err := db.First(&approval, "id = ?", prepared.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if approval.Status != ApprovalStatusInvalidated || storedRun.Status != RunStatusParked ||
		storedRun.ParkReason == nil || *storedRun.ParkReason != ParkReasonApprovalInvalidated {
		t.Fatalf("incompatible preparing outcome: approval=%s run=%s reason=%v", approval.Status, storedRun.Status, storedRun.ParkReason)
	}
}

func TestResumeAuthorizationRequiresExplicitExactDatabaseTruth(t *testing.T) {
	db := newP07Database(t, "p22_resume_authorization")
	store, ownerCtx, run, lease := p22RunningRun(t, db, "resume-authorization")
	prepared := p22Prepare(t, store, ownerCtx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	p22Publish(t, store, ownerCtx, run.ID, lease, prepared)
	decided, err := store.DecideApprovalAndWakeRun(p21Identity("p22-approver", policy.RoleApprover), DecideApprovalInput{
		ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash, ExpectedVersion: prepared.Version,
		Decision: ApprovalStatusApproved,
	})
	if err != nil {
		t.Fatalf("approve P22 fixture: %v", err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-p22-resume", LeaseDuration: time.Hour})
	if err != nil || !ok {
		t.Fatalf("claim approved Run: ok=%v err=%v", ok, err)
	}
	resumeCtx := p22OperatorContext(claimed.Run.UserID)
	target, err := store.LoadApprovalResumeTarget(resumeCtx, claimed.Token)
	if err != nil {
		t.Fatalf("load explicit Resume target: %v", err)
	}
	if target == nil || target.InterruptID == "" || target.Decision != ApprovalStatusApproved || target.ApprovalID != decided.ID {
		t.Fatalf("Resume target = %#v", target)
	}
	if _, err := store.AuthorizeApprovalResume(resumeCtx, AuthorizeApprovalResumeInput{
		Lease: claimed.Token, ApprovalID: target.ApprovalID, ProposalHash: target.ProposalHash,
		ToolName: decided.ToolName, ToolRevision: decided.ToolRevision, ToolSchemaHash: decided.ToolSchemaHash,
		PolicyHash: decided.PolicyHash, RuntimeCompatibilityHash: decided.RuntimeCompatibilityHash, ExplicitTarget: true, GateAllowed: true,
	}); err != nil {
		t.Fatalf("authorize exact approved Resume: %v", err)
	}
	if _, err := store.AuthorizeApprovalResume(resumeCtx, AuthorizeApprovalResumeInput{
		Lease: claimed.Token, ApprovalID: target.ApprovalID, ProposalHash: target.ProposalHash,
		ToolName: decided.ToolName, ToolRevision: decided.ToolRevision, ToolSchemaHash: decided.ToolSchemaHash,
		PolicyHash: decided.PolicyHash, RuntimeCompatibilityHash: decided.RuntimeCompatibilityHash, ExplicitTarget: false, GateAllowed: true,
	}); !errors.Is(err, ErrApprovalResumeTargetRequired) {
		t.Fatalf("implicit Resume error=%v, want ErrApprovalResumeTargetRequired", err)
	}
}

func TestCheckpointFingerprintPreparingOrphanBuildsEmptyResumeTargets(t *testing.T) {
	db := newP07Database(t, "p22_preparing_orphan")
	store, ctx, run, lease := p22RunningRun(t, db, "preparing-orphan")
	prepared := p22Prepare(t, store, ctx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	target, err := store.LoadApprovalResumeTarget(ctx, lease)
	if err != nil || target != nil {
		t.Fatalf("pre-Checkpoint preparing target=%#v err=%v, want immutable Replay", target, err)
	}
	checkpointID, _ := EinoCheckpointID(run.ID)
	checkpointContext, _ := ContextWithLeaseToken(ctx, lease)
	if err := store.Set(checkpointContext, checkpointID, []byte("opaque-preparing-orphan")); err != nil {
		t.Fatal(err)
	}
	target, err = store.LoadApprovalResumeTarget(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.ApprovalID != prepared.ID || target.InterruptID != "" || target.Decision != ApprovalStatusPreparing {
		t.Fatalf("preparing orphan target=%#v, want explicit empty InterruptID", target)
	}
}

func TestResumeAuthorizationTerminalDecisionsDoNotReopenApproval(t *testing.T) {
	for _, decision := range []string{ApprovalStatusRejected, ApprovalStatusExpired} {
		t.Run(decision, func(t *testing.T) {
			db := newP07Database(t, "p22_terminal_resume_"+decision)
			store, ownerCtx, run, lease := p22RunningRun(t, db, "terminal-resume-"+decision)
			expiresAt := time.Now().Add(time.Hour)
			if decision == ApprovalStatusExpired {
				expiresAt = time.Now().Add(-time.Minute)
			}
			prepared := p22Prepare(t, store, ownerCtx, run, lease, "block_ip", policy.RiskL2, expiresAt)
			p22Publish(t, store, ownerCtx, run.ID, lease, prepared)
			decided, err := store.DecideApprovalAndWakeRun(p21Identity("p22-terminal-approver-"+decision, policy.RoleApprover), DecideApprovalInput{
				ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash, ExpectedVersion: prepared.Version, Decision: decision,
			})
			if err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-p22-terminal-" + decision, LeaseDuration: time.Hour})
			if err != nil || !ok {
				t.Fatalf("claim terminal Approval Run: ok=%v err=%v", ok, err)
			}
			resumeCtx := p22OperatorContext(claimed.Run.UserID)
			target, err := store.LoadApprovalResumeTarget(resumeCtx, claimed.Token)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.AuthorizeApprovalResume(resumeCtx, AuthorizeApprovalResumeInput{
				Lease: claimed.Token, ApprovalID: target.ApprovalID, ProposalHash: target.ProposalHash,
				ToolName: decided.ToolName, ToolRevision: decided.ToolRevision, ToolSchemaHash: decided.ToolSchemaHash,
				PolicyHash: decided.PolicyHash, RuntimeCompatibilityHash: decided.RuntimeCompatibilityHash,
				ExplicitTarget: true, GateAllowed: true,
			})
			wantErr := ErrApprovalRejected
			if decision == ApprovalStatusExpired {
				wantErr = ErrApprovalExpired
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("terminal Resume error=%v, want %v", err, wantErr)
			}
			refreshed, err := store.PrepareApproval(resumeCtx, p22PrepareInput(run, claimed.Token, prepared.ProposalHash, "block_ip", policy.RiskL2, time.Now().Add(time.Hour)))
			if err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&mysql.AgentApproval{}).Where("run_id = ?", run.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if refreshed.Status != decision || count != 1 {
				t.Fatalf("terminal Approval reopened: status=%s count=%d", refreshed.Status, count)
			}
		})
	}
}

func TestResumeAuthorizationClosedGateParksWithoutChangingApprovedAudit(t *testing.T) {
	db := newP07Database(t, "p22_closed_gate")
	store, ownerCtx, run, lease := p22RunningRun(t, db, "closed-gate")
	prepared := p22Prepare(t, store, ownerCtx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	p22Publish(t, store, ownerCtx, run.ID, lease, prepared)
	decided, err := store.DecideApprovalAndWakeRun(p21Identity("p22-gate-approver", policy.RoleApprover), DecideApprovalInput{
		ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash, ExpectedVersion: prepared.Version, Decision: ApprovalStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-p22-closed-gate", LeaseDuration: time.Hour})
	if err != nil || !ok {
		t.Fatalf("claim closed-gate Run: ok=%v err=%v", ok, err)
	}
	resumeCtx := p22OperatorContext(claimed.Run.UserID)
	_, err = store.AuthorizeApprovalResume(resumeCtx, AuthorizeApprovalResumeInput{
		Lease: claimed.Token, ApprovalID: decided.ID, ProposalHash: decided.ProposalHash,
		ToolName: decided.ToolName, ToolRevision: decided.ToolRevision, ToolSchemaHash: decided.ToolSchemaHash,
		PolicyHash: decided.PolicyHash, RuntimeCompatibilityHash: decided.RuntimeCompatibilityHash,
		ExplicitTarget: true, GateAllowed: false,
	})
	if !errors.Is(err, ErrApprovalInvalidated) {
		t.Fatalf("closed Gate Resume error=%v, want ErrApprovalInvalidated", err)
	}
	var storedApproval mysql.AgentApproval
	var storedRun mysql.WorkflowRun
	if err := db.First(&storedApproval, "id = ?", prepared.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	var eventTypes []string
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", run.ID).Order("seq").Pluck("event_type", &eventTypes).Error; err != nil {
		t.Fatal(err)
	}
	wantSuffix := []string{EventApprovalInvalidated, EventRunParked}
	if storedApproval.Status != ApprovalStatusApproved || storedRun.Status != RunStatusParked || len(eventTypes) < 2 ||
		fmt.Sprint(eventTypes[len(eventTypes)-2:]) != fmt.Sprint(wantSuffix) {
		t.Fatalf("closed Gate outcome: approval=%s run=%s events=%v", storedApproval.Status, storedRun.Status, eventTypes)
	}
}

func TestResumeAuthorizationRejectsL2SelfApproval(t *testing.T) {
	db := newP07Database(t, "p22_l2_self_approval")
	store, ownerCtx, run, lease := p22RunningRun(t, db, "l2-self-approval")
	prepared := p22Prepare(t, store, ownerCtx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	p22Publish(t, store, ownerCtx, run.ID, lease, prepared)
	_, err := store.DecideApprovalAndWakeRun(p21Identity(run.UserID, policy.RoleApprover), DecideApprovalInput{
		ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash, ExpectedVersion: prepared.Version, Decision: ApprovalStatusApproved,
	})
	if !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("L2 self-approval error=%v, want policy.ErrForbidden", err)
	}
}

func TestCheckpointFingerprintOverwriteParksWithoutChangingApprovedAudit(t *testing.T) {
	db := newP07Database(t, "p22_overwrite_parks")
	store, ownerCtx, run, lease := p22RunningRun(t, db, "overwrite-parks")
	prepared := p22Prepare(t, store, ownerCtx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	p22Publish(t, store, ownerCtx, run.ID, lease, prepared)
	if _, err := store.DecideApprovalAndWakeRun(p21Identity("p22-overwrite-approver", policy.RoleApprover), DecideApprovalInput{
		ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash, ExpectedVersion: prepared.Version,
		Decision: ApprovalStatusApproved,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: "worker-p22-overwrite", LeaseDuration: time.Hour})
	if err != nil || !ok {
		t.Fatalf("claim approved Run: ok=%v err=%v", ok, err)
	}
	resumeCtx := p22OperatorContext(claimed.Run.UserID)
	checkpointID, _ := EinoCheckpointID(run.ID)
	checkpointContext, _ := ContextWithLeaseToken(resumeCtx, claimed.Token)
	if err := store.Set(checkpointContext, checkpointID, []byte("overwritten-under-stable-id")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadApprovalResumeTarget(resumeCtx, claimed.Token); !errors.Is(err, ErrApprovalCheckpointMismatch) {
		t.Fatalf("overwritten fingerprint error=%v, want ErrApprovalCheckpointMismatch", err)
	}
	var approval mysql.AgentApproval
	var storedRun mysql.WorkflowRun
	if err := db.First(&approval, "id = ?", prepared.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if approval.Status != ApprovalStatusApproved || storedRun.Status != RunStatusParked || storedRun.ParkReason == nil || *storedRun.ParkReason != ParkReasonApprovalInvalidated {
		t.Fatalf("overwrite outcome: approval=%s run=%s reason=%v", approval.Status, storedRun.Status, storedRun.ParkReason)
	}
}

func TestTwoPhaseApprovalExpiryScanUsesDecisionPrimitiveAndTerminalInvalidatesPreparing(t *testing.T) {
	db := newP07Database(t, "p22_expiry_terminal")
	store, ownerCtx, run, lease := p22RunningRun(t, db, "expiry")
	prepared := p22Prepare(t, store, ownerCtx, run, lease, "block_ip", policy.RiskL2, time.Now().Add(-time.Minute))
	p22Publish(t, store, ownerCtx, run.ID, lease, prepared)
	count, err := store.ExpireDueApprovals(context.Background(), strings.Repeat("w", 128), 20)
	if err != nil || count != 1 {
		t.Fatalf("expire due Approvals count=%d err=%v", count, err)
	}
	var expired mysql.AgentApproval
	var expiredRun mysql.WorkflowRun
	if err := db.First(&expired, "id = ?", prepared.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&expiredRun, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if expired.Status != ApprovalStatusExpired || expired.DecidedBy == nil || *expired.DecidedBy != "approval-expiry-worker" || expiredRun.Status != RunStatusPending {
		t.Fatalf("expiry outcome: approval=%s run=%s", expired.Status, expiredRun.Status)
	}

	terminalStore, terminalCtx, terminalRun, terminalLease := p22RunningRun(t, db, "terminal")
	terminalPreparing := p22Prepare(t, terminalStore, terminalCtx, terminalRun, terminalLease, "block_ip", policy.RiskL2, time.Now().Add(time.Hour))
	if err := terminalStore.CompleteRunAndCommitSession(terminalCtx, CompleteRunInput{
		RunID: terminalRun.ID, ExpectedStatus: RunStatusRunning, TargetStatus: RunStatusFailed,
		Lease: terminalLease, ErrorMessage: "terminal fixture", TraceQuality: "unknown",
	}); err != nil {
		t.Fatalf("complete terminal Run: %v", err)
	}
	var invalidated mysql.AgentApproval
	if err := db.First(&invalidated, "id = ?", terminalPreparing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if invalidated.Status != ApprovalStatusInvalidated || invalidated.DecidedAt == nil {
		t.Fatalf("terminal preparing Approval = %#v", invalidated)
	}
	var terminalEvents []string
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", terminalRun.ID).Order("seq").Pluck("event_type", &terminalEvents).Error; err != nil {
		t.Fatal(err)
	}
	wantTerminalEvents := []string{EventRunCreated, EventRunClaimed, EventApprovalPreparing, EventApprovalInvalidated, EventRunFailed}
	if fmt.Sprint(terminalEvents) != fmt.Sprint(wantTerminalEvents) {
		t.Fatalf("terminal orphan Events=%v, want %v", terminalEvents, wantTerminalEvents)
	}
}

func p22RunningRun(t *testing.T, db *gorm.DB, suffix string) (*GORMStore, context.Context, *mysql.WorkflowRun, LeaseToken) {
	t.Helper()
	store := NewGORMStore(db)
	ctx := p22OperatorContext("operator-p22-" + suffix)
	run, err := store.CreateRunWithSessionLock(ctx, p08CreateInput("run-p22-"+suffix, "session-p22-"+suffix))
	if err != nil {
		t.Fatalf("create P22 Run: %v", err)
	}
	p08MoveToRunning(t, store, ctx, run.ID)
	lease := p08CurrentLease(t, db, run.ID)
	if err := db.First(run, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("reload running P22 Run: %v", err)
	}
	return store, ctx, run, lease
}

func p22Prepare(t *testing.T, store *GORMStore, ctx context.Context, run *mysql.WorkflowRun, lease LeaseToken, toolName string, risk policy.RiskLevel, expiresAt time.Time) *mysql.AgentApproval {
	t.Helper()
	proposalHash := fmt.Sprintf("%064x", len(run.ID)+len(toolName))
	approval, err := store.PrepareApproval(ctx, p22PrepareInput(run, lease, proposalHash, toolName, risk, expiresAt))
	if err != nil {
		t.Fatalf("prepare Approval: %v", err)
	}
	return approval
}

func p22PrepareInput(run *mysql.WorkflowRun, lease LeaseToken, proposalHash, toolName string, risk policy.RiskLevel, expiresAt time.Time) PrepareApprovalInput {
	return PrepareApprovalInput{
		Lease: lease, ToolCallIDObserved: "call-" + toolName, ToolName: toolName, ToolRevision: "v1",
		ToolSchemaHash: strings.Repeat("2", 64), RiskLevel: risk, ProposalJSONRedacted: `{"target":"192.0.2.1"}`,
		ProposalHash: proposalHash, PolicyHash: strings.Repeat("d", 64), RuntimeCompatibilityHash: *run.RuntimeCompatibilityHash,
		ExpiresAt: expiresAt,
	}
}

func p22Publish(t *testing.T, store *GORMStore, ctx context.Context, runID string, lease LeaseToken, approval *mysql.AgentApproval) {
	t.Helper()
	checkpointID, _ := EinoCheckpointID(runID)
	checkpointContext, _ := ContextWithLeaseToken(ctx, lease)
	if err := store.Set(checkpointContext, checkpointID, []byte("opaque-"+runID)); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := store.LoadCheckpointFingerprint(ctx, lease, checkpointID)
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishApprovalAndWait(ctx, PublishApprovalInput{
		Lease: lease, ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		InterruptID: "interrupt-" + runID, InterruptAddress: "agent:root;tool:block-ip",
		CheckpointID: checkpointID, CheckpointPayloadSHA256: fingerprint.PayloadSHA256,
		CheckpointLeaseGeneration: fingerprint.LeaseGeneration, TraceID: "trace-" + runID,
	})
	if err != nil {
		t.Fatal(err)
	}
	*approval = *published
}

func p22OperatorContext(userID string) context.Context {
	return policy.WithIdentity(context.Background(), policy.Identity{
		UserID: userID, Role: policy.RoleOperator, Scope: policy.Scope{UserID: userID},
	})
}
