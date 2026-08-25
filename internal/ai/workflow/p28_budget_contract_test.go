package workflow

import (
	"testing"
	"time"
)

func TestControlPlaneReservationKindsShareBasePrimitive(t *testing.T) {
	for _, kind := range []BaseBudgetKind{BaseBudgetKindPlanner, BaseBudgetKindExecutor, BaseBudgetKindReplanner, BaseBudgetKindRetry, BaseBudgetKindFailover, BaseBudgetKindMCP, BaseBudgetKindRAG, BaseBudgetKindSkill} {
		if used, limit := budgetKindUsageLimit(BaseBudgetLimits{}, BaseBudgetUsage{}, kind); used != 0 || limit != 0 {
			t.Fatalf("kind %q unexpectedly has a separate budget path", kind)
		}
	}
}

func TestBudgetDimensionsUseSharedReservationPrimitive(t *testing.T) {
	limits := BaseBudgetLimits{
		MaxModelCalls: 10, MaxL0ToolCalls: 10, MaxDurationMS: 1000,
		MaxPlannerRounds: 1, MaxRetryCalls: 2, MaxInputTokens: 8, MaxOutputTokens: 6,
		MaxMCPConcurrency: 2, MaxRAGDocuments: 3,
	}
	usage := BaseBudgetUsage{Schema: BaseBudgetSchema, PlannerRounds: 1, InputTokens: 7, RAGDocuments: 3}
	if got := baseBudgetCountExhaustedReason(limits, usage, BaseBudgetKindPlanner); got != "planner" {
		t.Fatalf("planner hard stop = %q", got)
	}
	if got := baseBudgetCountExhaustedReason(limits, usage, BaseBudgetKindRAG); got != "rag_documents" {
		t.Fatalf("rag hard stop = %q", got)
	}
	if got := baseBudgetEstimateExhaustedReason(limits, usage, ReserveBaseBudgetInput{Kind: BaseBudgetKindModelCall, Estimate: BaseBudgetEstimate{InputTokens: 2}}); got != "input_tokens" {
		t.Fatalf("conservative token hard stop = %q", got)
	}
	reservations := BaseBudgetReservations{Schema: BaseBudgetSchema, Items: map[string]BaseBudgetReservation{
		"mcp-1": {Identity: "mcp-1", Kind: BaseBudgetKindMCP, State: BaseBudgetReservationPending, Estimate: BaseBudgetEstimate{Concurrency: 2}},
	}}
	if got := baseBudgetConcurrencyExhaustedReason(limits, reservations, ReserveBaseBudgetInput{Kind: BaseBudgetKindMCP, Estimate: BaseBudgetEstimate{Concurrency: 1}}); got != "mcp_concurrency" {
		t.Fatalf("MCP concurrency hard stop = %q", got)
	}
}

func TestSettlementActualDoesNotDoubleCountDetails(t *testing.T) {
	actual := sanitizeBudgetActual(BaseBudgetActual{InputTokens: 10, CachedInputTokens: 20, OutputTokens: 8, ReasoningTokens: 12, CostCNY: -1})
	if actual.CachedInputTokens != 10 || actual.ReasoningTokens != 8 || actual.CostCNY != 0 {
		t.Fatalf("sanitized actual = %#v", actual)
	}
	usage := BaseBudgetUsage{Schema: BaseBudgetSchema, ModelCalls: 1, InputTokens: actual.InputTokens, CachedInputTokens: actual.CachedInputTokens, OutputTokens: actual.OutputTokens, ReasoningTokens: actual.ReasoningTokens}
	now := time.Now().UTC()
	reservations := BaseBudgetReservations{Schema: BaseBudgetSchema, Items: map[string]BaseBudgetReservation{
		"model": {Identity: "model", Kind: BaseBudgetKindModelCall, Subject: "provider/chat", Metadata: BaseBudgetMetadata{CatalogRef: "provider/chat", Provider: "provider", Driver: "driver", ModelID: "chat", Profile: "default", SnapshotIdentity: "snapshot"}, State: BaseBudgetReservationSettled, Outcome: BaseBudgetOutcomeSucceeded, UsageQuality: "reliable", Actual: &actual, ReservedAt: now, SettledAt: &now, ReservedAttempt: 1, ReservedGeneration: 1},
	}}
	if !validBaseBudgetTruth(usage, reservations) {
		t.Fatal("reliable actual usage should match durable truth")
	}
	usage.OutputTokens++
	if validBaseBudgetTruth(usage, reservations) {
		t.Fatal("double-counted output usage unexpectedly accepted")
	}
}
