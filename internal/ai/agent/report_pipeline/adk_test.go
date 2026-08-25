package report_pipeline

import (
	"context"
	"testing"

	"SentinelOps/internal/ai/runtime"
	agenttest "SentinelOps/internal/testutil/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestAgentReportContract(t *testing.T) {
	model := agenttest.NewScriptedChatModel(agenttest.ModelStep{Message: schema.AssistantMessage("done", nil)})
	agent, err := NewReportAgent(context.Background(), model, runtime.NewRuntimeHandler())
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name(context.Background()) != "ReportAgent" {
		t.Fatalf("name = %q", agent.Name(context.Background()))
	}
	if agent.Description(context.Background()) == "" {
		t.Fatal("description is empty")
	}
	var _ adk.Agent = agent
	assertReportInventory(t)
}

func assertReportInventory(t *testing.T) {
	t.Helper()
	want := []string{"query_events", "query_reports", "query_report_templates", "search_similar_events", "get_current_time", "create_report", "web_search"}
	if len(reportTools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(reportTools), len(want))
	}
	for index, name := range want {
		if reportTools[index] != name {
			t.Fatalf("tool[%d] = %q, want %q", index, reportTools[index], name)
		}
	}
}
