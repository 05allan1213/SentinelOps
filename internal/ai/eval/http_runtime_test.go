package eval

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductionRuntimeAdapterSelectsConfiguredExecutionIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer viewer-token" {
			t.Fatalf("viewer authorization header was not selected")
		}
		_, _ = writer.Write([]byte(`{"data":{"run_id":"run-viewer","status":"pending"}}`))
	}))
	defer server.Close()
	adapter, err := NewHTTPRuntimeAdapterWithIdentities(server.URL, server.Client(), map[ExecutionIdentity]http.Header{
		ExecutionIdentityViewer: {"Authorization": []string{"Bearer viewer-token"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Submit(t.Context(), EvalCase{ID: "viewer", Query: "safe", ExecutionIdentity: ExecutionIdentityViewer}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if _, err := adapter.Submit(t.Context(), EvalCase{ID: "operator", Query: "safe", ExecutionIdentity: ExecutionIdentityOperator}); err == nil {
		t.Fatal("Submit() accepted an identity without an authorization header")
	}
}

func TestProductionRuntimeAdapterUsesDurableHTTPAPI(t *testing.T) {
	var submittedSessionID string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/chat/v2/runs" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization header was not forwarded")
		}
		var body createRunRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Query != "production path" || body.SessionID == "" {
			t.Fatalf("body = %+v", body)
		}
		submittedSessionID = body.SessionID
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"OK","data":{"run_id":"run-http","status":"pending"}}`))
	}))
	defer server.Close()

	adapter, err := NewHTTPRuntimeAdapter(server.URL, server.Client(), http.Header{"Authorization": []string{"Bearer test-token"}})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := adapter.Submit(context.Background(), EvalCase{ID: strings.Repeat("long-case-id-", 16), Query: "production path"})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if handle.RunID != "run-http" || handle.SessionID == "" {
		t.Fatalf("handle = %+v", handle)
	}
	if len(submittedSessionID) > 64 || !strings.HasPrefix(submittedSessionID, "eval-") {
		t.Fatalf("generated session_id = %q", submittedSessionID)
	}
	adapter.Close()
	if adapter.Header != nil {
		t.Fatalf("Close() retained request headers: %#v", adapter.Header)
	}
}

func TestProductionRuntimeAdapterRejectsCredentialedBaseURL(t *testing.T) {
	if _, err := NewHTTPRuntimeAdapter("https://user:password@example.com", http.DefaultClient, nil); err == nil {
		t.Fatal("credentialed production API base URL was accepted")
	}
}

func TestProductionRuntimeAdapterDoesNotEchoErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "Bearer should-never-appear", http.StatusUnauthorized)
	}))
	defer server.Close()
	adapter, err := NewHTTPRuntimeAdapter(server.URL, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Submit(context.Background(), EvalCase{ID: "failure", Query: "safe"})
	if err == nil || err.Error() != "production Run API returned HTTP 401" {
		t.Fatalf("error = %v", err)
	}
}

func TestProductionRuntimeAdapterDrivesApprovalThroughPublicAPI(t *testing.T) {
	var getCalls, decisionCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+evalBearerToken("approver-user", "approver") {
			t.Fatal("Approval API did not use the approver identity")
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/ops/v1/approvals/approval-1":
			getCalls++
			_, _ = writer.Write([]byte(`{"data":{"item":{"id":"approval-1","run_id":"run-approval","proposal_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","requested_by":"operator-user","status":"pending","version":2}}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/ops/v1/approvals/approval-1/approve":
			decisionCalls++
			var body approvalDecisionRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Version != 2 || body.ProposalHash != strings.Repeat("a", 64) {
				t.Fatalf("Approval decision body = %+v", body)
			}
			_, _ = writer.Write([]byte(`{"data":{"item":{"id":"approval-1","status":"approved"}}}`))
		default:
			t.Fatalf("unexpected Approval request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	adapter, err := NewHTTPRuntimeAdapterWithIdentities(server.URL, server.Client(), map[ExecutionIdentity]http.Header{
		ExecutionIdentityApprover: {"Authorization": []string{"Bearer " + evalBearerToken("approver-user", "approver")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	probe := &scenarioProbeStub{approval: ApprovalTruth{
		ID: "approval-1", RunID: "run-approval", ProposalHash: strings.Repeat("a", 64), Version: 2, RequestedBy: "operator-user",
	}}
	cleanup, err := adapter.DriveScenario(t.Context(), EvalCase{Scenario: Scenario{
		Kind: ScenarioApprovalDecision, Decision: "approve", DecisionIdentity: ExecutionIdentityApprover,
	}}, RunHandle{RunID: "run-approval"}, probe)
	if err != nil {
		t.Fatalf("DriveScenario() error = %v", err)
	}
	if err := cleanup(); err != nil || getCalls != 1 || decisionCalls != 1 {
		t.Fatalf("Approval calls/cleanup = %d/%d/%v", getCalls, decisionCalls, err)
	}
}

func TestProductionRuntimeAdapterRejectsSelfApprovalBeforeHTTP(t *testing.T) {
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { serverCalls++ }))
	defer server.Close()
	adapter, err := NewHTTPRuntimeAdapterWithIdentities(server.URL, server.Client(), map[ExecutionIdentity]http.Header{
		ExecutionIdentityApprover: {"Authorization": []string{"Bearer " + evalBearerToken("same-user", "approver")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.DriveScenario(t.Context(), EvalCase{Scenario: Scenario{
		Kind: ScenarioApprovalDecision, Decision: "approve", DecisionIdentity: ExecutionIdentityApprover,
	}}, RunHandle{RunID: "run-self"}, &scenarioProbeStub{approval: ApprovalTruth{
		ID: "approval-self", RunID: "run-self", ProposalHash: strings.Repeat("b", 64), Version: 1, RequestedBy: "same-user",
	}})
	if err == nil || serverCalls != 0 {
		t.Fatalf("self approval error/calls = %v/%d", err, serverCalls)
	}
}

func TestProductionRuntimeAdapterUsesExplicitFaultController(t *testing.T) {
	fault := &scenarioFaultStub{}
	adapter, err := NewHTTPRuntimeAdapter("http://127.0.0.1:8001", http.DefaultClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Faults = fault
	cleanup, err := adapter.DriveScenario(t.Context(), EvalCase{Scenario: Scenario{Kind: ScenarioPreCheckpointReplay}}, RunHandle{RunID: "run-replay"}, &scenarioProbeStub{
		attempt: AttemptTruth{RunID: "run-replay", LeaseOwner: "worker-a", Attempt: 1},
	})
	if err != nil {
		t.Fatalf("DriveScenario() error = %v", err)
	}
	if !fault.workerSuspended {
		t.Fatal("pre-Checkpoint replay did not suspend the owning Worker")
	}
	if err := cleanup(); err != nil || !fault.restored {
		t.Fatalf("fault cleanup = %v, restored=%t", err, fault.restored)
	}
}

type scenarioProbeStub struct {
	approval ApprovalTruth
	attempt  AttemptTruth
}

func (s *scenarioProbeStub) WaitForApproval(context.Context, string) (ApprovalTruth, error) {
	return s.approval, nil
}

func (s *scenarioProbeStub) WaitForCheckpoint(context.Context, string) (AttemptTruth, error) {
	return s.attempt, nil
}

func (s *scenarioProbeStub) WaitForRunningWithoutCheckpoint(context.Context, string) (AttemptTruth, error) {
	return s.attempt, nil
}

func (s *scenarioProbeStub) WaitForDependencyCall(context.Context, string, string) (AttemptTruth, error) {
	return s.attempt, nil
}

type scenarioFaultStub struct {
	workerSuspended     bool
	dependencySuspended bool
	restored            bool
}

func (s *scenarioFaultStub) SuspendWorker(context.Context, AttemptTruth) (func() error, error) {
	s.workerSuspended = true
	return func() error { s.restored = true; return nil }, nil
}

func (s *scenarioFaultStub) SuspendDependency(context.Context, string, AttemptTruth) (func() error, error) {
	s.dependencySuspended = true
	return func() error { s.restored = true; return nil }, nil
}

func evalBearerToken(userID, role string) string {
	payload, _ := json.Marshal(map[string]string{"uid": userID, "role": role})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
