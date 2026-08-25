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

type p33Tool struct {
	info *schema.ToolInfo
}

func (t *p33Tool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }
func (t *p33Tool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "ok", nil
}

type p33Model struct{}

func (*p33Model) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}
func (*p33Model) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}
func (*p33Model) BindTools([]*schema.ToolInfo) error { return nil }

type p33Source struct{ tools []tool.BaseTool }

func (s p33Source) Tools(context.Context) ([]tool.BaseTool, error) { return s.tools, nil }

func p33Info(name string, annotations map[string]any) *schema.ToolInfo {
	return &schema.ToolInfo{Name: name, Desc: name, Extra: map[string]any{"mcp.annotations": annotations}}
}

func TestMCPAgentRejectsMCPWriteToolsEvenWhenRemoteMarksSafe(t *testing.T) {
	_, err := BuildMCPAgent(context.Background(), Config{
		Model:          &p33Model{},
		RuntimeHandler: airuntime.NewRuntimeHandler(),
		Source: p33Source{tools: []tool.BaseTool{
			&p33Tool{info: p33Info("inventory__delete", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("MCP write tool was accepted: %v", err)
	}
}

func TestMCPAgentRequiresProviderContractForModelToolSearch(t *testing.T) {
	_, err := BuildMCPAgent(context.Background(), Config{
		Model:              &p33Model{},
		RuntimeHandler:     airuntime.NewRuntimeHandler(),
		UseModelToolSearch: true,
		Source: p33Source{tools: []tool.BaseTool{
			&p33Tool{info: p33Info("inventory__search", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "provider tool-search contract") {
		t.Fatalf("missing provider contract did not fail closed: %v", err)
	}
}

func TestMCPAgentToolSearchDefaultsToClientMode(t *testing.T) {
	cfg, err := newAgentConfig(context.Background(), Config{
		Model:          &p33Model{},
		RuntimeHandler: airuntime.NewRuntimeHandler(),
		Source: p33Source{tools: []tool.BaseTool{
			&p33Tool{info: p33Info("inventory__search", map[string]any{"readOnlyHint": true})},
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

func TestMCPAgentAcceptsModelToolSearchOnlyWithValidatedContract(t *testing.T) {
	_, err := BuildMCPAgent(context.Background(), Config{
		Model:              &p33Model{},
		RuntimeHandler:     airuntime.NewRuntimeHandler(),
		UseModelToolSearch: true,
		ProviderContract: &ProviderToolSearchContract{
			CatalogRef: "provider/chat", ProviderRevision: "rev-1", ContractHash: strings.Repeat("c", 64), Validated: true,
		},
		Source: p33Source{tools: []tool.BaseTool{
			&p33Tool{info: p33Info("inventory__search", map[string]any{"readOnlyHint": true})},
		}},
	})
	if err != nil {
		t.Fatalf("validated provider tool-search contract rejected: %v", err)
	}
}
