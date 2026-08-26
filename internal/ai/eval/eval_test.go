package eval

import (
	"context"
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
