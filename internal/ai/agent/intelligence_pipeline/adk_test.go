package intelligence_pipeline

import (
	"context"
	"testing"

	"SentinelOps/internal/ai/runtime"
	agenttest "SentinelOps/internal/testutil/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestAgentIntelligenceContract(t *testing.T) {
	model := agenttest.NewScriptedChatModel(agenttest.ModelStep{Message: schema.AssistantMessage("done", nil)})
	agent, err := NewIntelligenceAgent(context.Background(), model, runtime.NewRuntimeHandler())
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name(context.Background()) != "IntelligenceAgent" {
		t.Fatalf("name = %q", agent.Name(context.Background()))
	}
	if agent.Description(context.Background()) == "" {
		t.Fatal("description is empty")
	}
	var _ adk.Agent = agent
	assertIntelligenceInventory(t)
}

func assertIntelligenceInventory(t *testing.T) {
	t.Helper()
	want := []string{"query_internal_docs", "get_current_time", "web_search", "save_intelligence"}
	if len(intelligenceTools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(intelligenceTools), len(want))
	}
	for index, name := range want {
		if intelligenceTools[index] != name {
			t.Fatalf("tool[%d] = %q, want %q", index, intelligenceTools[index], name)
		}
	}
}
