package runtime

import (
	"errors"
	"strings"
	"testing"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestCanonicalStatusMapping(t *testing.T) {
	tests := []struct {
		raw, mode string
		want      v1.RuntimeStatus
		quality   v1.DataQuality
	}{
		{workflow.RunStatusPending, workflow.RuntimeModeDurableV1, v1.RuntimeStatusPending, v1.DataQualityComplete},
		{workflow.RunStatusRunning, workflow.RuntimeModeDurableV1, v1.RuntimeStatusRunning, v1.DataQualityComplete},
		{workflow.RunStatusWaitingApproval, workflow.RuntimeModeDurableV1, v1.RuntimeStatusWaitingApproval, v1.DataQualityComplete},
		{workflow.RunStatusRetryableFailed, workflow.RuntimeModeDurableV1, v1.RuntimeStatusRetryableFailed, v1.DataQualityComplete},
		{workflow.RunStatusParked, workflow.RuntimeModeDurableV1, v1.RuntimeStatusParked, v1.DataQualityComplete},
		{workflow.RunStatusReconciling, workflow.RuntimeModeDurableV1, v1.RuntimeStatusReconciling, v1.DataQualityComplete},
		{workflow.RunStatusSucceeded, workflow.RuntimeModeDurableV1, v1.RuntimeStatusSucceeded, v1.DataQualityComplete},
		{workflow.RunStatusFailed, workflow.RuntimeModeDurableV1, v1.RuntimeStatusFailed, v1.DataQualityComplete},
		{workflow.RunStatusCanceled, workflow.RuntimeModeDurableV1, v1.RuntimeStatusCanceled, v1.DataQualityComplete},
		{workflow.RunStatusSuccess, workflow.RuntimeModeLegacy, v1.RuntimeStatusSucceeded, v1.DataQualityPartial},
		{workflow.RunStatusSuccess, workflow.RuntimeModeDurableV1, "", v1.DataQualityUnknown},
		{"future", workflow.RuntimeModeDurableV1, "", v1.DataQualityUnknown},
	}
	for _, tt := range tests {
		got := MapCanonicalStatus(tt.raw, tt.mode)
		if got.Status != tt.want || got.DataQuality != tt.quality || (tt.want == "" && got.Known) {
			t.Errorf("%q/%q => %#v", tt.raw, tt.mode, got)
		}
		if tt.want == "" && got.Status.Valid() {
			t.Errorf("unknown status unexpectedly valid: %#v", got)
		}
	}
}

func TestCurrentPhasePrecedence(t *testing.T) {
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusSucceeded}, AttemptFacts{Active: true, Recovery: true}, EventFacts{Type: workflow.EventAgentToolCall}); got != v1.CurrentPhaseCompleted {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusRunning}, AttemptFacts{Active: true, Recovery: true}, EventFacts{}); got != v1.CurrentPhaseRecovering {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusRunning}, AttemptFacts{}, EventFacts{Type: workflow.EventApprovalRequested}); got != v1.CurrentPhaseWaitingApproval {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusRunning}, AttemptFacts{}, EventFacts{Type: workflow.EventRunReconciling}); got != v1.CurrentPhaseReconciling {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusRunning}, AttemptFacts{}, EventFacts{Type: workflow.EventAgentPlan}); got != v1.CurrentPhasePlanning {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusRunning}, AttemptFacts{}, EventFacts{Type: workflow.EventAgentToolCall}); got != v1.CurrentPhaseExecuting {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusParked}, AttemptFacts{}, EventFacts{}); got != v1.CurrentPhaseUnknown {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusSuccess, RuntimeMode: workflow.RuntimeModeDurableV1}, AttemptFacts{}, EventFacts{}); got != v1.CurrentPhaseUnknown {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: workflow.RunStatusSuccess, RuntimeMode: workflow.RuntimeModeLegacy}, AttemptFacts{}, EventFacts{}); got != v1.CurrentPhaseCompleted {
		t.Fatal(got)
	}
	if got := CurrentPhaseFromFacts(RunFacts{Status: "future", RuntimeMode: workflow.RuntimeModeDurableV1}, AttemptFacts{OperationActive: true}, EventFacts{Type: workflow.EventAgentToolCall}); got != v1.CurrentPhaseUnknown {
		t.Fatal(got)
	}
}

func TestResourceMetaAndRuntimeEvent(t *testing.T) {
	meta, err := MapResourceMeta(nil, ProjectionState{Available: true, Complete: true})
	if err != nil || meta.Availability != v1.AvailabilityAvailable {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if _, err := MapResourceMeta(ErrRuntimeForbidden, ProjectionState{}); !errors.Is(err, ErrRuntimeForbidden) {
		t.Fatalf("err=%v", err)
	}
	if _, err := MapResourceMeta(ErrRuntimeOperationConflict, ProjectionState{Available: true}); !errors.Is(err, ErrRuntimeOperationConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	reconstructed, err := MapResourceMeta(nil, ProjectionState{Available: true, Reconstructed: true, NotRun: true})
	if err != nil || reconstructed.Availability != v1.AvailabilityPartial || reconstructed.DataQuality != v1.DataQualityReconstructed || !reconstructed.NotRun {
		t.Fatalf("reconstructed=%#v err=%v", reconstructed, err)
	}
	partial, err := MapResourceMeta(errors.New("optional source timeout"), ProjectionState{Available: true})
	if err != nil || partial.Availability != v1.AvailabilityPartial || partial.ReasonCode != "source_error" {
		t.Fatalf("partial=%#v err=%v", partial, err)
	}
	e := mysql.WorkflowEvent{Seq: 7, EventType: workflow.EventAgentToolCall, PayloadVersion: workflow.EventPayloadVersion, TraceID: "trace", Payload: `{"schema":"sentinelops/workflow-event/v1","summary":"password=secret","data":{"status":"ok","reason":"Authorization: Bearer secret-token","password":"secret","unknown":"drop"}}`}
	got := MapRuntimeEvent(e)
	if got.Seq != 7 || got.EventType != workflow.EventAgentToolCall || got.Attributes["status"] != "ok" || got.Attributes["unknown"] != "" || got.Attributes["password"] != "" || got.Summary == "password=secret" || got.Attributes["reason"] == "Authorization: Bearer secret-token" {
		t.Fatalf("event=%#v", got)
	}
}

func TestRuntimeEventKeepsUnknownAndMalformedRows(t *testing.T) {
	unknown := MapRuntimeEvent(mysql.WorkflowEvent{Seq: 8, EventType: "future.event", PayloadVersion: workflow.EventPayloadVersion, Payload: `{"schema":"sentinelops/workflow-event/v1","data":{"status":"future"}}`})
	if unknown.Seq != 8 || unknown.ResourceMeta.Availability != v1.AvailabilityPartial {
		t.Fatalf("unknown=%#v", unknown)
	}
	malformed := MapRuntimeEvent(mysql.WorkflowEvent{Seq: 9, EventType: workflow.EventAgentPlan, Payload: `{broken`})
	if malformed.Seq != 9 || malformed.ResourceMeta.DataQuality != v1.DataQualityUnknown {
		t.Fatalf("malformed=%#v", malformed)
	}
	legacy := MapRuntimeEvent(mysql.WorkflowEvent{Seq: 10, EventType: workflow.EventRunResumed, Payload: `{"attempt":2,"generation":3,"operation_id":"op-safe","secret":"drop"}`})
	if legacy.Attempt != 2 || legacy.Generation != 3 || legacy.OperationID != "op-safe" || legacy.Attributes["secret"] != "" {
		t.Fatalf("legacy=%#v", legacy)
	}
	nonscalar := MapRuntimeEvent(mysql.WorkflowEvent{Seq: 11, EventType: workflow.EventRunResumed, Payload: `{"reason":{"nested":"drop"}}`})
	if nonscalar.Attributes["reason"] != "" {
		t.Fatalf("non-scalar attribute retained: %#v", nonscalar)
	}
	allowlist := MapRuntimeEvent(mysql.WorkflowEvent{Seq: 12, EventType: workflow.EventEffectStarted, Payload: `{"checkpoint_id":"cp","effect_role":"primary","runtime_version":"v1","input_tokens":12,"owner":"drop","prompt":"drop","provider":"drop","actual":"drop"}`})
	if allowlist.Attributes["checkpoint_id"] != "cp" || allowlist.Attributes["effect_role"] != "primary" || allowlist.Attributes["runtime_version"] != "v1" || allowlist.Attributes["input_tokens"] != "12" {
		t.Fatalf("safe keys missing: %#v", allowlist)
	}
	for _, key := range []string{"owner", "prompt", "provider", "actual"} {
		if allowlist.Attributes[key] != "" {
			t.Fatalf("unsafe key retained: %s=%q", key, allowlist.Attributes[key])
		}
	}
	list := MapRuntimeEvent(mysql.WorkflowEvent{Seq: 13, EventType: workflow.EventEvidenceRetrieved, Payload: `{"evidence_ids":["ev-1","ev-2"]}`})
	if list.Attributes["evidence_ids"] != `["ev-1","ev-2"]` {
		t.Fatalf("list attr=%#v", list.Attributes)
	}
	budget := MapRuntimeEvent(mysql.WorkflowEvent{Seq: 14, EventType: workflow.EventBudgetSettled, Payload: `{"subject":"model-call","reservation_identity":"reserve-1","agent_name":"planner","step_count":2,"retryable":true,"metadata":{"catalog_ref":"provider:model","provider":"dashscope","tool_name":"safe-tool","secret":"drop"},"estimate":{"input_tokens":10,"concurrency":2,"raw":"drop"},"actual":{"cached_input_tokens":3,"cost_cny":0.5,"tool_output":"drop"}}`})
	for key, want := range map[string]string{"subject": "model-call", "reservation_identity": "reserve-1", "agent_name": "planner", "step_count": "2", "retryable": "true"} {
		if budget.Attributes[key] != want {
			t.Fatalf("budget %s=%q want %q: %#v", key, budget.Attributes[key], want, budget)
		}
	}
	for key, forbidden := range map[string]string{"metadata": "secret", "estimate": "raw", "actual": "tool_output"} {
		if strings.Contains(budget.Attributes[key], forbidden) {
			t.Fatalf("nested raw key retained: %s=%q", key, budget.Attributes[key])
		}
	}
	if !strings.Contains(budget.Attributes["metadata"], `"catalog_ref":"provider:model"`) || !strings.Contains(budget.Attributes["estimate"], `"input_tokens":10`) || !strings.Contains(budget.Attributes["actual"], `"cached_input_tokens":3`) {
		t.Fatalf("nested safe fields missing: %#v", budget.Attributes)
	}
	for index, payload := range []string{`{"attempt":-1}`, `{"generation":1.5}`, `{"operation_id":7}`, `{"operation_id":"password=secret"}`} {
		mapped := MapRuntimeEvent(mysql.WorkflowEvent{Seq: uint64(20 + index), EventType: workflow.EventRunResumed, Payload: payload})
		if mapped.ResourceMeta.ReasonCode != "invalid_event_correlation" || mapped.ResourceMeta.Availability != v1.AvailabilityPartial {
			t.Fatalf("invalid correlation retained: %#v", mapped)
		}
	}
}
