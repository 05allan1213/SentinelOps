package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatasetSchemaRequiresVersionedDocument(t *testing.T) {
	dataset := Dataset{
		Schema:  DatasetSchema,
		Version: "2026-08-26",
		Repeat:  3,
		Cases:   makeDatasetCases(8, 5),
	}
	if err := dataset.Validate(); err != nil {
		t.Fatalf("Validate() rejected complete dataset: %v", err)
	}
	badSchema := dataset
	badSchema.Schema = "sentinelops/eval-dataset/v0"
	if err := badSchema.Validate(); err == nil {
		t.Fatal("Validate() accepted an unsupported Dataset schema")
	}
}

func TestDatasetCoverageRequiresEightCategoriesOutcomesAndRepresentatives(t *testing.T) {
	dataset := Dataset{
		Schema:  DatasetSchema,
		Version: "2026-08-26",
		Repeat:  3,
		Cases:   makeDatasetCases(8, 5),
	}
	if err := dataset.Validate(); err != nil {
		t.Fatalf("Validate() rejected complete Dataset coverage: %v", err)
	}

	missing := dataset
	missing.Cases = missing.Cases[:4]
	if err := missing.Validate(); err == nil {
		t.Fatal("Validate() accepted a dataset with fewer than five cases per category")
	}

	duplicate := dataset
	duplicate.Cases = append([]DatasetCase(nil), dataset.Cases...)
	duplicate.Cases[1].ID = duplicate.Cases[0].ID
	if err := duplicate.Validate(); err == nil {
		t.Fatal("Validate() accepted duplicate case IDs")
	}

	noRepresentative := dataset
	noRepresentative.Cases = append([]DatasetCase(nil), dataset.Cases...)
	noRepresentative.Cases[0].Representative = false
	if err := noRepresentative.Validate(); err == nil {
		t.Fatal("Validate() accepted a category without a representative case")
	}
}

func TestDatasetSnapshotRejectsSecretsAndUnqualifiedIdentity(t *testing.T) {
	item := makeDatasetCases(1, 1)[0]
	item.Snapshot.Models[0].CatalogRef = "chat-model"
	if err := item.Validate(); err == nil {
		t.Fatal("DatasetCase.Validate() accepted an unqualified Catalog Ref")
	}

	item = makeDatasetCases(1, 1)[0]
	item.Snapshot.PromptSnapshot = "api_key=plaintext-secret"
	if err := item.Validate(); err == nil {
		t.Fatal("DatasetCase.Validate() accepted secret material in a snapshot")
	}

	item = makeDatasetCases(1, 1)[0]
	duplicateOrder := item.Snapshot.Models[0]
	duplicateOrder.CatalogRef = "provider_b/chat"
	duplicateOrder.Provider = "provider_b"
	item.Snapshot.Models = append(item.Snapshot.Models, duplicateOrder)
	if err := item.Validate(); err == nil {
		t.Fatal("DatasetCase.Validate() accepted duplicate candidate order")
	}
}

func TestLoadDatasetDirectoryRejectsUnknownFieldsAndAggregatesSortedFiles(t *testing.T) {
	first := `schema: sentinelops/eval-dataset/v1
version: "2026-08-26"
repeat: 3
category: routing_conversation
cases:
  - id: routing-success
    outcome: success
    query: say hello
    expected:
      statuses: [succeeded]
    budget: {max_latency_ms: 1000, max_total_tokens: 1000, max_cost_cny: 1}
    external_dependencies: [production-runtime]
    contracts: [query_database_denied]
    snapshot:
      models:
        - catalog_ref: provider_a/chat
          kind: chat
          provider: provider_a
          provider_revision: provider-a-1
          driver: openai_compatible_chat
          driver_revision: driver-1
          model_id: chat
          routing_profile: default
          route_options: {enable_thinking: false}
          pricing_revision: pricing-1
          pricing_currency: CNY
          pricing_unit: per_million_tokens
          input_price: 1
          output_price: 2
          model_snapshot_identity: 0ef7b955e97610d78a75abb62c0c074b4289d03708210c6a213962f518301202
      prompt_snapshot: prompt-1
      tool_snapshot: tool-1
      skill_snapshot: skill-1
      mcp_snapshot: mcp-1
      kb_snapshot: kb-1
`
	if _, err := LoadDataset(strings.NewReader(first)); err != nil {
		t.Fatalf("LoadDataset() error = %v", err)
	}

	unknown := strings.Replace(first, "outcome: success", "outcome: success\n    typo: true", 1)
	if _, err := LoadDataset(strings.NewReader(unknown)); err == nil {
		t.Fatal("LoadDataset() silently ignored an unknown field")
	}
}

func TestBaselineApprovalRequiresIndependentApprovalAndThresholdComparison(t *testing.T) {
	baseline := Baseline{
		Schema:         BaselineSchema,
		ID:             "approved-v1",
		Version:        "2026-08-26",
		DatasetSchema:  DatasetSchema,
		DatasetVersion: "2026-08-26",
		Snapshots:      []SnapshotIdentity{makeSnapshot()},
		Summary:        MetricSummary{Cases: 40, Passed: 38, TaskSuccessRate: 0.95, ToolSelectionPassRate: 0.98, ToolCallSuccessRate: 0.98, P95LatencyMs: 100, AverageTokens: 100, AverageCostCNY: 0.1},
		Thresholds:     DefaultThresholds(),
		Approval:       BaselineApproval{Status: "approved", Role: "approver", Subject: "alice", Reason: "initial approved baseline", ApprovedAt: "2026-08-26T00:00:00Z"},
	}
	if err := baseline.Validate(); err != nil {
		t.Fatalf("Validate() rejected approved baseline: %v", err)
	}

	current := MetricSummary{Cases: 40, Passed: 37, TaskSuccessRate: 0.93, ToolSelectionPassRate: 0.97, ToolCallSuccessRate: 0.97, P95LatencyMs: 115, AverageTokens: 110, AverageCostCNY: 0.11}
	comparison, err := CompareBaseline(current, baseline)
	if err != nil {
		t.Fatalf("CompareBaseline() error = %v", err)
	}
	if !comparison.Passed || len(comparison.Failures) != 0 {
		t.Fatalf("comparison = %+v", comparison)
	}

	baseline.Approval.Status = "pending"
	if _, err := CompareBaseline(current, baseline); err == nil {
		t.Fatal("CompareBaseline() accepted an unapproved baseline")
	}
}

func TestThresholdsRejectInvalidValuesAndEnforceSafety(t *testing.T) {
	thresholds := DefaultThresholds()
	thresholds.MinTaskSuccessRate = 1.1
	if err := thresholds.Validate(); err == nil {
		t.Fatal("Thresholds.Validate() accepted a value above one")
	}

	baseline := Baseline{
		Schema: BaselineSchema, ID: "approved", Version: "v1", DatasetSchema: DatasetSchema, DatasetVersion: "v1",
		Snapshots:  []SnapshotIdentity{makeSnapshot()},
		Summary:    MetricSummary{Cases: 2, Passed: 2, TaskSuccessRate: 1, ToolSelectionPassRate: 1, ToolCallSuccessRate: 1, P95LatencyMs: 200, AverageTokens: 25, AverageCostCNY: 0.15},
		Thresholds: DefaultThresholds(), Approval: BaselineApproval{Status: "approved", Role: "admin", Subject: "admin", Reason: "test", ApprovedAt: "2026-08-26T00:00:00Z"},
	}
	baseline.Summary.Safety.BudgetExceeded = 1
	if err := baseline.Validate(); err == nil {
		t.Fatal("Baseline.Validate() accepted a safety-invariant failure")
	}
	if regressed(0.92, 0.95, 0.03) {
		t.Fatal("task success regression at the three-point allowance was rejected")
	}
	if !regressed(0.919, 0.95, 0.03) {
		t.Fatal("task success regression beyond three percentage points was accepted")
	}
}

func TestLoadDatasetDirectoryAndSampleSelection(t *testing.T) {
	root := t.TempDir()
	for index, category := range DatasetCategories() {
		fragment := `schema: sentinelops/eval-dataset/v1
version: "2026-08-26"
repeat: 3
category: ` + category + `
default_budget: {max_latency_ms: 1000, max_total_tokens: 1000, max_cost_cny: 1}
default_external_dependencies: [production-runtime]
default_contracts: [query_database_denied, l0_event_analysis_read_only, l0_risk_read_only, l0_solve_read_only, policy_mutation_disabled, primary_derived_effect_keys, approval_fingerprint_rejects_resume, legacy_run_claim_excluded, cross_provider_same_model_identity]
default_identity_dimensions: [trace, budget, cost, breaker, eval]
default_snapshot:
  models:
    - kind: chat
      candidate_order: 0
      catalog_ref: provider_a/chat
      provider: provider_a
      provider_revision: provider-a-1
      driver: openai_compatible_chat
      driver_revision: driver-1
      model_id: chat
      routing_profile: default
      route_options: {enable_thinking: false}
      pricing_revision: pricing-1
      pricing_currency: CNY
      pricing_unit: per_million_tokens
      input_price: 1
      output_price: 2
      model_snapshot_identity: 0ef7b955e97610d78a75abb62c0c074b4289d03708210c6a213962f518301202
    - kind: chat
      candidate_order: 1
      catalog_ref: provider_b/chat
      provider: provider_b
      provider_revision: provider-b-1
      driver: openai_compatible_chat
      driver_revision: driver-1
      model_id: chat
      routing_profile: default
      route_options: {enable_thinking: false}
      pricing_revision: pricing-1
      pricing_currency: CNY
      pricing_unit: per_million_tokens
      input_price: 1
      output_price: 2
      model_snapshot_identity: 5880d6c4412a7e353f3d0e46eaeafd0a46cf8cdb976c842be8efc8dc7a2fee69
  prompt_snapshot: prompt-1
  tool_snapshot: tool-1
  skill_snapshot: skill-1
  mcp_snapshot: mcp-1
  kb_snapshot: kb-1
cases:
  - id: ` + category + `-success
    outcome: success
    representative: true
    query: safe
    expected: {statuses: [succeeded]}
  - id: ` + category + `-rejection
    outcome: rejection
    query: reject
    expected: {statuses: [failed]}
  - id: ` + category + `-recovery
    outcome: recovery
    query: recover
    expected: {statuses: [parked], recovery_mode: parked}
  - id: ` + category + `-extra-a
    outcome: success
    query: safe-a
    expected: {statuses: [succeeded]}
  - id: ` + category + `-extra-b
    outcome: success
    query: safe-b
    expected: {statuses: [succeeded]}
`
		if err := os.WriteFile(filepath.Join(root, string(rune('a'+index))+".yaml"), []byte(fragment), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dataset, err := LoadDatasetDirectory(root)
	if err != nil {
		t.Fatalf("LoadDatasetDirectory() error = %v", err)
	}
	if len(dataset.Cases) != 40 || dataset.Repeat != 3 {
		t.Fatalf("dataset shape = %d cases/repeat %d", len(dataset.Cases), dataset.Repeat)
	}
	selected, err := SelectSamplePerCategory(dataset, 1)
	if err != nil {
		t.Fatalf("SelectSamplePerCategory() error = %v", err)
	}
	if len(selected) != 8 {
		t.Fatalf("selected %d cases, want 8", len(selected))
	}
}

func TestSummarizeResultsAndCompareReport(t *testing.T) {
	cases := []EvalCase{
		{ID: "success", Query: "safe", Expected: Expected{Statuses: []string{"succeeded"}, Tools: []string{"search_events"}}},
		{ID: "rejected", Query: "unsafe", Expected: Expected{Statuses: []string{"failed"}}, Forbidden: Forbidden{Tools: []string{"query_database"}, RBACWrite: true}},
	}
	results := []CaseResult{
		{CaseID: "success", Passed: true, ActualTools: []string{"search_events"}, ToolCallCount: 1, SuccessfulToolCalls: 1, LatencyMs: 100, InputTokens: 10, OutputTokens: 5, CostCNY: 0.1},
		{CaseID: "rejected", Passed: true, RBACDenied: true, LatencyMs: 200, InputTokens: 20, OutputTokens: 10, CostCNY: 0.2},
	}
	summary, err := SummarizeResults(cases, results)
	if err != nil {
		t.Fatalf("SummarizeResults() error = %v", err)
	}
	if summary.Cases != 2 || summary.Passed != 2 || summary.TaskSuccessRate != 1 || summary.ToolCallSuccessRate != 1 || summary.P95LatencyMs != 200 {
		t.Fatalf("summary = %+v", summary)
	}

	baseline := Baseline{
		Schema: BaselineSchema, ID: "approved", Version: "v1", DatasetSchema: DatasetSchema, DatasetVersion: "v1",
		Snapshots: []SnapshotIdentity{makeSnapshot()}, Summary: MetricSummary{Cases: 2, Passed: 2, TaskSuccessRate: 1, ToolSelectionPassRate: 1, ToolCallSuccessRate: 1, P95LatencyMs: 200, AverageTokens: 25, AverageCostCNY: 0.15},
		Thresholds: DefaultThresholds(), Approval: BaselineApproval{Status: "approved", Role: "admin", Subject: "admin", Reason: "test", ApprovedAt: "2026-08-26T00:00:00Z"},
	}
	report := EvaluationReport{Schema: ReportSchema, Command: "run", DatasetSchema: DatasetSchema, DatasetVersion: "v1", Snapshots: []SnapshotIdentity{makeSnapshot()}, Repeat: 1, Cases: 2, Passed: 2, Results: []CaseResult{{CaseID: "success"}, {CaseID: "rejected"}}, Summary: summary}
	if _, err := CompareReport(report, baseline); err != nil {
		t.Fatalf("CompareReport() error = %v", err)
	}
	report.DatasetVersion = "other"
	if _, err := CompareReport(report, baseline); err == nil {
		t.Fatal("CompareReport() accepted mismatched Dataset version")
	}
	report.DatasetVersion = "v1"
	report.Snapshots = nil
	if _, err := CompareReport(report, baseline); err == nil {
		t.Fatal("CompareReport() accepted a report without snapshot identity")
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("report JSON = %v", err)
	}
}

func makeDatasetCases(categoryIndex, count int) []DatasetCase {
	categories := DatasetCategories()
	result := make([]DatasetCase, 0, categoryIndex*count)
	for category := 0; category < categoryIndex; category++ {
		for index := 0; index < count; index++ {
			outcome := "success"
			switch index {
			case 1:
				outcome = "rejection"
			case 2:
				outcome = "recovery"
			}
			result = append(result, DatasetCase{
				ID:       categories[category] + "-" + outcome + "-" + string(rune('a'+index)),
				Category: categories[category], Outcome: outcome, Representative: index == 0, Query: "safe synthetic query",
				Expected: Expected{Statuses: []string{"succeeded"}}, Snapshot: makeSnapshot(),
				Budget:               EvalBudget{MaxLatencyMS: 1000, MaxTotalTokens: 1000, MaxCostCNY: 1},
				ExternalDependencies: []string{"production-runtime"},
				Contracts:            append([]string(nil), requiredDatasetContracts...),
				IdentityDimensions:   []string{"trace", "budget", "cost", "breaker", "eval"},
			})
		}
	}
	result[0].Snapshot.Models = append(result[0].Snapshot.Models, ModelCandidate{
		Kind: "chat", CandidateOrder: 1, CatalogRef: "provider_b/chat", Provider: "provider_b", ProviderRevision: "provider-b-1",
		Driver: "openai_compatible_chat", DriverRevision: "driver-1", ModelID: "chat", RoutingProfile: "default",
		RouteOptions: map[string]any{"enable_thinking": false}, PricingRevision: "pricing-1", PricingCurrency: "CNY", PricingUnit: "per_million_tokens", InputPrice: 1, OutputPrice: 2,
		ModelSnapshotIdentity: "5880d6c4412a7e353f3d0e46eaeafd0a46cf8cdb976c842be8efc8dc7a2fee69",
	})
	return result
}

func makeSnapshot() SnapshotIdentity {
	return SnapshotIdentity{
		Models: []ModelCandidate{{
			Kind: "chat", CandidateOrder: 0,
			CatalogRef: "provider_a/chat", Provider: "provider_a", ProviderRevision: "provider-a-1",
			Driver: "openai_compatible_chat", DriverRevision: "driver-1", ModelID: "chat",
			RoutingProfile: "default", PricingRevision: "pricing-1", PricingCurrency: "CNY", PricingUnit: "per_million_tokens", InputPrice: 1, OutputPrice: 2,
			RouteOptions:          map[string]any{"enable_thinking": false},
			ModelSnapshotIdentity: "0ef7b955e97610d78a75abb62c0c074b4289d03708210c6a213962f518301202",
		}},
		PromptSnapshot: "prompt-1", ToolSnapshot: "tool-1", SkillSnapshot: "skill-1", MCPSnapshot: "mcp-1", KBSnapshot: "kb-1",
	}
}
