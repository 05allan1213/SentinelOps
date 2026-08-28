package runtime

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

func TestMutationRouteSnapshotCoversEveryDurableMutation(t *testing.T) {
	config := &appconfig.Config{
		AgentRuntime: appconfig.AgentRuntime{Enabled: true, AcceptNewRuns: true},
		Providers: map[string]appconfig.Provider{
			"test": {Endpoints: map[string]string{appconfig.DriverOpenAICompatibleChat: "https://example.invalid/v1"}},
		},
		ModelCatalog: map[string]appconfig.Model{
			"test/chat": {
				ModelID: "test-chat", Driver: appconfig.DriverOpenAICompatibleChat,
				Pricing: appconfig.Pricing{Revision: "phase26", Currency: "CNY", Unit: "per_million_tokens", Input: 1, Output: 2},
			},
		},
		Routing: appconfig.Routing{Chat: map[string]appconfig.ChatRoute{"default": {Candidates: []appconfig.Route{{Model: "test/chat"}}}}},
	}
	frozen, err := BuildDurableRuntimeSnapshot(config)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(frozen.Tools()))
	for _, item := range frozen.Tools() {
		got[item.Name] = true
	}
	for _, name := range []string{
		"create_report", "save_intelligence", "update_event_status", "block_ip",
		"notify_dingtalk", "notify_wecom", "notify_email", "webhook_out",
		"event_analysis_agent", "report_agent", "risk_assessment_agent",
		"solve_agent", "intelligence_agent", "ops_agent",
	} {
		if !got[name] {
			t.Errorf("durable Runtime Snapshot missing %q", name)
		}
		entry, lookupErr := policy.LookupCatalog(name)
		if lookupErr != nil {
			t.Fatalf("lookup %q: %v", name, lookupErr)
		}
		if entry.Name != name {
			t.Fatalf("catalog name=%q want=%q", entry.Name, name)
		}
	}
	if frozen.FeatureGate("agent_runtime.l1_writes") || frozen.FeatureGate("agent_runtime.l2_writes") {
		t.Fatalf("phase26 production Mutation gates must remain false: %+v", frozen.document.FeatureGates)
	}
}

func TestNoDirectWriteDurableContextIsIdentified(t *testing.T) {
	ctx := contextWithAttemptForP26(t)
	if !IsDurableV1Context(ctx) {
		t.Fatal("typed durable_v1 Attempt context was not identified")
	}
	if IsDurableV1Context(context.TODO()) {
		t.Fatal("context without Attempt was identified as durable_v1")
	}
}

func TestMutationRouteEveryLeafInterruptsBeforeEndpoint(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase26_every_leaf_interrupt")
	cases := []struct {
		name string
		risk policy.RiskLevel
		args string
	}{
		{name: "create_report", risk: policy.RiskL1, args: `{"title":"phase26","content":"phase26"}`},
		{name: "save_intelligence", risk: policy.RiskL1, args: `{"title":"phase26","content":"phase26"}`},
		{name: "update_event_status", risk: policy.RiskL1, args: `{"event_id":"phase26","status":"resolved"}`},
		{name: "block_ip", risk: policy.RiskL2, args: `{"ip":"192.0.2.26"}`},
		{name: "notify_dingtalk", risk: policy.RiskL2, args: `{"title":"phase26","content":"phase26"}`},
		{name: "notify_wecom", risk: policy.RiskL2, args: `{"content":"phase26"}`},
		{name: "notify_email", risk: policy.RiskL2, args: `{"subject":"phase26"}`},
		{name: "webhook_out", risk: policy.RiskL2, args: `{"url":"https://example.invalid","payload":"{}"}`},
	}
	endpointCalls := 0
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store, ctx := fixture22RuntimeContext(t, db, "phase26-"+test.name, test.name, test.risk)
			handler, err := NewHITLRuntimeHandler(store)
			if err != nil {
				t.Fatal(err)
			}
			wrapped, err := handler.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
				endpointCalls++
				return "unexpected", nil
			}, &adk.ToolContext{Name: test.name, CallID: "phase26-" + test.name})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := wrapped(ctx, test.args); err == nil {
				t.Fatal("Mutation did not interrupt")
			} else {
				signal := new(adk.InterruptSignal)
				if !errors.As(err, &signal) {
					t.Fatalf("Mutation error=%T %v, want official InterruptSignal", err, err)
				}
			}
			var approvalCount, effectCount int64
			if err := db.Model(&mysql.AgentApproval{}).Where("run_id = ?", "run-phase22-phase26-"+test.name).Count(&approvalCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ?", "run-phase22-phase26-"+test.name).Count(&effectCount).Error; err != nil {
				t.Fatal(err)
			}
			if approvalCount != 1 || effectCount != 0 {
				t.Fatalf("before Approval: approvals=%d effects=%d", approvalCount, effectCount)
			}
		})
	}
	if endpointCalls != 0 {
		t.Fatalf("Mutation endpoints called before Approval: %d", endpointCalls)
	}

	var preparingCount int64
	if err := db.Model(&mysql.AgentApproval{}).Where("status = ?", workflow.ApprovalStatusPreparing).Count(&preparingCount).Error; err != nil {
		t.Fatal(err)
	}
	if preparingCount != int64(len(cases)) {
		t.Fatalf("preparing Approval rows=%d, want %d", preparingCount, len(cases))
	}
}

func contextWithAttemptForP26(t *testing.T) context.Context {
	t.Helper()
	return context.WithValue(context.Background(), attemptContextKey{}, &AttemptContext{Run: RunIdentity{ID: "run-phase26"}})
}
