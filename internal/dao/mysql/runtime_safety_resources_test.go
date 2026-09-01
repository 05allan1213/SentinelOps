package mysql

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"

	"gorm.io/gorm"
)

func TestRuntimeApprovalQueryReturnsAllLifecycleStatesAndScope(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_safety_approvals")
	now := time.Now().UTC().Truncate(time.Millisecond)
	owned := WorkflowRun{ID: "safety-owned", WorkflowKey: "wf", UserID: "alice", SessionID: "session", Status: "waiting_approval", RuntimeMode: "durable_v1", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	other := WorkflowRun{ID: "safety-other", WorkflowKey: "wf", UserID: "bob", SessionID: "other-session", Status: "waiting_approval", RuntimeMode: "durable_v1", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&[]WorkflowRun{owned, other}).Error; err != nil {
		t.Fatal(err)
	}
	statuses := []string{"preparing", "pending", "approved", "rejected", "expired", "invalidated"}
	rows := make([]AgentApproval, 0, len(statuses))
	for index, status := range statuses {
		proposalHash := strings.Repeat(string(rune('a'+index)), 64)
		id, err := policy.ApprovalID(owned.ID, proposalHash)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, AgentApproval{ID: id, RunID: owned.ID, ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: strings.Repeat("1", 64), RiskLevel: string(policy.RiskL2), ProposalJSONRedacted: `{"target":"192.0.2.1"}`, ProposalHash: proposalHash, PolicyHash: strings.Repeat("2", 64), RuntimeCompatibilityHash: strings.Repeat("3", 64), RequestedBy: "requester", Status: status, Version: 1, PreparingAt: now.Add(time.Duration(index) * time.Second), CreatedAt: now.Add(time.Duration(index) * time.Second)})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	otherHash := strings.Repeat("f", 64)
	otherID, err := policy.ApprovalID(other.ID, otherHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AgentApproval{ID: otherID, RunID: other.ID, ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: strings.Repeat("1", 64), RiskLevel: string(policy.RiskL2), ProposalJSONRedacted: `{}`, ProposalHash: otherHash, PolicyHash: strings.Repeat("2", 64), RuntimeCompatibilityHash: strings.Repeat("3", 64), RequestedBy: "requester", Status: "pending", Version: 1, PreparingAt: now, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	got, total, err := store.ListRuntimeApprovals(ctx, owned.ID, 1, 100)
	if err != nil || total != int64(len(statuses)) || len(got) != len(statuses) {
		t.Fatalf("approvals=%d/%d err=%v", len(got), total, err)
	}
	seen := map[string]bool{}
	for _, row := range got {
		seen[row.Status] = true
	}
	for _, status := range statuses {
		if !seen[status] {
			t.Fatalf("lifecycle status %q missing: %#v", status, seen)
		}
	}
	if rows, total, err := store.ListRuntimeApprovals(ctx, other.ID, 1, 100); err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("cross-scope approvals rows=%d total=%d err=%v, want empty", len(rows), total, err)
	}
}

func TestRuntimeEffectQueryBuildsStablePrimaryDerivedOrderAndFilters(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_safety_effects")
	now := time.Now().UTC().Truncate(time.Millisecond)
	run := WorkflowRun{ID: "safety-effects", WorkflowKey: "wf", UserID: "alice", SessionID: "session", Status: "running", RuntimeMode: "durable_v1", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	proposalHash := strings.Repeat("a", 64)
	primaryID, err := policy.EffectKey(run.ID, proposalHash, "primary")
	if err != nil {
		t.Fatal(err)
	}
	derivedID, err := policy.EffectKey(run.ID, proposalHash, "index")
	if err != nil {
		t.Fatal(err)
	}
	parent := primaryID
	rows := []AgentEffect{
		{ID: derivedID, RunID: run.ID, IdempotencyKey: derivedID, EffectRole: "derived", EffectStep: "index", ParentEffectID: &parent, ProposalHash: proposalHash, ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: strings.Repeat("b", 64), TargetHash: strings.Repeat("c", 64), EffectType: string(policy.EffectReconcilable), Status: "unknown", Version: 1, LeaseGeneration: 2, Attempt: 1, CreatedAt: now, UpdatedAt: now},
		{ID: primaryID, RunID: run.ID, IdempotencyKey: primaryID, EffectRole: "primary", EffectStep: "primary", ProposalHash: proposalHash, ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: strings.Repeat("b", 64), TargetHash: strings.Repeat("c", 64), EffectType: string(policy.EffectTransactionalDB), Status: "succeeded", Version: 3, LeaseGeneration: 1, Attempt: 1, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	got, total, err := store.ListRuntimeEffects(ctx, run.ID, RuntimeEffectFilter{Page: 1, PageSize: 100})
	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("effects=%d/%d err=%v", len(got), total, err)
	}
	if got[0].EffectRole != "primary" || got[1].EffectRole != "derived" || got[1].ParentEffectID == nil || *got[1].ParentEffectID != got[0].ID {
		t.Fatalf("DAG order=%+v", got)
	}
	filtered, total, err := store.ListRuntimeEffects(ctx, run.ID, RuntimeEffectFilter{Status: "unknown", EffectRole: "derived", EffectStep: "index", Attempt: 1, Generation: 2, Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != derivedID {
		t.Fatalf("filtered effects=%+v total=%d err=%v", filtered, total, err)
	}
}

func TestRuntimeEffectLookupAndEventQueryRespectScope(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_safety_scope")
	now := time.Now().UTC().Truncate(time.Millisecond)
	owned := WorkflowRun{ID: "safety-event-owned", WorkflowKey: "wf", UserID: "alice", Status: "running", RuntimeMode: "durable_v1", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	other := WorkflowRun{ID: "safety-event-other", WorkflowKey: "wf", UserID: "bob", Status: "running", RuntimeMode: "durable_v1", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&[]WorkflowRun{owned, other}).Error; err != nil {
		t.Fatal(err)
	}
	proposalHash := strings.Repeat("d", 64)
	effectID, err := policy.EffectKey(other.ID, proposalHash, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&AgentEffect{ID: effectID, RunID: other.ID, IdempotencyKey: effectID, EffectRole: "primary", EffectStep: "primary", ProposalHash: proposalHash, ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: strings.Repeat("1", 64), TargetHash: strings.Repeat("2", 64), EffectType: string(policy.EffectReconcilable), Status: "unknown", Version: 1, LeaseGeneration: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&WorkflowEvent{RunID: other.ID, Seq: 1, EventType: "effect.unknown", Payload: `{}`, PayloadVersion: 1, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	if _, err := store.GetRuntimeEffect(ctx, effectID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-scope effect err=%v, want record not found", err)
	}
	if rows, err := store.ListRuntimeEventsForRun(ctx, other.ID); err != nil || len(rows) != 0 {
		t.Fatalf("cross-scope events rows=%d err=%v, want empty", len(rows), err)
	}
	if _, err := store.ListRuntimeEventsForRun(ctx, owned.ID); err != nil {
		t.Fatalf("owned event query err=%v", err)
	}
}
