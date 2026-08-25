package event_analysis_pipeline

import (
	"context"
	"strings"
	"testing"

	"SentinelOps/internal/ai/prompt/agents"
	"SentinelOps/internal/ai/runtime"
	agenttest "SentinelOps/internal/testutil/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestEventAnalysisADKContract(t *testing.T) {
	model := agenttest.NewScriptedChatModel(agenttest.ModelStep{Message: schema.AssistantMessage("done", nil)})
	agent, err := NewEventAnalysisAgent(context.Background(), model, runtime.NewRuntimeHandler())
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name(context.Background()) != "EventAnalysisAgent" {
		t.Fatalf("name = %q", agent.Name(context.Background()))
	}
	if !strings.Contains(agent.Description(context.Background()), "structured analysis") {
		t.Fatalf("description = %q", agent.Description(context.Background()))
	}
	if strings.Contains(agents.EventAnalysis, "save_intelligence") || strings.Contains(agents.EventAnalysis, "保存到本地知识库") {
		t.Fatal("EventAnalysis instruction still contains persistence behavior")
	}
	var _ adk.Agent = agent
	assertL0Inventory(t, eventAnalysisL0Tools)
}

func assertL0Inventory(t *testing.T, names []string) {
	t.Helper()
	for _, name := range names {
		if name == "save_intelligence" || name == "create_report" || name == "query_database" {
			t.Fatalf("mutation or admin tool in L0 inventory: %q", name)
		}
	}
}
