package ops_pipeline

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/runtime"
	agenttest "SentinelOps/internal/testutil/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type fixture26DurableContext struct {
	context.Context
	attempt *runtime.AttemptContext
}

func (c fixture26DurableContext) Value(any) any { return c.attempt }

type fixture26OpenLegacyGate struct{ called bool }

func (g *fixture26OpenLegacyGate) AllowLegacyOpsWrites(context.Context) bool {
	g.called = true
	return true
}

func TestAgentOpsContract(t *testing.T) {
	model := agenttest.NewScriptedChatModel(agenttest.ModelStep{Message: schema.AssistantMessage("done", nil)})
	agent, err := NewOpsAgent(context.Background(), model, runtime.NewRuntimeHandler())
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name(context.Background()) != "OpsAgent" {
		t.Fatalf("name = %q", agent.Name(context.Background()))
	}
	if agent.Description(context.Background()) == "" {
		t.Fatal("description is empty")
	}
	var _ adk.Agent = agent
	assertOpsInventory(t)
}

func TestNoDirectWriteDurableContextRejectsOpenLegacyGate(t *testing.T) {
	ctx := fixture26DurableContext{
		Context: context.Background(),
		attempt: &runtime.AttemptContext{Run: runtime.RunIdentity{ID: "run-phase26-durable"}},
	}
	gate := &fixture26OpenLegacyGate{}
	if err := RequireLegacyOpsWrites(ctx, gate); !errors.Is(err, ErrLegacyOpsWritesDisabled) {
		t.Fatalf("durable context with open Gate error=%v, want ErrLegacyOpsWritesDisabled", err)
	}
	if gate.called {
		t.Fatal("durable context reached the legacy Gate evaluator")
	}
}

func assertOpsInventory(t *testing.T) {
	t.Helper()
	want := []string{"query_events", "trigger_ops", "update_event_status", "block_ip", "notify_dingtalk", "notify_wecom", "notify_email", "webhook_out", "get_current_time"}
	if len(opsDurableTools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(opsDurableTools), len(want))
	}
	for index, name := range want {
		if opsDurableTools[index] != name {
			t.Fatalf("tool[%d] = %q, want %q", index, opsDurableTools[index], name)
		}
	}
}
