package skill_pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/tools"
	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const (
	// AgentName 是外层 Executor 唯一可见的 Skill 编排 Tool 名称。
	AgentName        = "skill_agent"
	agentDescription = "Call the Skill Agent to load approved read-only SOP instructions."
	defaultProfile   = "default"
	defaultSkillDir  = "manifest/skills"
	// configuredMaxIterations 是已批准 Skill Agent 的固定迭代上界；只读 SOP 流程
	// 不应进入无界工具循环，避免真实模型调用预算失控。
	configuredMaxIterations = 6
)

// DefaultToolNames 是 Skill Agent 可调用的最小 L0 Tool 集。
var DefaultToolNames = []string{"get_current_time", "query_internal_docs"}

// Config 描述官方 Skill middleware、只读 Backend、L0 Tool 和统一模型可靠性组合。
type Config struct {
	Model          model.BaseChatModel
	Reliability    *models.Reliability
	Profile        string
	RuntimeHandler *runtime.RuntimeHandler
	Backend        skill.Backend
	BaseDir        string
	MaxBytes       int64
	ToolNames      []string
	MaxIterations  int
}

// BuildSkillAgent 构建独立的 Eino ChatModelAgent。
func BuildSkillAgent(ctx context.Context, cfg Config) (adk.Agent, error) {
	agentConfig, err := newAgentConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return adk.NewChatModelAgent(ctx, agentConfig)
}

// NewSkillAgent 是 BuildSkillAgent 的兼容入口。
func NewSkillAgent(ctx context.Context, cfg Config) (adk.Agent, error) {
	return BuildSkillAgent(ctx, cfg)
}

func newAgentConfig(ctx context.Context, cfg Config) (*adk.ChatModelAgentConfig, error) {
	if ctx == nil {
		return nil, errors.New("skill agent context is required")
	}
	if cfg.RuntimeHandler == nil {
		return nil, errors.New("skill agent RuntimeHandler is required")
	}
	if cfg.Backend == nil {
		return nil, errors.New("skill agent Backend is required")
	}
	baseDir := strings.TrimSpace(cfg.BaseDir)
	if baseDir == "" {
		baseDir = defaultSkillDir
	}
	if !filepath.IsAbs(baseDir) {
		if absolute, err := filepath.Abs(baseDir); err == nil {
			baseDir = absolute
		} else {
			return nil, fmt.Errorf("resolve skill BaseDir: %w", err)
		}
	}
	if _, err := ValidateBackend(ctx, cfg.Backend, ValidationPolicy{BaseDir: baseDir, MaxBytes: cfg.MaxBytes}); err != nil {
		return nil, err
	}
	toolNames := append([]string(nil), cfg.ToolNames...)
	if len(toolNames) == 0 {
		toolNames = append([]string(nil), DefaultToolNames...)
	}
	if err := ValidateL0ToolNames(toolNames); err != nil {
		return nil, err
	}
	registered, err := tools.GetManyRequired(toolNames)
	if err != nil {
		return nil, fmt.Errorf("resolve skill agent Tools: %w", err)
	}
	handler, err := skill.NewMiddleware(ctx, &skill.Config{Backend: cfg.Backend})
	if err != nil {
		return nil, fmt.Errorf("configure official Skill middleware: %w", err)
	}
	handlers, err := runtime.RuntimeHandlerFirst(cfg.RuntimeHandler, handler)
	if err != nil {
		return nil, err
	}
	agentConfig := &adk.ChatModelAgentConfig{
		Name: AgentName, Description: agentDescription,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: registered}},
		MaxIterations: cfg.MaxIterations,
		Handlers:      handlers,
	}
	if cfg.Model != nil {
		agentConfig.Model = cfg.Model
	} else {
		profile := strings.TrimSpace(cfg.Profile)
		if profile == "" {
			profile = defaultProfile
		}
		if cfg.Reliability == nil {
			cfg.Reliability, err = models.BuildReliability(ctx, profile, runtime.PhysicalModelBinder(cfg.RuntimeHandler))
			if err != nil {
				return nil, fmt.Errorf("build Skill Agent model profile %q: %w", profile, err)
			}
		}
		if err := models.ConfigureChatModelAgent(agentConfig, cfg.Reliability); err != nil {
			return nil, fmt.Errorf("configure Skill Agent reliability: %w", err)
		}
	}
	return agentConfig, nil
}

// NewConfiguredSkillAgentTool returns the sole outer Executor Tool. Configuration and
// Backend are resolved only when the nested Agent is invoked.
func NewConfiguredSkillAgentTool(ctx context.Context, handler *runtime.RuntimeHandler) tool.BaseTool {
	return adk.NewAgentTool(ctx, &configuredSkillAgent{handler: handler})
}

// NewAgentTool is an alias retained for callers that name the framework Tool directly.
func NewAgentTool(ctx context.Context, handler *runtime.RuntimeHandler) tool.BaseTool {
	return NewConfiguredSkillAgentTool(ctx, handler)
}

type configuredSkillAgent struct {
	handler *runtime.RuntimeHandler
}

func (*configuredSkillAgent) Name(context.Context) string        { return AgentName }
func (*configuredSkillAgent) Description(context.Context) string { return agentDescription }

func (a *configuredSkillAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	agent, err := buildConfiguredSkillAgent(ctx, a.handler)
	if err != nil {
		iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		generator.Send(&adk.AgentEvent{Err: err})
		generator.Close()
		return iter
	}
	return agent.Run(ctx, input, opts...)
}

func buildConfiguredSkillAgent(ctx context.Context, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	if handler == nil {
		return nil, errors.New("skill agent RuntimeHandler is required")
	}
	allowed, err := handler.GateAllowed(ctx, runtime.GateSkillEnabled)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errors.New("skill agent Gate is closed")
	}
	config, err := appconfig.Current()
	if err != nil {
		return nil, err
	}
	baseDir := strings.TrimSpace(config.Skill.BaseDir)
	if baseDir == "" {
		baseDir = defaultSkillDir
	}
	if !filepath.IsAbs(baseDir) {
		baseDir, err = filepath.Abs(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve configured Skill BaseDir: %w", err)
		}
	}
	allowed, err = handler.GateAllowed(ctx, runtime.GateSkillEnabled)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errors.New("skill agent Gate is closed")
	}
	backend, err := NewSkillBackendFromFilesystem(ctx, baseDir)
	if err != nil {
		return nil, err
	}
	backend = &gateCheckedSkillBackend{delegate: backend, handler: handler}
	return BuildSkillAgent(ctx, Config{
		RuntimeHandler: handler, Backend: backend, BaseDir: baseDir,
		MaxBytes: config.Skill.MaxBytes, Profile: defaultProfile, MaxIterations: configuredMaxIterations,
		ToolNames: append([]string(nil), DefaultToolNames...),
	})
}

type gateCheckedSkillBackend struct {
	delegate skill.Backend
	handler  *runtime.RuntimeHandler
}

func (b *gateCheckedSkillBackend) List(ctx context.Context) ([]skill.FrontMatter, error) {
	if err := b.require(ctx); err != nil {
		return nil, err
	}
	return b.delegate.List(ctx)
}

func (b *gateCheckedSkillBackend) Get(ctx context.Context, name string) (skill.Skill, error) {
	if err := b.require(ctx); err != nil {
		return skill.Skill{}, err
	}
	return b.delegate.Get(ctx, name)
}

func (b *gateCheckedSkillBackend) require(ctx context.Context) error {
	if b == nil || b.delegate == nil || b.handler == nil {
		return errors.New("skill Gate backend is not initialized")
	}
	allowed, err := b.handler.GateAllowed(ctx, runtime.GateSkillEnabled)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("skill agent Gate is closed")
	}
	return nil
}

// BuildConfiguredSkillSnapshots loads the configured read-only SOP catalog for
// the durable Runtime Snapshot. A disabled Skill gate contributes an empty list.
func BuildConfiguredSkillSnapshots(ctx context.Context, config *appconfig.Config) ([]runtime.SkillSnapshot, error) {
	if ctx == nil {
		return nil, errors.New("skill snapshot context is required")
	}
	if config == nil {
		return nil, errors.New("application configuration is required")
	}
	if !config.Skill.Enabled {
		return []runtime.SkillSnapshot{}, nil
	}
	baseDir := strings.TrimSpace(config.Skill.BaseDir)
	if baseDir == "" {
		baseDir = defaultSkillDir
	}
	if !filepath.IsAbs(baseDir) {
		var err error
		baseDir, err = filepath.Abs(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve configured Skill BaseDir: %w", err)
		}
	}
	backend, err := NewSkillBackendFromFilesystem(ctx, baseDir)
	if err != nil {
		return nil, err
	}
	loaded, err := ValidateBackend(ctx, backend, ValidationPolicy{BaseDir: baseDir, MaxBytes: config.Skill.MaxBytes})
	if err != nil {
		return nil, err
	}
	return loaded.Snapshots, nil
}
