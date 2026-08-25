package solve_pipeline

import (
	"context"
	"testing"

	"SentinelOps/internal/ai/runtime"
	agenttest "SentinelOps/internal/testutil/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestSolveADKContract(t *testing.T) {
	model := agenttest.NewScriptedChatModel(agenttest.ModelStep{Message: schema.AssistantMessage("done", nil)})
	agent, err := NewSolveAgent(context.Background(), model, runtime.NewRuntimeHandler())
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name(context.Background()) != "SolveAgent" {
		t.Fatalf("name = %q", agent.Name(context.Background()))
	}
	var _ adk.Agent = agent
	for _, name := range solveL0Tools {
		if name == "save_intelligence" || name == "create_report" || name == "query_database" {
			t.Fatalf("non-L0 tool in solve inventory: %q", name)
		}
	}
}
