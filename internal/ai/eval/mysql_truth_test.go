package eval

import (
	"reflect"
	"strings"
	"testing"

	aitrace "SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestMetricReuseProjectsWorkflowTruth(t *testing.T) {
	evidenceID := "evidence-v1:" + strings.Repeat("a", 64)
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{EventType: workflow.EventRunResumed, Payload: `{"data":{"mode":"resume"}}`},
		{EventType: workflow.EventAgentToolCall, Payload: `{"data":{"tool_name":"search_events"}}`},
		{EventType: workflow.EventAgentToolResult, Payload: `{"data":{"tool_name":"search_events","success":true}}`},
		{EventType: workflow.EventAgentReplan, Payload: `{"data":{}}`},
		{EventType: workflow.EventEvidenceCited, Payload: `{"data":{"evidence_ids":["` + evidenceID + `"]}}`},
		{EventType: workflow.EventRunFailed, Payload: `{"data":{"error_code":"policy_mutation_disabled"}}`},
	})
	if truth.RecoveryMode != "resume" || truth.ReplanCount != 1 || len(truth.ToolCalls) != 1 || !truth.ToolCalls[0].Success {
		t.Fatalf("projected truth = %+v", truth)
	}
	if !truth.Evidence.Valid || len(truth.Evidence.References) != 1 || !truth.RBACDenied {
		t.Fatalf("evidence/RBAC truth = %+v", truth)
	}
}

func TestMetricReuseDoesNotTreatJSONNumbersAsSecrets(t *testing.T) {
	for _, value := range []string{
		`{"data":{"attempt":1,"lease_generation":1,"runtime_version":"app-v1"}}`,
		`{"data":{"estimate":{"input_tokens":754},"kind":"model_call","metadata":{"input_price":12,"output_price":36}}}`,
	} {
		if looksLikeSecret(value) {
			t.Fatalf("numeric JSON was treated as secret material: %s", value)
		}
	}
}

func TestMetricReuseIgnoresInitialReplaySelection(t *testing.T) {
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{EventType: workflow.EventRunReplayed, Payload: `{"data":{"mode":"replay","attempt":1}}`},
	})
	if truth.RecoveryMode != "" {
		t.Fatalf("initial replay selection was reported as recovery: %q", truth.RecoveryMode)
	}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{EventType: workflow.EventRunReplayed, Payload: `{"data":{"mode":"replay","attempt":2}}`},
	})
	if truth.RecoveryMode != "replay" {
		t.Fatalf("later replay recovery was not reported: %q", truth.RecoveryMode)
	}
}

func TestMetricReuseFailsClosedForInvalidEvidence(t *testing.T) {
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{
			EventType: workflow.EventEvidenceCited,
			Payload:   `{"data":{"evidence_ids":["evidence-v1:bad"],"valid":false}}`,
		},
		{
			EventType: workflow.EventEvidenceRetrieved,
			Payload:   `{"data":{"evidence_ids":["evidence-v1:later"]}}`,
		},
	})
	if truth.Evidence.Valid || len(truth.Evidence.References) != 2 {
		t.Fatalf("invalid Evidence was accepted: %+v", truth.Evidence)
	}
}

func TestMetricReuseDistinguishesRetrievedFromCitedEvidence(t *testing.T) {
	evidenceID := "evidence-v1:" + strings.Repeat("a", 64)
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{{
		EventType: workflow.EventEvidenceRetrieved,
		Payload:   `{"data":{"evidence_ids":["` + evidenceID + `"]}}`,
	}})
	if !truth.Evidence.Valid || truth.Evidence.Cited {
		t.Fatalf("retrieved Evidence projection = %+v", truth.Evidence)
	}
}

func TestMetricReuseRejectsCitedEventWithoutReferences(t *testing.T) {
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{{
		EventType: workflow.EventEvidenceCited,
		Payload:   `{"data":{}}`,
	}})
	if !truth.Evidence.Cited || truth.Evidence.Valid {
		t.Fatalf("empty cited Evidence event was accepted: %+v", truth.Evidence)
	}
}

func TestMetricReuseRejectsMalformedEvidenceReference(t *testing.T) {
	for _, reference := range []string{"evidence-v1:bad", "evidence-v1:" + strings.Repeat("A", 64)} {
		truth := RunTruth{}
		projectEvents(&truth, []mysql.WorkflowEvent{{
			EventType: workflow.EventEvidenceCited,
			Payload:   `{"data":{"evidence_ids":["` + reference + `"]}}`,
		}})
		if truth.Evidence.Valid {
			t.Fatalf("malformed Evidence reference %q was accepted: %+v", reference, truth.Evidence)
		}
	}
}

func TestMetricReuseDetectsSecretsAcrossPersistedTruth(t *testing.T) {
	secret := "Authorization: Bearer persisted-secret"
	value := secret
	checks := map[string]bool{
		"workflow Run": workflowRunContainsSecret(&mysql.WorkflowRun{OutputPayload: secret}),
		"Approval":     approvalContainsSecret(mysql.AgentApproval{DecisionReason: &value}),
		"Effect":       effectContainsSecret(mysql.AgentEffect{ResponseRedacted: &value}),
		"Trace Run":    traceRunContainsSecret(mysql.TraceRun{ErrorMessage: secret}),
		"Trace node":   traceNodesContainSecret([]mysql.TraceNode{{CompletionText: secret}}),
	}
	for name, detected := range checks {
		if !detected {
			t.Fatalf("%s secret was not detected", name)
		}
	}
}

func TestMetricReuseFailsClosedForImplicitToolResultSuccess(t *testing.T) {
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{EventType: workflow.EventAgentToolCall, Payload: `{"data":{"tool_name":"search_events"}}`},
		{EventType: workflow.EventAgentToolResult, Payload: `{"data":{"tool_name":"search_events"}}`},
	})
	if len(truth.ToolCalls) != 1 || truth.ToolCalls[0].Success {
		t.Fatalf("implicit Tool result was treated as success: %+v", truth.ToolCalls)
	}
}

func TestMetricReuseMatchesRepeatedToolResultsOnce(t *testing.T) {
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{EventType: workflow.EventAgentToolCall, Payload: `{"data":{"tool_name":"search_events"}}`},
		{EventType: workflow.EventAgentToolCall, Payload: `{"data":{"tool_name":"search_events"}}`},
		{EventType: workflow.EventAgentToolResult, Payload: `{"data":{"tool_name":"search_events","success":true}}`},
		{EventType: workflow.EventAgentToolResult, Payload: `{"data":{"tool_name":"search_events","success":false}}`},
	})
	if len(truth.ToolCalls) != 2 || truth.ToolCalls[0].Success || !truth.ToolCalls[1].Success {
		t.Fatalf("repeated Tool results were not matched once each: %+v", truth.ToolCalls)
	}
}

func TestMetricReuseRequiresExplicitCompleteTrace(t *testing.T) {
	trace := mysql.TraceRun{
		DurationMs:        42,
		TotalInputTokens:  10,
		TotalOutputTokens: 5,
		Tags:              `{"trace_quality":"unknown"}`,
	}
	if got := projectTraceTruth(workflow.TraceQualityComplete, trace); got.Complete {
		t.Fatalf("unknown Trace quality was accepted: %+v", got)
	}
	trace.Tags = `{"trace_quality":"complete"}`
	if got := projectTraceTruth(workflow.TraceQualityIncomplete, trace); got.Complete {
		t.Fatalf("incomplete workflow truth was accepted: %+v", got)
	}
	if got := projectTraceTruth(workflow.TraceQualityComplete, trace); !got.Complete || got.LatencyMs != 42 || got.InputTokens != 10 || got.OutputTokens != 5 {
		t.Fatalf("complete Trace projection = %+v", got)
	}
}

func TestMetricReuseDoesNotConflateEffectParkingWithRecovery(t *testing.T) {
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{EventType: workflow.EventRunResumed, Payload: `{"data":{"mode":"resume"}}`},
		{EventType: workflow.EventRunParked, Payload: `{"data":{"park_reason":"effect_unknown"}}`},
	})
	if truth.RecoveryMode != "resume" {
		t.Fatalf("effect parking overwrote recovery mode: %+v", truth)
	}

	truth = RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{{
		EventType: workflow.EventRunParked,
		Payload:   `{"data":{"mode":"parked","park_reason":"checkpoint_missing"}}`,
	}})
	if truth.RecoveryMode != "parked" {
		t.Fatalf("recovery parking was not projected: %+v", truth)
	}
}

func TestMetricReuseDoesNotCountBudgetStopsAsActualOverrun(t *testing.T) {
	truth := RunTruth{}
	projectEvents(&truth, []mysql.WorkflowEvent{
		{EventType: workflow.EventBudgetExhausted, Payload: `{"data":{"kind":"model_call"}}`},
		{EventType: workflow.EventBudgetUsageUnknown, Payload: `{"data":{"kind":"model_call"}}`},
	})
	if truth.BudgetExceeded {
		t.Fatalf("budget stop events were treated as actual overrun: %+v", truth)
	}

	result := compare(EvalCase{
		ID: "budget-stop", Query: "safe", Expected: Expected{Statuses: []string{"parked"}},
		Budget: EvalBudget{MaxLatencyMS: 100, MaxTotalTokens: 100, MaxCostCNY: 1},
	}, RunTruth{
		RunID: "run-budget-stop", Status: "parked", Trace: TraceTruth{Complete: true},
	})
	if result.BudgetExceeded || !result.Passed {
		t.Fatalf("budget stop without measured overrun was not accepted: %+v", result)
	}
}

func TestMetricReuseUsesExistingTraceToolStatus(t *testing.T) {
	calls := projectTraceToolCalls([]mysql.TraceNode{
		{NodeType: aitrace.NodeTypeLLM, NodeName: "ignored", Status: aitrace.StatusSuccess},
		{NodeType: aitrace.NodeTypeTool, NodeName: "fallback", Status: aitrace.StatusSuccess, Metadata: `{"tool_name":"search_events"}`},
		{NodeType: aitrace.NodeTypeTool, NodeName: "query_database", Status: aitrace.StatusError},
	})
	want := []ToolCallTruth{{Name: "search_events", Success: true, SuccessKnown: true}, {Name: "query_database", Success: false, SuccessKnown: true}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("Trace Tool calls = %#v, want %#v", calls, want)
	}
}

func TestMetricReuseKeepsEventOnlyToolWhenTraceProjectionIsPartial(t *testing.T) {
	events := []ToolCallTruth{
		{Name: "search_events", Success: false},
		{Name: "query_database", Success: false},
	}
	trace := []ToolCallTruth{{Name: "search_events", Success: true}}
	got := mergeToolCalls(events, trace)
	want := []ToolCallTruth{
		{Name: "search_events", Success: true},
		{Name: "query_database", Success: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged Tool calls = %#v, want %#v", got, want)
	}
}

func TestMetricReuseKeepsExplicitEventFailureOverTraceSuccess(t *testing.T) {
	events := []ToolCallTruth{{Name: "search_events", Success: false, SuccessKnown: true}}
	trace := []ToolCallTruth{{Name: "search_events", Success: true, SuccessKnown: true}}
	got := mergeToolCalls(events, trace)
	if len(got) != 1 || got[0].Success {
		t.Fatalf("explicit Event failure was hidden by Trace success: %#v", got)
	}
}
