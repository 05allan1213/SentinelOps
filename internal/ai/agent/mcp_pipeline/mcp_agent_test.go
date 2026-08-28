package mcp_pipeline

import (
	"context"
	"strings"
	"testing"

	airuntime "SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type fixture33Tool struct {
	info *schema.ToolInfo
}

func (t *fixture33Tool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }
func (t *fixture33Tool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "ok", nil
}

type fixture33Model struct{}

func (*fixture33Model) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}
func (*fixture33Model) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}
func (*fixture33Model) BindTools([]*schema.ToolInfo) error { return nil }

type fixture33Source struct{ tools []tool.BaseTool }

func (s fixture33Source) Tools(context.Context) ([]tool.BaseTool, error) { return s.tools, nil }

func fixture33Info(name string, annotations map[string]any) *schema.ToolInfo {
	return &schema.ToolInfo{Name: name, Desc: name, Extra: map[string]any{"mcp.annotations": annotations}}
}

func TestMCPAgentRejectsMCPWriteToolsEvenWhenRemoteMarksSafe(t *testing.T) {
	_, err := BuildMCPAgent(context.Background(), Config{
		Model:          &fixture33Model{},
		RuntimeHandler: airuntime.NewRuntimeHandler(),
		Source: fixture33Source{tools: []tool.BaseTool{
			&fixture33Tool{info: fixture33Info("inventory__delete", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("MCP write tool was accepted: %v", err)
	}
}

func TestMCPAgentRequiresProviderContractForModelToolSearch(t *testing.T) {
	_, err := BuildMCPAgent(context.Background(), Config{
		Model:              &fixture33Model{},
		RuntimeHandler:     airuntime.NewRuntimeHandler(),
		UseModelToolSearch: true,
		Source: fixture33Source{tools: []tool.BaseTool{
			&fixture33Tool{info: fixture33Info("inventory__search", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "provider tool-search contract") {
		t.Fatalf("missing provider contract did not fail closed: %v", err)
	}
}

func TestMCPAgentToolSearchDefaultsToClientMode(t *testing.T) {
	cfg, err := newAgentConfig(context.Background(), Config{
		Model:          &fixture33Model{},
		RuntimeHandler: airuntime.NewRuntimeHandler(),
		Source: fixture33Source{tools: []tool.BaseTool{
			&fixture33Tool{info: fixture33Info("inventory__search", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Handlers) < 2 {
		t.Fatalf("MCP Agent handlers = %d, want RuntimeHandler + official Tool Search", len(cfg.Handlers))
	}
	if cfg.Name != "mcp_agent" {
		t.Fatalf("MCP Agent name = %q", cfg.Name)
	}
}

func TestMCPDirectModeStillUsesRuntimeHandler(t *testing.T) {
	handler := airuntime.NewRuntimeHandler()
	cfg, err := newAgentConfig(context.Background(), Config{
		Model:             &fixture33Model{},
		RuntimeHandler:    handler,
		DisableToolSearch: true,
		Source: fixture33Source{tools: []tool.BaseTool{
			&fixture33Tool{info: fixture33Info("inventory__search", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Handlers) < 2 {
		t.Fatalf("direct MCP Agent handlers = %d, want RuntimeHandler + dynamic catalog", len(cfg.Handlers))
	}
	if cfg.Handlers[0] != handler {
		t.Fatalf("direct MCP Agent outer handler = %T, want shared RuntimeHandler", cfg.Handlers[0])
	}
}

func TestMCPAgentAcceptsModelToolSearchOnlyWithValidatedContract(t *testing.T) {
	_, err := BuildMCPAgent(context.Background(), Config{
		Model:              &fixture33Model{},
		RuntimeHandler:     airuntime.NewRuntimeHandler(),
		UseModelToolSearch: true,
		ProviderContract: &ProviderToolSearchContract{
			CatalogRef: "provider/chat", ProviderRevision: "rev-1", ContractHash: strings.Repeat("c", 64), Validated: true,
		},
		Source: fixture33Source{tools: []tool.BaseTool{
			&fixture33Tool{info: fixture33Info("inventory__search", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err != nil {
		t.Fatalf("validated provider tool-search contract rejected: %v", err)
	}
}
