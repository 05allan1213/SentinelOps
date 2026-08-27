package mcp_pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	mcptools "SentinelOps/internal/ai/tools/mcp"
	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	// AgentName 是外层 Executor 唯一可见的 MCP 编排 Tool 名称。
	AgentName           = "mcp_agent"
	agentDescription    = "Call the MCP Agent to search and invoke approved read-only tools from configured MCP servers."
	defaultProfile      = "default"
	dynamicToolRevision = "mcp_tool_v1"
	// configuredMaxIterations 是已批准 MCP Agent 的固定迭代上界；只读文档检索
	// 不应进入无界工具循环，避免真实模型调用预算失控。
	configuredMaxIterations = 3
)

// ToolSource 在每次 mcp_agent 构建时读取当前 MCP Session 的官方 Tool。
type ToolSource interface {
	Tools(context.Context) ([]tool.BaseTool, error)
}

// StaticToolSource 是 contract test 和本地组装使用的最薄 ToolSource。
type StaticToolSource []tool.BaseTool

func (s StaticToolSource) Tools(context.Context) ([]tool.BaseTool, error) {
	return append([]tool.BaseTool(nil), s...), nil
}

// ProviderToolSearchContract 是供应商原生 Tool Search 能力的已验证非敏感 contract。
// ContractHash 必须由真实 provider contract test 产生，不能用配置开关替代。
type ProviderToolSearchContract struct {
	CatalogRef       string
	ProviderRevision string
	ContractHash     string
	Validated        bool
}

// Config 描述独立 mcp_agent 的模型、动态 Tool 和官方 middleware 组合。
type Config struct {
	Model              model.BaseChatModel
	Reliability        *models.Reliability
	Profile            string
	RuntimeHandler     *airuntime.RuntimeHandler
	Source             ToolSource
	DynamicTools       []tool.BaseTool
	CatalogHash        string
	UseModelToolSearch bool
	ProviderContract   *ProviderToolSearchContract
	MaxIterations      int
	// DisableToolSearch 对已批准的小型只读 MCP 目录直接暴露官方 Tool，
	// 跳过 tool_search 元工具轮次；默认保持官方 Tool Search 语义。
	DisableToolSearch bool
}

// BuildMCPAgent 构建独立的 Eino ChatModelAgent；远端 Tool 只在该 Agent 内动态可见。
func BuildMCPAgent(ctx context.Context, cfg Config) (adk.Agent, error) {
	agentConfig, err := newAgentConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return adk.NewChatModelAgent(ctx, agentConfig)
}

// NewMCPAgent 是 BuildMCPAgent 的兼容入口。
func NewMCPAgent(ctx context.Context, cfg Config) (adk.Agent, error) {
	return BuildMCPAgent(ctx, cfg)
}

func newAgentConfig(ctx context.Context, cfg Config) (*adk.ChatModelAgentConfig, error) {
	if ctx == nil {
		return nil, errors.New("mcp agent context is required")
	}
	if cfg.RuntimeHandler == nil {
		return nil, errors.New("mcp agent RuntimeHandler is required")
	}
	if cfg.Model == nil && cfg.Reliability == nil {
		profile := strings.TrimSpace(cfg.Profile)
		if profile == "" {
			profile = defaultProfile
		}
		reliability, reliabilityErr := models.BuildReliability(ctx, profile, airuntime.PhysicalModelBinder(cfg.RuntimeHandler))
		if reliabilityErr != nil {
			return nil, fmt.Errorf("build MCP Agent model profile %q: %w", profile, reliabilityErr)
		}
		cfg.Reliability = reliability
	}
	if (cfg.Model == nil) == (cfg.Reliability == nil) {
		return nil, errors.New("mcp agent requires exactly one Model or Reliability configuration")
	}
	if cfg.UseModelToolSearch {
		if cfg.ProviderContract == nil || !cfg.ProviderContract.Validated ||
			strings.TrimSpace(cfg.ProviderContract.CatalogRef) == "" ||
			strings.TrimSpace(cfg.ProviderContract.ProviderRevision) == "" ||
			!validSHA256(cfg.ProviderContract.ContractHash) {
			return nil, errors.New("provider tool-search contract is required for model tool search")
		}
	}

	dynamicTools, catalog, err := resolveDynamicTools(ctx, cfg)
	if err != nil {
		return nil, err
	}
	handlers := []adk.ChatModelAgentMiddleware{&dynamicCatalogMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, catalog: catalog}}
	if cfg.DisableToolSearch {
		if cfg.UseModelToolSearch {
			return nil, errors.New("mcp agent cannot combine direct tools with model tool search")
		}
		agentConfig := &adk.ChatModelAgentConfig{
			Name: AgentName, Description: agentDescription,
			ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: dynamicTools}},
			MaxIterations: cfg.MaxIterations,
			Handlers:      handlers,
		}
		if cfg.Model != nil {
			agentConfig.Model = cfg.Model
		} else if err := models.ConfigureChatModelAgent(agentConfig, cfg.Reliability); err != nil {
			return nil, fmt.Errorf("configure MCP Agent reliability: %w", err)
		}
		return agentConfig, nil
	}
	searchMiddleware, err := toolsearch.New(ctx, &toolsearch.Config{
		DynamicTools: dynamicTools, UseModelToolSearch: cfg.UseModelToolSearch,
	})
	if err != nil {
		return nil, fmt.Errorf("configure official MCP Tool Search: %w", err)
	}
	handlers, err = airuntime.RuntimeHandlerFirst(cfg.RuntimeHandler, handlers[0], searchMiddleware)
	if err != nil {
		return nil, err
	}

	agentConfig := &adk.ChatModelAgentConfig{
		Name: AgentName, Description: agentDescription,
		ToolsConfig:   adk.ToolsConfig{},
		MaxIterations: cfg.MaxIterations,
		Handlers:      handlers,
	}
	if cfg.Model != nil {
		agentConfig.Model = cfg.Model
	} else if err := models.ConfigureChatModelAgent(agentConfig, cfg.Reliability); err != nil {
		return nil, fmt.Errorf("configure MCP Agent reliability: %w", err)
	}
	return agentConfig, nil
}

func resolveDynamicTools(ctx context.Context, cfg Config) ([]tool.BaseTool, airuntime.DynamicToolCatalog, error) {
	var tools []tool.BaseTool
	if cfg.Source != nil {
		loaded, err := cfg.Source.Tools(ctx)
		if err != nil {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("load MCP Tools: %w", err)
		}
		tools = append(tools, loaded...)
	} else {
		tools = append(tools, cfg.DynamicTools...)
	}
	if len(tools) == 0 {
		return nil, airuntime.DynamicToolCatalog{}, errors.New("mcp agent requires at least one approved read-only Tool")
	}

	entries := make([]airuntime.DynamicToolCatalogEntry, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for index, current := range tools {
		if current == nil {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("MCP Tool %d is nil", index)
		}
		info, err := current.Info(ctx)
		if err != nil {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("read MCP Tool %d info: %w", index, err)
		}
		if info == nil || strings.TrimSpace(info.Name) == "" {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("MCP Tool %d has incomplete ToolInfo", index)
		}
		if _, duplicate := seen[info.Name]; duplicate {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("duplicate MCP Tool %q", info.Name)
		}
		seen[info.Name] = struct{}{}
		if !isReadOnlyMCPTool(info) {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("MCP Tool %q is not read-only", info.Name)
		}
		schemaHash, err := policy.ToolSchemaHash(info)
		if err != nil {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("hash MCP Tool %q schema: %w", info.Name, err)
		}
		entries = append(entries, airuntime.DynamicToolCatalogEntry{Name: info.Name, Revision: dynamicToolRevision, SchemaHash: schemaHash, ReadOnly: true})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	hash := strings.TrimSpace(cfg.CatalogHash)
	if hash == "" {
		canonical, err := policy.CanonicalJSON(entries)
		if err != nil {
			return nil, airuntime.DynamicToolCatalog{}, fmt.Errorf("canonicalize MCP Tool catalog: %w", err)
		}
		digest := sha256.Sum256(canonical)
		hash = hex.EncodeToString(digest[:])
	} else if !validSHA256(hash) {
		return nil, airuntime.DynamicToolCatalog{}, errors.New("MCP catalog hash must be a 64-character SHA-256")
	}
	catalog, err := airuntime.NewDynamicToolCatalog(hash, entries)
	if err != nil {
		return nil, airuntime.DynamicToolCatalog{}, err
	}
	return tools, catalog, nil
}

func isReadOnlyMCPTool(info *schema.ToolInfo) bool {
	if info == nil || info.Extra == nil {
		return false
	}
	annotations, ok := info.Extra["mcp.annotations"].(map[string]any)
	if !ok {
		return false
	}
	readOnly, _ := annotations["readOnlyHint"].(bool)
	destructive, _ := annotations["destructiveHint"].(bool)
	return readOnly && !destructive && !looksLikeMutationTool(info.Name)
}

func looksLikeMutationTool(name string) bool {
	lower := strings.ToLower(name)
	for _, verb := range []string{"create", "delete", "destroy", "drop", "insert", "mutat", "patch", "post", "put", "remove", "send", "update", "write", "execute", "trigger"} {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	return false
}

type dynamicCatalogMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	catalog airuntime.DynamicToolCatalog
}

func (m *dynamicCatalogMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	return airuntime.WithDynamicToolCatalog(ctx, m.catalog), runCtx, nil
}

// NewConfiguredMCPAgentTool returns the sole outer Executor Tool. Config and Sessions are
// resolved only when the nested Agent is invoked, so disabled MCP never opens a Session.
func NewConfiguredMCPAgentTool(ctx context.Context, handler *airuntime.RuntimeHandler) tool.BaseTool {
	return adk.NewAgentTool(ctx, &configuredMCPAgent{handler: handler})
}

// NewAgentTool is an alias retained for callers that name the framework Tool directly.
func NewAgentTool(ctx context.Context, handler *airuntime.RuntimeHandler) tool.BaseTool {
	return NewConfiguredMCPAgentTool(ctx, handler)
}

type configuredMCPAgent struct{ handler *airuntime.RuntimeHandler }

func (*configuredMCPAgent) Name(context.Context) string        { return AgentName }
func (*configuredMCPAgent) Description(context.Context) string { return agentDescription }

func (a *configuredMCPAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	agent, err := buildConfiguredMCPAgent(ctx, a.handler)
	if err != nil {
		iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		generator.Send(&adk.AgentEvent{Err: err})
		generator.Close()
		return iter
	}
	return agent.Run(ctx, input, opts...)
}

func buildConfiguredMCPAgent(ctx context.Context, handler *airuntime.RuntimeHandler) (adk.Agent, error) {
	if handler == nil {
		return nil, errors.New("mcp agent RuntimeHandler is required")
	}
	allowed, err := handler.GateAllowed(ctx, airuntime.GateMCPEnabled)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, mcptools.ErrServerDisabled
	}
	app, err := appconfig.Current()
	if err != nil {
		return nil, err
	}
	mcpConfig, err := mcptools.FromAppConfig(app)
	if err != nil {
		return nil, err
	}
	if !mcpConfig.Enabled {
		return nil, mcptools.ErrServerDisabled
	}
	owners := make([]*mcptools.SessionOwner, 0, len(mcpConfig.Servers))
	for _, server := range mcpConfig.Servers {
		if !server.Enabled {
			continue
		}
		owner, ownerErr := mcptools.NewSessionOwner(server)
		if ownerErr != nil {
			return nil, ownerErr
		}
		owners = append(owners, owner)
	}
	reliability, err := models.BuildReliability(ctx, defaultProfile, airuntime.PhysicalModelBinder(handler))
	if err != nil {
		return nil, err
	}
	inner, err := BuildMCPAgent(ctx, Config{
		Reliability: reliability, RuntimeHandler: handler, Profile: defaultProfile,
		Source: ownerToolSource{owners: owners, handler: handler}, CatalogHash: mustMCPConfigHash(mcpConfig),
		MaxIterations: configuredMaxIterations, DisableToolSearch: true,
	})
	if err != nil {
		for _, owner := range owners {
			_ = owner.Close()
		}
		return nil, err
	}
	return &sessionOwnedAgent{inner: inner, owners: owners}, nil
}

func mustMCPConfigHash(config mcptools.Config) string {
	hash, err := mcptools.ConfigCatalogHash(config)
	if err != nil {
		return ""
	}
	return hash
}

type ownerToolSource struct {
	owners  []*mcptools.SessionOwner
	handler *airuntime.RuntimeHandler
}

func (s ownerToolSource) Tools(ctx context.Context) ([]tool.BaseTool, error) {
	if s.handler == nil {
		return nil, errors.New("MCP RuntimeHandler is required")
	}
	var tools []tool.BaseTool
	for _, owner := range s.owners {
		allowed, err := s.handler.GateAllowed(ctx, airuntime.GateMCPEnabled)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, mcptools.ErrServerDisabled
		}
		loaded, err := owner.Tools(ctx)
		if err != nil {
			return nil, err
		}
		tools = append(tools, loaded...)
	}
	return tools, nil
}

type sessionOwnedAgent struct {
	inner  adk.Agent
	owners []*mcptools.SessionOwner
}

func (a *sessionOwnedAgent) Name(ctx context.Context) string        { return a.inner.Name(ctx) }
func (a *sessionOwnedAgent) Description(ctx context.Context) string { return a.inner.Description(ctx) }

func (a *sessionOwnedAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	inner := a.inner.Run(ctx, input, opts...)
	iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		defer func() {
			for _, owner := range a.owners {
				_ = owner.Close()
			}
		}()
		for {
			event, ok := inner.Next()
			if !ok {
				return
			}
			generator.Send(event)
		}
	}()
	return iter
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
