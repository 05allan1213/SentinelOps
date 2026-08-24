package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestRuntimeSnapshotCompatibilityStableAndSensitive(t *testing.T) {
	base := p11SnapshotInput()
	first, err := FreezeRuntimeSnapshot(base)
	if err != nil {
		t.Fatalf("freeze base snapshot: %v", err)
	}

	reordered := p11SnapshotInput()
	reordered.Models[0], reordered.Models[1] = reordered.Models[1], reordered.Models[0]
	reordered.Tools[0], reordered.Tools[1] = reordered.Tools[1], reordered.Tools[0]
	second, err := FreezeRuntimeSnapshot(reordered)
	if err != nil {
		t.Fatalf("freeze reordered snapshot: %v", err)
	}
	if first.CompatibilityHash() != second.CompatibilityHash() || string(first.CanonicalJSON()) != string(second.CanonicalJSON()) {
		t.Fatal("equivalent snapshot order changed canonical identity")
	}

	tests := []struct {
		name   string
		mutate func(*RuntimeSnapshotInput)
	}{
		{"go", func(v *RuntimeSnapshotInput) { v.Runtime.Go = "go1.27.1" }},
		{"eino", func(v *RuntimeSnapshotInput) { v.Runtime.Eino = "v0.9.16" }},
		{"app", func(v *RuntimeSnapshotInput) { v.Runtime.App = "app-rev-2" }},
		{"agent", func(v *RuntimeSnapshotInput) { v.AgentRevision = "agent-rev-2" }},
		{"prompt", func(v *RuntimeSnapshotInput) { v.PromptHash = p11Hash("prompt-2") }},
		{"catalog_ref", func(v *RuntimeSnapshotInput) { v.Models[0].CatalogRef = "provider_a/chat-v2" }},
		{"provider", func(v *RuntimeSnapshotInput) {
			v.Models[0].Provider = "provider_b"
			v.Models[0].CatalogRef = "provider_b/chat"
		}},
		{"driver", func(v *RuntimeSnapshotInput) { v.Models[0].Driver = "another_driver" }},
		{"vendor_model", func(v *RuntimeSnapshotInput) { v.Models[0].ModelID = "vendor-chat-2" }},
		{"profile", func(v *RuntimeSnapshotInput) { v.Models[0].Profile = "reasoning" }},
		{"route_options", func(v *RuntimeSnapshotInput) { enabled := true; v.Models[0].RouteOptions.EnableThinking = &enabled }},
		{"pricing_revision", func(v *RuntimeSnapshotInput) { v.Models[0].Pricing.Revision = "pricing-2" }},
		{"tool_revision", func(v *RuntimeSnapshotInput) { v.Tools[0].Revision = "tool-rev-2" }},
		{"tool_schema", func(v *RuntimeSnapshotInput) { v.Tools[0].SchemaHash = p11Hash("schema-2") }},
		{"mcp", func(v *RuntimeSnapshotInput) { v.MCPCatalogHash = p11Hash("mcp-2") }},
		{"skill", func(v *RuntimeSnapshotInput) { v.Skills[0].ContentHash = p11Hash("skill-2") }},
		{"gate", func(v *RuntimeSnapshotInput) { v.FeatureGates["agent_runtime.enabled"] = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := p11SnapshotInput()
			test.mutate(&changed)
			frozen, err := FreezeRuntimeSnapshot(changed)
			if err != nil {
				t.Fatalf("freeze changed snapshot: %v", err)
			}
			if first.CompatibilityHash() == frozen.CompatibilityHash() {
				t.Fatalf("%s change did not change compatibility hash", test.name)
			}
		})
	}
	if err := RequireExactCompatibility(first.CompatibilityHash(), second.CompatibilityHash()); err != nil {
		t.Fatalf("equal v1 compatibility rejected: %v", err)
	}
	if err := RequireExactCompatibility(first.CompatibilityHash(), strings.Repeat("f", 64)); err == nil {
		t.Fatal("v1 accepted non-exact compatibility")
	}
}

func TestRuntimeSnapshotProviderQualifiedIdentityAndSecretsExcluded(t *testing.T) {
	cfg := &appconfig.Config{
		Providers: map[string]appconfig.Provider{
			"provider_a": {SecretRef: "env:PROVIDER_A_SECRET", Endpoints: map[string]string{appconfig.DriverOpenAICompatibleChat: "https://a.invalid/v1"}},
			"provider_b": {SecretRef: "file:/run/secrets/provider_b", Endpoints: map[string]string{appconfig.DriverOpenAICompatibleChat: "https://b.invalid/v1"}},
		},
		ModelCatalog: map[string]appconfig.Model{
			"provider_a/chat": {ModelID: "shared-vendor-id", Driver: appconfig.DriverOpenAICompatibleChat, Pricing: appconfig.Pricing{Revision: "pricing-1", Currency: "CNY", Unit: "per_million_tokens"}},
			"provider_b/chat": {ModelID: "shared-vendor-id", Driver: appconfig.DriverOpenAICompatibleChat, Pricing: appconfig.Pricing{Revision: "pricing-1", Currency: "CNY", Unit: "per_million_tokens"}},
		},
		Routing: appconfig.Routing{Chat: map[string]appconfig.Route{
			"default":   {Model: "provider_a/chat"},
			"reasoning": {Model: "provider_b/chat"},
		}},
	}
	a, err := ModelSnapshotFromRoute(cfg, "chat", "default")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ModelSnapshotFromRoute(cfg, "chat", "reasoning")
	if err != nil {
		t.Fatal(err)
	}
	if a.ModelID != b.ModelID || a.Identity() == b.Identity() {
		t.Fatalf("provider-qualified identities collapsed: a=%q b=%q", a.Identity(), b.Identity())
	}
	input := p11SnapshotInput()
	input.Models = []ModelSnapshot{a, b}
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(frozen.CanonicalJSON())
	for _, forbidden := range []string{"PROVIDER_A_SECRET", "/run/secrets/provider_b", "api_key", "secret_ref"} {
		if strings.Contains(strings.ToLower(payload), strings.ToLower(forbidden)) {
			t.Fatalf("snapshot contains secret material %q", forbidden)
		}
	}
}

func TestTypedContextRebuildsAttemptFromDatabaseTruth(t *testing.T) {
	frozen, err := FreezeRuntimeSnapshot(p11SnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	fields := frozen.WorkflowFields()
	deadline := time.Now().Add(20 * time.Minute).UTC().Truncate(time.Millisecond)
	contextPayload, err := json.Marshal(workflow.DurableContextSnapshot{
		Schema: workflow.DurableContextSnapshotSchema,
		Identity: workflow.DurableIdentitySnapshot{
			UserID: "user-1", Role: string(policy.RoleOperator), Scope: policy.Scope{UserID: "user-1"},
		},
		History:      json.RawMessage(`{"summary":"durable mysql"}`),
		BudgetLimits: json.RawMessage(`{"max_tokens":4096}`),
		DeadlineAt:   deadline,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := mysql.WorkflowRun{
		ID: "run-1", UserID: "user-1", SessionID: "session-1", RuntimeMode: workflow.RuntimeModeDurableV1,
		Status: workflow.RunStatusRunning, Attempt: 1, LeaseGeneration: 7, LeaseOwner: p11String("worker-1"),
		ContextSnapshotJSON: p11String(string(contextPayload)), RuntimeVersion: &fields.RuntimeVersion,
		RuntimeCompatibilityHash: &fields.RuntimeCompatibilityHash, AgentRevision: &fields.AgentRevision,
		ModelSnapshot: p11String(string(fields.ModelSnapshotJSON)), ToolSnapshot: p11String(string(fields.ToolSnapshotJSON)),
		MCPCatalogHash: &fields.MCPCatalogHash, SkillSnapshot: p11String(string(fields.SkillSnapshotJSON)),
		PromptHash: &fields.PromptHash, PolicyHash: &fields.PolicyHash, ConfigHash: &fields.ConfigHash,
		FeatureSnapshot: p11String(string(fields.FeatureSnapshotJSON)), BudgetLimitsJSON: p11String(`{"max_tokens":4096}`),
		BudgetUsageJSON: p11String(`{}`), BudgetReservationsJSON: p11String(`{}`),
	}
	claimed := workflow.ClaimedRun{Run: run, Token: workflow.LeaseToken{RunID: run.ID, Owner: "worker-1", Generation: 7}}
	budgetFactory := &p11BudgetFactory{}
	ctx, attempt, err := BuildAttemptContext(context.Background(), claimed, budgetFactory)
	if err != nil {
		t.Fatalf("build attempt context: %v", err)
	}
	t.Cleanup(attempt.Cancel)
	if attempt.Run.ID != run.ID || attempt.Run.Attempt != 1 || attempt.Lease != claimed.Token {
		t.Fatalf("attempt run/lease mismatch: %+v", attempt)
	}
	if attempt.Identity.UserID != "user-1" || attempt.Identity.Role != policy.RoleOperator || attempt.Scope.UserID != "user-1" {
		t.Fatalf("rebuilt identity/scope = %+v/%+v", attempt.Identity, attempt.Scope)
	}
	if attempt.Budget == nil || attempt.Trace.ID == "" || !attempt.Deadline.Equal(deadline) {
		t.Fatalf("missing budget/trace/deadline: budget=%v trace=%+v deadline=%s", attempt.Budget, attempt.Trace, attempt.Deadline)
	}
	if budgetFactory.state.RunID != run.ID || !json.Valid(budgetFactory.state.Limits) {
		t.Fatalf("budget was not rebuilt from Run state: %+v", budgetFactory.state)
	}
	fromContext, err := AttemptContextFromContext(ctx)
	if err != nil || fromContext != attempt {
		t.Fatalf("typed context lookup = %p, %v", fromContext, err)
	}
	if token, err := workflow.LeaseTokenFromContext(ctx); err != nil || token != claimed.Token {
		t.Fatalf("P09 lease accessor = %+v, %v", token, err)
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil || identity.UserID != "user-1" {
		t.Fatalf("policy identity lookup = %+v, %v", identity, err)
	}
}

func TestAttemptPreservesRunSnapshotAndRotatesAttemptTrace(t *testing.T) {
	baseRun, frozen := p11AttemptRun(t)
	_, first, err := BuildAttemptContext(context.Background(), workflow.ClaimedRun{
		Run: baseRun, Token: workflow.LeaseToken{RunID: baseRun.ID, Owner: "worker-a", Generation: 1},
	}, &p11BudgetFactory{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Cancel)
	baseRun.Attempt = 2
	baseRun.LeaseGeneration = 2
	baseRun.LeaseOwner = p11String("worker-b")
	_, second, err := BuildAttemptContext(context.Background(), workflow.ClaimedRun{
		Run: baseRun, Token: workflow.LeaseToken{RunID: baseRun.ID, Owner: "worker-b", Generation: 2},
	}, &p11BudgetFactory{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Cancel)
	if first.Run.ID != second.Run.ID || first.Snapshot.CompatibilityHash() != frozen.CompatibilityHash() || second.Snapshot.CompatibilityHash() != frozen.CompatibilityHash() {
		t.Fatal("new attempt changed run or frozen snapshot")
	}
	if first.Run.Attempt != 1 || second.Run.Attempt != 2 || first.Trace.ID == second.Trace.ID {
		t.Fatalf("attempt/trace did not rotate: first=%+v second=%+v", first.Run, second.Run)
	}
}

func TestSessionValuesAllowlistRejectsHandlesSecretsAndMutableValues(t *testing.T) {
	valid := map[string]any{
		SessionRunIDKey:             "run-1",
		SessionRuntimeVersionKey:    "runtime-v1",
		SessionCompatibilityHashKey: p11Hash("compat"),
	}
	if _, err := SafeSessionValues(valid); err != nil {
		t.Fatalf("safe values rejected: %v", err)
	}
	invalid := []map[string]any{
		{"db": &struct{}{}},
		{"function": func() {}},
		{"channel": make(chan struct{})},
		{"api_key": "resolved-secret"},
		{SessionRunIDKey: []string{"mutable"}},
		{SessionRunIDKey: map[string]any{"mutable": true}},
	}
	for index, values := range invalid {
		if _, err := SafeSessionValues(values); err == nil {
			t.Errorf("invalid SessionValues case %d accepted", index)
		}
	}
}

func TestSessionValuesDoNotImplicitlyFormatLiteralInstruction(t *testing.T) {
	input := &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("hello")}}
	instruction := `Return literal JSON: {"secret":"{api_key}"}`
	messages, err := LiteralGenModelInput(context.Background(), instruction, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Content != instruction {
		t.Fatalf("literal instruction changed: %#v", messages)
	}
}

type p11BudgetHandle struct{ id string }

func (p11BudgetHandle) RuntimeBudgetHandle() {}

type p11BudgetFactory struct{ state BudgetState }

func (f *p11BudgetFactory) RebuildBudgetHandle(_ context.Context, state BudgetState) (BudgetHandle, error) {
	f.state = state
	return p11BudgetHandle{id: "budget/" + state.RunID}, nil
}

func p11SnapshotInput() RuntimeSnapshotInput {
	thinking := false
	return RuntimeSnapshotInput{
		Runtime:       RuntimeVersionSnapshot{Go: "go1.27.0", Eino: "v0.9.15", App: "app-rev-1"},
		AgentRevision: "agent-rev-1",
		PromptHash:    p11Hash("prompt-1"),
		PolicyHash:    p11Hash("policy-1"),
		ConfigHash:    p11Hash("config-1"),
		Models: []ModelSnapshot{
			{Kind: "chat", Profile: "default", CatalogRef: "provider_a/chat", Provider: "provider_a", Driver: appconfig.DriverOpenAICompatibleChat, ModelID: "vendor-chat", RouteOptions: RouteOptionsSnapshot{EnableThinking: &thinking}, Pricing: PricingSnapshot{Revision: "pricing-1", Currency: "CNY", Unit: "per_million_tokens", Input: 1, Output: 2}},
			{Kind: "embedding", Profile: "default", CatalogRef: "provider_a/embed", Provider: "provider_a", Driver: appconfig.DriverOpenAICompatibleEmbedding, ModelID: "vendor-embed", Pricing: PricingSnapshot{Revision: "pricing-1", Currency: "CNY", Unit: "per_million_tokens", Input: 0.5}},
		},
		Tools: []ToolSnapshot{
			{Name: "query_events", Revision: "tool-rev-1", SchemaHash: p11Hash("query-events-schema")},
			{Name: "search_similar_events", Revision: "tool-rev-1", SchemaHash: p11Hash("search-schema")},
		},
		MCPCatalogHash: p11Hash("mcp-1"),
		Skills:         []SkillSnapshot{{Name: "incident-analysis", ContentHash: p11Hash("skill-1")}},
		FeatureGates: map[string]bool{
			"agent_runtime.enabled":                    true,
			"agent_runtime.accept_new_runs":            true,
			"agent_runtime.shadow_mode":                false,
			"agent_runtime.l1_writes":                  false,
			"agent_runtime.l2_writes":                  false,
			"agent_runtime.admin_query_database_debug": false,
			"mcp.enabled":                              false,
			"skill.enabled":                            false,
			"langfuse.enabled":                         false,
		},
	}
}

func p11AttemptRun(t *testing.T) (mysql.WorkflowRun, FrozenRuntimeSnapshot) {
	t.Helper()
	frozen, err := FreezeRuntimeSnapshot(p11SnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	fields := frozen.WorkflowFields()
	deadline := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	contextPayload, _ := json.Marshal(workflow.DurableContextSnapshot{
		Schema:   workflow.DurableContextSnapshotSchema,
		Identity: workflow.DurableIdentitySnapshot{UserID: "user-1", Role: string(policy.RoleViewer), Scope: policy.Scope{UserID: "user-1"}},
		History:  json.RawMessage(`{}`), BudgetLimits: json.RawMessage(`{}`), DeadlineAt: deadline,
	})
	return mysql.WorkflowRun{
		ID: "run-stable", UserID: "user-1", SessionID: "session-1", RuntimeMode: workflow.RuntimeModeDurableV1,
		Status: workflow.RunStatusRunning, Attempt: 1, LeaseGeneration: 1, LeaseOwner: p11String("worker-a"),
		ContextSnapshotJSON: p11String(string(contextPayload)), RuntimeVersion: &fields.RuntimeVersion,
		RuntimeCompatibilityHash: &fields.RuntimeCompatibilityHash, AgentRevision: &fields.AgentRevision,
		ModelSnapshot: p11String(string(fields.ModelSnapshotJSON)), ToolSnapshot: p11String(string(fields.ToolSnapshotJSON)),
		MCPCatalogHash: &fields.MCPCatalogHash, SkillSnapshot: p11String(string(fields.SkillSnapshotJSON)), PromptHash: &fields.PromptHash,
		PolicyHash: &fields.PolicyHash, ConfigHash: &fields.ConfigHash, FeatureSnapshot: p11String(string(fields.FeatureSnapshotJSON)),
		BudgetLimitsJSON: p11String(`{}`), BudgetUsageJSON: p11String(`{}`), BudgetReservationsJSON: p11String(`{}`),
	}, frozen
}

func p11Hash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func p11String(value string) *string { return &value }
