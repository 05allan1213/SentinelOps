package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestEvalCaseValidationRejectsMissingFieldsAndSecrets(t *testing.T) {
	cases := []struct {
		name string
		item EvalCase
	}{
		{name: "missing id", item: EvalCase{Query: "hello"}},
		{name: "missing query", item: EvalCase{ID: "case-1"}},
		{name: "missing terminal expectation", item: EvalCase{ID: "case-2", Query: "hello"}},
		{name: "secret in query", item: EvalCase{ID: "case-3", Query: "api_key=sk-test-secret", Expected: Expected{Statuses: []string{"succeeded"}}}},
		{name: "secret in case id", item: EvalCase{ID: "password=case-secret", Query: "hello", Expected: Expected{Statuses: []string{"succeeded"}}}},
		{name: "invalid recovery mode", item: EvalCase{ID: "case-4", Query: "hello", Expected: Expected{Statuses: []string{"succeeded"}, RecoveryMode: "restart"}}},
		{name: "ambiguous tool contract", item: EvalCase{ID: "case-5", Query: "hello", Expected: Expected{Statuses: []string{"succeeded"}, Tools: []string{"search_events"}}, Forbidden: Forbidden{Tools: []string{"search_events"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.item.Validate(); err == nil {
				t.Fatalf("Validate() accepted invalid case: %+v", tc.item)
			}
		})
	}
}

func TestEvaluateCaseRejectsMismatchedTruthRunID(t *testing.T) {
	truth := &scriptedTruth{truth: RunTruth{
		RunID:  "run-other",
		Status: "succeeded",
		Trace:  TraceTruth{Complete: true},
	}}
	_, err := EvaluateCase(context.Background(), &scriptedRuntime{runID: "run-expected"}, truth, EvalCase{
		ID: "run-identity", Query: "safe", Expected: Expected{Statuses: []string{"succeeded"}},
	})
	if err == nil {
		t.Fatal("EvaluateCase() accepted truth for a different Run")
	}
}

func TestEvaluateCaseFailsFastOnUnexpectedApprovalWait(t *testing.T) {
	truth := &scriptedApprovalWaitTruth{}
	result, err := EvaluateCase(context.Background(), &scriptedRuntime{runID: "run-wait"}, truth, EvalCase{
		ID: "approval-wait", Query: "safe", Expected: Expected{Statuses: []string{"failed"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if result.Passed || result.Status != "waiting_approval" {
		t.Fatalf("unexpected approval wait result = %+v", result)
	}
	if result.Retries != 2 || len(result.DiscardedRunIDs) != 2 || len(result.Failures) == 0 {
		t.Fatalf("unexpected approval wait retries = %+v", result)
	}
}

func TestEvaluateCaseReportsFailureWhenScenarioRetriesExhausted(t *testing.T) {
	runtime := &scriptedRetryScenarioRuntime{scriptedRuntime: scriptedRuntime{runID: "run-scenario-exhausted"}, failures: 3}
	truth := &scriptedScenarioTruth{
		scriptedTruth: scriptedTruth{truth: RunTruth{RunID: "run-scenario-exhausted", Status: "succeeded", Trace: TraceTruth{Complete: true}}},
	}
	result, err := EvaluateCase(context.Background(), runtime, truth, EvalCase{
		ID: "scenario-exhausted", Query: "safe",
		Scenario: Scenario{Kind: ScenarioPreCheckpointReplay}, Expected: Expected{Statuses: []string{"succeeded"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if result.Passed || result.Retries != 2 || len(result.DiscardedRunIDs) != 2 || len(result.Failures) == 0 {
		t.Fatalf("exhausted scenario retries result = %+v", result)
	}
}

func TestLoadCasesReadsVersionedYAML(t *testing.T) {
	input := strings.NewReader(`schema: sentinelops/eval-case/v1
cases:
  - id: hello
    query: say hello
    expected:
      statuses: [succeeded]
      tools: []
    forbidden:
      tools: [query_database]
`)
	cases, err := LoadCases(input)
	if err != nil {
		t.Fatalf("LoadCases() error = %v", err)
	}
	if len(cases) != 1 || cases[0].ID != "hello" || cases[0].Expected.Statuses[0] != "succeeded" {
		t.Fatalf("loaded cases = %+v", cases)
	}
}

func TestLoadCasesRejectsUnknownSecurityFields(t *testing.T) {
	input := strings.NewReader(`schema: sentinelops/eval-case/v1
cases:
  - id: typo
    query: safe
    expected:
      statuses: [succeeded]
    forbiden:
      tools: [query_database]
`)
	if _, err := LoadCases(input); err == nil {
		t.Fatal("LoadCases() silently ignored an unknown security field")
	}
}

func TestEvalSecretDetectionCoversStructuredValuesButAllowsReferences(t *testing.T) {
	if !looksLikeSecret(`{"password":"plaintext"}`) {
		t.Fatal("structured plaintext secret was not detected")
	}
	if looksLikeSecret(`{"credential_ref":"env:EVAL_CREDENTIAL"}`) {
		t.Fatal("valid SecretRef was treated as plaintext secret")
	}
}

func TestEvaluateCaseUsesProductionRuntimeAndTruthMetrics(t *testing.T) {
	adapter := &scriptedRuntime{
		runID: "run-1",
	}
	truth := &scriptedTruth{
		truth: RunTruth{
			RunID:        "run-1",
			Status:       "succeeded",
			Trace:        TraceTruth{Complete: true, LatencyMs: 42, InputTokens: 10, OutputTokens: 5, CostCNY: 0.01},
			ToolCalls:    []ToolCallTruth{{Name: "search_events", Success: true}},
			RecoveryMode: "resume",
			ReplanCount:  1,
			Evidence:     EvidenceTruth{Valid: true, Cited: true, References: []string{"evidence-v1:" + strings.Repeat("a", 64)}},
			RBACDenied:   true,
			Approval:     true,
			Effect:       true,
		},
	}
	caseDef := EvalCase{
		ID:    "case-1",
		Query: "find an event",
		Expected: Expected{
			Statuses:      []string{"succeeded"},
			Tools:         []string{"search_events"},
			RecoveryMode:  "resume",
			EvidenceValid: true,
		},
	}
	result, err := EvaluateCase(context.Background(), adapter, truth, caseDef)
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if !result.Passed || !result.ExecutionSuccess || result.ToolCallSuccessRate != 1 || result.ReplanCount != 1 || result.LatencyMs != 42 {
		t.Fatalf("result = %+v", result)
	}
	if !result.RBACDenied || !result.ApprovalObserved || !result.EffectObserved {
		t.Fatalf("control-plane truth was not reported: %+v", result)
	}
	if adapter.got.ID != caseDef.ID || truth.gotRunID != "run-1" {
		t.Fatalf("adapter/truth calls = %+v/%q", adapter.got, truth.gotRunID)
	}
}

func TestEvaluateCaseDrivesAndRestoresDeclaredScenario(t *testing.T) {
	runtime := &scriptedScenarioRuntime{scriptedRuntime: scriptedRuntime{runID: "run-scenario"}}
	truth := &scriptedScenarioTruth{
		scriptedTruth: scriptedTruth{truth: RunTruth{RunID: "run-scenario", Status: "succeeded", Trace: TraceTruth{Complete: true}}},
	}
	result, err := EvaluateCase(t.Context(), runtime, truth, EvalCase{
		ID: "scenario", Query: "safe", ExecutionIdentity: ExecutionIdentityOperator,
		Scenario: Scenario{Kind: ScenarioPreCheckpointReplay}, Expected: Expected{Statuses: []string{"succeeded"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if !result.Passed || !runtime.drove || !runtime.restored {
		t.Fatalf("scenario result/runtime = %+v/%+v", result, runtime)
	}
}

func TestEvaluateCaseRequiresCitedEvidence(t *testing.T) {
	truth := &scriptedTruth{truth: RunTruth{
		RunID: "run-retrieval-only", Status: "succeeded", Trace: TraceTruth{Complete: true},
		Evidence: EvidenceTruth{Valid: true, References: []string{"evidence-v1:" + strings.Repeat("a", 64)}},
	}}
	result, err := EvaluateCase(context.Background(), &scriptedRuntime{runID: "run-retrieval-only"}, truth, EvalCase{
		ID: "citation-required", Query: "safe",
		Expected: Expected{Statuses: []string{"succeeded"}, EvidenceValid: true},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if result.Passed || result.EvidenceValid {
		t.Fatalf("retrieval-only Evidence satisfied citation expectation: %+v", result)
	}
}

func TestEvaluateCaseFailsClosedForPersistedInvalidEvidence(t *testing.T) {
	truth := &scriptedTruth{truth: RunTruth{
		RunID:    "run-evidence",
		Status:   "succeeded",
		Trace:    TraceTruth{Complete: true},
		Evidence: EvidenceTruth{Valid: false, References: []string{"evidence-v1:bad"}},
	}}
	result, err := EvaluateCase(context.Background(), &scriptedRuntime{runID: "run-evidence"}, truth, EvalCase{
		ID: "invalid-evidence", Query: "safe", Expected: Expected{Statuses: []string{"succeeded"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if result.Passed || len(result.Failures) != 1 || result.Failures[0] != "persisted evidence references are invalid" {
		t.Fatalf("invalid Evidence result = %+v", result)
	}
}

func TestEvaluateCaseFailsClosedForForbiddenToolAndIncompleteTrace(t *testing.T) {
	truth := &scriptedTruth{truth: RunTruth{
		RunID:     "run-2",
		Status:    "succeeded",
		Trace:     TraceTruth{Complete: false},
		ToolCalls: []ToolCallTruth{{Name: "query_database", Success: true}},
	}}
	result, err := EvaluateCase(context.Background(), &scriptedRuntime{runID: "run-2"}, truth, EvalCase{
		ID:    "case-2",
		Query: "unsafe",
		Expected: Expected{
			Statuses: []string{"succeeded"},
		},
		Forbidden: Forbidden{Tools: []string{"query_database"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if result.Passed || result.ExecutionSuccess || len(result.Failures) < 2 {
		t.Fatalf("fail-closed result = %+v", result)
	}
}

func TestEvaluateCaseAlwaysFailsDuplicateEffectAndSecretLeak(t *testing.T) {
	truth := &scriptedTruth{truth: RunTruth{
		RunID: "run-invariant", Status: "succeeded", Trace: TraceTruth{Complete: true},
		DuplicateEffect: true, SecretLeak: true,
	}}
	result, err := EvaluateCase(context.Background(), &scriptedRuntime{runID: "run-invariant"}, truth, EvalCase{
		ID: "global-invariants", Query: "safe", Expected: Expected{Statuses: []string{"succeeded"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if result.Passed || len(result.Failures) != 2 {
		t.Fatalf("global safety invariants were not enforced: %+v", result)
	}
}

func TestEvaluateCaseBoundedRetryRecordsDiscardedRuns(t *testing.T) {
	runtime := &scriptedRetryRuntime{}
	truth := &scriptedRetryTruth{succeedAfter: 2}
	result, err := EvaluateCase(context.Background(), runtime, truth, EvalCase{
		ID: "flake", Query: "safe", Expected: Expected{Statuses: []string{"succeeded"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if !result.Passed || result.Retries != 2 || len(result.DiscardedRunIDs) != 2 {
		t.Fatalf("retry result = %+v", result)
	}
	if result.DiscardedRunIDs[0] != "run-flake-0" || result.DiscardedRunIDs[1] != "run-flake-1" {
		t.Fatalf("discarded runs = %v", result.DiscardedRunIDs)
	}
}

func TestEvaluateCaseRetriesScenarioTerminalBeforePrecondition(t *testing.T) {
	runtime := &scriptedRetryScenarioRuntime{scriptedRuntime: scriptedRuntime{runID: "run-scenario-retry"}, failures: 1}
	truth := &scriptedRetryTruth{succeedAfter: 0}
	result, err := EvaluateCase(t.Context(), runtime, truth, EvalCase{
		ID: "recovery", Query: "safe", ExecutionIdentity: ExecutionIdentityOperator,
		Scenario: Scenario{
			Kind: ScenarioCheckpointResume, Decision: "approve",
			DecisionIdentity: ExecutionIdentityApprover,
		},
		Expected: Expected{Statuses: []string{"succeeded"}},
	})
	if err != nil {
		t.Fatalf("EvaluateCase() error = %v", err)
	}
	if !result.Passed || result.Retries != 1 || len(result.DiscardedRunIDs) != 1 {
		t.Fatalf("scenario retry result = %+v", result)
	}
}

type scriptedRuntime struct {
	runID string
	got   EvalCase
}

func (s *scriptedRuntime) Submit(_ context.Context, item EvalCase) (RunHandle, error) {
	s.got = item
	return RunHandle{RunID: s.runID, Status: "pending"}, nil
}

type scriptedTruth struct {
	truth    RunTruth
	gotRunID string
}

func (s *scriptedTruth) Wait(_ context.Context, runID string) (RunTruth, error) {
	s.gotRunID = runID
	return s.truth, nil
}

type scriptedApprovalWaitTruth struct{}

func (s *scriptedApprovalWaitTruth) Wait(_ context.Context, runID string) (RunTruth, error) {
	return RunTruth{}, ErrUnexpectedApprovalWait
}

type scriptedScenarioRuntime struct {
	scriptedRuntime
	drove    bool
	restored bool
}

func (s *scriptedScenarioRuntime) DriveScenario(context.Context, EvalCase, RunHandle, ScenarioProbe) (func() error, error) {
	s.drove = true
	return func() error {
		s.restored = true
		return nil
	}, nil
}

type scriptedScenarioTruth struct {
	scriptedTruth
	scenarioProbeStub
}

type scriptedRetryRuntime struct {
	calls int
}

func (s *scriptedRetryRuntime) Submit(_ context.Context, item EvalCase) (RunHandle, error) {
	id := fmt.Sprintf("run-flake-%d", s.calls)
	s.calls++
	return RunHandle{RunID: id, Status: "pending"}, nil
}

type scriptedRetryTruth struct {
	scenarioProbeStub
	succeedAfter int
	calls        int
}

func (s *scriptedRetryTruth) Wait(_ context.Context, runID string) (RunTruth, error) {
	s.calls++
	if s.calls <= s.succeedAfter {
		return RunTruth{RunID: runID, Status: "failed", Trace: TraceTruth{Complete: true}}, nil
	}
	return RunTruth{RunID: runID, Status: "succeeded", Trace: TraceTruth{Complete: true}}, nil
}

type scriptedRetryScenarioRuntime struct {
	scriptedRuntime
	failures int
	calls    int
}

func (s *scriptedRetryScenarioRuntime) DriveScenario(context.Context, EvalCase, RunHandle, ScenarioProbe) (func() error, error) {
	s.calls++
	if s.calls <= s.failures {
		return nil, fmt.Errorf("run reached terminal status before Checkpoint was observed")
	}
	return func() error { return nil }, nil
}
