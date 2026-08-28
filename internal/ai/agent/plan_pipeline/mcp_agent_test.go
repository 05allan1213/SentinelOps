package plan_pipeline

import (
	"context"
	"reflect"
	"testing"

	"SentinelOps/internal/ai/agent/mcp_pipeline"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/components/tool"
)

func TestExecutorOuterInventoryKeepsMCPToolsBehindOneAgentTool(t *testing.T) {
	ctx := context.Background()
	configured := mcp_pipeline.NewAgentTool(ctx, airuntime.NewRuntimeHandler())
	cfg, err := newExecutorAgentConfig(ctx, &ExecutorBuilderConfig{
		Model:               &fixture15Model{},
		RegisteredToolNames: []string{"get_current_time"},
		AgentTools:          []tool.BaseTool{configured},
		RuntimeHandler:      airuntime.NewRuntimeHandler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(cfg.ToolsConfig.Tools))
	for _, current := range cfg.ToolsConfig.Tools {
		info, infoErr := current.Info(ctx)
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		got = append(got, info.Name)
	}
	if !reflect.DeepEqual(got, []string{"get_current_time", "mcp_agent"}) {
		t.Fatalf("outer Executor inventory = %#v, want one mcp_agent boundary", got)
	}
	entry, err := policy.LookupCatalog("mcp_agent")
	if err != nil || entry.Risk != policy.RiskL0 || entry.EffectType != policy.EffectNone {
		t.Fatalf("mcp_agent Catalog entry = %+v, err=%v", entry, err)
	}
	if err := policy.RequireExecutable("mcp_agent"); err != nil {
		t.Fatalf("mcp_agent is not executable through L0 Catalog: %v", err)
	}
}
