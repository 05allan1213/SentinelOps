package ops_pipeline

import (
	"context"
	"testing"

	"SentinelOps/internal/ai/runtime"
	agenttest "SentinelOps/internal/testutil/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

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

func assertOpsInventory(t *testing.T) {
	t.Helper()
	want := []string{"query_events", "trigger_ops", "update_event_status", "block_ip", "notify_dingtalk", "notify_wecom", "notify_email", "get_current_time"}
	if len(opsTools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(opsTools), len(want))
	}
	for index, name := range want {
		if opsTools[index] != name {
			t.Fatalf("tool[%d] = %q, want %q", index, opsTools[index], name)
		}
	}
}
