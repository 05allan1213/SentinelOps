package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestRuntimeApprovalListsAllLifecycleStates(t *testing.T) {
	runID := "run-approval-lifecycle"
	proposalHash := strings.Repeat("1", 64)
	approvalID, err := policy.ApprovalID(runID, proposalHash)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{workflow.ApprovalStatusPreparing, workflow.ApprovalStatusPending, workflow.ApprovalStatusApproved, workflow.ApprovalStatusRejected, workflow.ApprovalStatusExpired, workflow.ApprovalStatusInvalidated} {
		row := approvalProjectionFixture(runID, approvalID, proposalHash, status)
		item, meta := mapRuntimeApproval(row, []mysql.WorkflowEvent{approvalEvent(row.ID, workflow.EventApprovalRequested, 7)})
		if item.Status != status || meta.DataQuality != v1.DataQualityComplete {
			t.Fatalf("status=%q item=%+v meta=%+v", status, item, meta)
		}
	}
}

func TestRuntimeApprovalProposalBindingIsReadOnly(t *testing.T) {
	runID := "run-approval-readonly"
	proposalHash := strings.Repeat("2", 64)
	approvalID, err := policy.ApprovalID(runID, proposalHash)
	if err != nil {
		t.Fatal(err)
	}
	row := approvalProjectionFixture(runID, approvalID, proposalHash, workflow.ApprovalStatusPending)
	row.ProposalJSONRedacted = `{"safe":"value","password":"plaintext-secret"}`
	item, _ := mapRuntimeApproval(row, []mysql.WorkflowEvent{approvalEvent(row.ID, workflow.EventApprovalRequested, 1)})
	if item.Proposal["safe"] != `value` || item.Proposal["password"] != `[REDACTED]` {
		t.Fatalf("proposal was not safely projected: %#v", item.Proposal)
	}
	if item.ProposalHash != proposalHash || item.ToolRevision != row.ToolRevision {
		t.Fatal("proposal binding changed while mapping")
	}
}

func TestRuntimeEffectBuildsPrimaryDerivedDAG(t *testing.T) {
	runID := "run-effect-dag"
	proposalHash := strings.Repeat("3", 64)
	primaryID, err := policy.EffectKey(runID, proposalHash, workflow.EffectStepPrimary)
	if err != nil {
		t.Fatal(err)
	}
	derivedID, err := policy.EffectKey(runID, proposalHash, "index")
	if err != nil {
		t.Fatal(err)
	}
	parent := primaryID
	rows := []mysql.AgentEffect{
		effectProjectionFixture(runID, primaryID, proposalHash, workflow.EffectRolePrimary, workflow.EffectStepPrimary, nil, workflow.EffectStatusSucceeded),
		effectProjectionFixture(runID, derivedID, proposalHash, workflow.EffectRoleDerived, "index", &parent, workflow.EffectStatusUnknown),
	}
	events := []mysql.WorkflowEvent{effectEvent(primaryID, workflow.EventEffectStarted, workflow.EffectStatusRunning, 2), effectEvent(primaryID, workflow.EventEffectSucceeded, workflow.EffectStatusSucceeded, 3), effectEvent(derivedID, workflow.EventEffectUnknown, workflow.EffectStatusUnknown, 4)}
	primary, primaryMeta := mapRuntimeEffect(rows[0], events)
	derived, derivedMeta := mapRuntimeEffect(rows[1], events)
	if primary.EffectRole != workflow.EffectRolePrimary || primary.ParentEffectID != "" || primaryMeta.DataQuality != v1.DataQualityComplete {
		t.Fatalf("primary=%+v meta=%+v", primary, primaryMeta)
	}
	if derived.EffectRole != workflow.EffectRoleDerived || derived.ParentEffectID != primaryID || derived.Status != workflow.EffectStatusUnknown || derivedMeta.DataQuality != v1.DataQualityComplete {
		t.Fatalf("derived=%+v meta=%+v", derived, derivedMeta)
	}
}

func TestRuntimeEffectHistoryCorrelatesEvents(t *testing.T) {
	runID := "run-effect-history"
	proposalHash := strings.Repeat("4", 64)
	effectID, err := policy.EffectKey(runID, proposalHash, workflow.EffectStepPrimary)
	if err != nil {
		t.Fatal(err)
	}
	effect := effectProjectionFixture(runID, effectID, proposalHash, workflow.EffectRolePrimary, workflow.EffectStepPrimary, nil, workflow.EffectStatusUnknown)
	actor := "operator-1"
	reason := "provider response remains uncertain"
	events := []mysql.WorkflowEvent{
		effectEvent(effectID, workflow.EventEffectUnknown, workflow.EffectStatusUnknown, 9),
		{RunID: runID, Seq: 10, EventType: workflow.EventEffectResolved, Payload: eventPayload(effectID, map[string]any{"effect_id": effectID, "status": workflow.EffectStatusUnknown, "resolved_by": actor, "reason": reason, "evidence_reference": "evidence-1"}), ActorID: &actor, CreatedAt: time.Now()},
	}
	history, ok := buildEffectHistory(effect, events)
	if !ok || len(history) != 2 || history[1].ActorID != actor || history[1].Reason != reason || history[1].EvidenceReference != "evidence-1" {
		t.Fatalf("history=%+v observed=%t", history, ok)
	}
}

func TestRuntimeUnknownNeverSuccess(t *testing.T) {
	runID := "run-effect-unknown"
	proposalHash := strings.Repeat("5", 64)
	effectID, err := policy.EffectKey(runID, proposalHash, workflow.EffectStepPrimary)
	if err != nil {
		t.Fatal(err)
	}
	effect := effectProjectionFixture(runID, effectID, proposalHash, workflow.EffectRolePrimary, workflow.EffectStepPrimary, nil, workflow.EffectStatusUnknown)
	item, _ := mapRuntimeEffect(effect, []mysql.WorkflowEvent{effectEvent(effectID, workflow.EventEffectUnknown, workflow.EffectStatusUnknown, 2)})
	if item.Status == workflow.EffectStatusSucceeded || item.History[0].Status == workflow.EffectStatusSucceeded {
		t.Fatalf("unknown effect was mapped to success: %+v", item)
	}

	effect.Status = workflow.EffectStatusReconciling
	item, _ = mapRuntimeEffect(effect, []mysql.WorkflowEvent{effectEvent(effectID, workflow.EventEffectReconciling, workflow.EffectStatusReconciling, 3)})
	if item.Status == workflow.EffectStatusSucceeded || item.History[0].Status == workflow.EffectStatusSucceeded {
		t.Fatalf("reconciling effect was mapped to success: %+v", item)
	}
}

func TestRuntimeEffectScope(t *testing.T) {
	if err := validateEffectFilter(EffectFilter{Status: workflow.EffectStatusUnknown, EffectRole: workflow.EffectRoleDerived, EffectStep: "index", Page: 1, PageSize: 20}); err != nil {
		t.Fatalf("valid effect filter rejected: %v", err)
	}
	for _, f := range []EffectFilter{{Status: "success"}, {EffectRole: "other"}, {EffectStep: " bad"}, {Attempt: -1}, {PageSize: 101}} {
		if err := validateEffectFilter(f); err == nil {
			t.Fatalf("invalid effect filter accepted: %+v", f)
		}
	}
}

func TestRuntimeReadDoesNotExecuteEffect(t *testing.T) {
	// The aggregate mappers accept only persisted rows/events and have no
	// callback, executor or mutation input.  Keep this regression assertion
	// explicit so future changes cannot accidentally add an execution hook.
	if strings.Contains(fmt.Sprintf("%T", mapRuntimeEffect), "func(context.Context)") {
		t.Fatal("effect read mapper unexpectedly accepts an execution context callback")
	}
}

func approvalProjectionFixture(runID, id, proposalHash, status string) mysql.AgentApproval {
	now := time.Now().UTC()
	return mysql.AgentApproval{ID: id, RunID: runID, ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: strings.Repeat("a", 64), RiskLevel: string(policy.RiskL2), ProposalJSONRedacted: `{"target":"192.0.2.1"}`, ProposalHash: proposalHash, PolicyHash: strings.Repeat("b", 64), RuntimeCompatibilityHash: strings.Repeat("c", 64), RequestedBy: "requester", Status: status, Version: 1, PreparingAt: now, CreatedAt: now}
}

func effectProjectionFixture(runID, id, proposalHash, role, step string, parent *string, status string) mysql.AgentEffect {
	now := time.Now().UTC()
	return mysql.AgentEffect{ID: id, RunID: runID, IdempotencyKey: id, EffectRole: role, EffectStep: step, ParentEffectID: parent, ProposalHash: proposalHash, ToolName: "block_ip", ToolRevision: "v1", ToolSchemaHash: strings.Repeat("a", 64), TargetHash: strings.Repeat("b", 64), EffectType: string(policy.EffectReconcilable), Status: status, Version: 1, LeaseGeneration: 1, Attempt: 1, CreatedAt: now, UpdatedAt: now}
}

func approvalEvent(approvalID, eventType string, seq uint64) mysql.WorkflowEvent {
	return mysql.WorkflowEvent{RunID: "run-approval-lifecycle", Seq: seq, EventType: eventType, Payload: eventPayload(approvalID, map[string]any{"approval_id": approvalID}), PayloadVersion: 1, CreatedAt: time.Now()}
}

func effectEvent(effectID, eventType, status string, seq uint64) mysql.WorkflowEvent {
	return mysql.WorkflowEvent{RunID: "run-effect", Seq: seq, EventType: eventType, Payload: eventPayload(effectID, map[string]any{"effect_id": effectID, "status": status}), PayloadVersion: 1, CreatedAt: time.Now()}
}

func eventPayload(reference string, data map[string]any) string {
	value := map[string]any{"schema": workflow.EventEnvelopeSchema, "reference": reference, "data": data}
	raw, _ := json.Marshal(value)
	return string(raw)
}
