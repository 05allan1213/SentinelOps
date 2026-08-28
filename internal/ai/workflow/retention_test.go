package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"
)

func TestCleanupLeaseGenerationFenceAllowsOneWorker(t *testing.T) {
	db := newP07Database(t, "phase36_cleanup_lease")
	store := NewGORMStore(db)
	if err := store.EnsureRetentionLeaseRun(context.Background()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for _, owner := range []string{"worker-phase36-a", "worker-phase36-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := store.ClaimRetentionLease(context.Background(), RetentionClaimInput{
				Owner: owner, LeaseDuration: time.Minute,
			})
			if err != nil {
				t.Error(err)
			}
			results <- ok
		}()
	}
	wg.Wait()
	close(results)
	claimed := 0
	for ok := range results {
		if ok {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed workers=%d, want 1", claimed)
	}
}

func TestPhysicalDeleteRetentionProtectsResumeAndSeparatesPayloadFromAudit(t *testing.T) {
	db := newP07Database(t, "phase36_physical_delete")
	store := NewGORMStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	oldPayload := now.Add(-31 * 24 * time.Hour)
	oldAudit := now.Add(-181 * 24 * time.Hour)

	terminal := fixture36RetentionRun("run-phase36-terminal", RunStatusSucceeded, oldAudit)
	parked := fixture36RetentionRun("run-phase36-parked", RunStatusParked, oldAudit)
	parked.FinishedAt = nil
	if err := db.Create(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&parked).Error; err != nil {
		t.Fatal(err)
	}
	for _, run := range []mysql.WorkflowRun{terminal, parked} {
		checkpointID := "checkpoint-" + run.ID
		checkpointKey := "key-" + run.ID
		if err := db.Create(&mysql.WorkflowCheckpoint{
			ID: checkpointID, RunID: run.ID, CheckpointKey: checkpointKey,
			SnapshotJSON: `{"state":"sensitive"}`, CheckpointBlob: []byte("opaque-sensitive"),
			CommittedAt: &oldAudit, CreatedAt: oldAudit, UpdatedAt: oldAudit,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&mysql.WorkflowEvent{RunID: run.ID, Seq: 1, EventType: EventAgentToolResult, Payload: `{"tool_output":"sensitive"}`, PayloadVersion: 1, CreatedAt: oldAudit}).Error; err != nil {
			t.Fatal(err)
		}
	}
	proposal := `{"target":"sensitive"}`
	request, response := proposal, `{"result":"sensitive"}`
	if err := db.Create(&mysql.AgentApproval{
		ID: "approval-phase36", RunID: terminal.ID, ToolName: "block_ip", ToolRevision: "v1",
		ToolSchemaHash: fixture36Hex('a'), RiskLevel: "L2", ProposalJSONRedacted: proposal,
		ProposalHash: fixture36Hex('b'), PolicyHash: fixture36Hex('c'), RuntimeCompatibilityHash: fixture36Hex('d'),
		RequestedBy: "user-phase36", Status: ApprovalStatusApproved, Version: 1,
		PreparingAt: oldAudit, CreatedAt: oldAudit, DecidedAt: &oldAudit,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mysql.AgentEffect{
		ID: "effect-phase36", RunID: terminal.ID, IdempotencyKey: "effect-phase36", EffectRole: "primary", EffectStep: "primary",
		ProposalHash: fixture36Hex('b'), ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: fixture36Hex('a'),
		TargetHash: fixture36Hex('e'), RequestRedacted: &request, ResponseRedacted: &response,
		EffectType: "external", Status: EffectStatusSucceeded, Version: 1, LeaseGeneration: 1,
		FinishedAt: &oldAudit, CreatedAt: oldAudit, UpdatedAt: oldAudit,
	}).Error; err != nil {
		t.Fatal(err)
	}

	counts, err := store.PhysicalDeleteSensitivePayloads(context.Background(), oldPayload, 100)
	if err != nil {
		t.Fatal(err)
	}
	if counts.RunPayloads != 1 || counts.Checkpoints != 1 || counts.EventPayloads != 1 || counts.ApprovalPayloads != 1 || counts.EffectPayloads != 1 {
		t.Fatalf("payload cleanup counts=%+v", counts)
	}
	var gotTerminal, gotParked mysql.WorkflowRun
	if err := db.First(&gotTerminal, "id = ?", terminal.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&gotParked, "id = ?", parked.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotTerminal.QueryText != "" || gotTerminal.InputPayload != "" || gotTerminal.OutputPayload != "" || gotTerminal.ImmutableInputJSON != nil || gotTerminal.ContextSnapshotJSON != nil {
		t.Fatalf("terminal payload survived: %#v", gotTerminal)
	}
	if gotParked.QueryText == "" || gotParked.ImmutableInputJSON == nil {
		t.Fatal("non-terminal parked Run payload was deleted")
	}
	var terminalCheckpoints, parkedCheckpoints int64
	db.Model(&mysql.WorkflowCheckpoint{}).Where("run_id = ?", terminal.ID).Count(&terminalCheckpoints)
	db.Model(&mysql.WorkflowCheckpoint{}).Where("run_id = ?", parked.ID).Count(&parkedCheckpoints)
	if terminalCheckpoints != 0 || parkedCheckpoints != 1 {
		t.Fatalf("checkpoint counts terminal=%d parked=%d", terminalCheckpoints, parkedCheckpoints)
	}

	metadataCounts, err := store.PhysicalDeleteAuditMetadata(context.Background(), now.Add(-180*24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if metadataCounts.Runs != 1 || metadataCounts.Events != 1 || metadataCounts.Approvals != 1 || metadataCounts.Effects != 1 {
		t.Fatalf("audit cleanup counts=%+v", metadataCounts)
	}
	if err := db.First(&gotParked, "id = ?", parked.ID).Error; err != nil {
		t.Fatal("parked Run audit metadata was deleted")
	}
}

func fixture36RetentionRun(id, status string, finished time.Time) mysql.WorkflowRun {
	input := `{"query":"sensitive"}`
	contextSnapshot := `{"history":"sensitive"}`
	version, compatibility := "runtime-v1", fixture36Hex('f')
	return mysql.WorkflowRun{
		ID: id, WorkflowKey: "phase36", UserID: "user-phase36", SessionID: "session-" + id,
		Status: status, RuntimeMode: RuntimeModeDurableV1, AvailableAt: finished, MaxAttempts: 3,
		ImmutableInputJSON: &input, QueryText: "sensitive query", ContextSnapshotJSON: &contextSnapshot,
		RuntimeVersion: &version, RuntimeCompatibilityHash: &compatibility,
		InputPayload: "sensitive input", OutputPayload: "sensitive output",
		StartedAt: finished.Add(-time.Minute), FinishedAt: &finished, CreatedAt: finished, UpdatedAt: finished,
	}
}

func fixture36Hex(ch byte) string { return strings.Repeat(string(ch), 64) }
