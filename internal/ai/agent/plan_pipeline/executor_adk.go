package plan_pipeline

import (
	"context"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/models"
	airuntime "SentinelOps/internal/ai/runtime"
	aitools "SentinelOps/internal/ai/tools"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const executorMaxIterations = 20

// ExecutorBuilderConfig 只接收 strict Registry Tool 和官方 AgentTool 的组装结果。
type ExecutorBuilderConfig struct {
	Model               model.BaseChatModel
	Reliability         *models.Reliability
	RegisteredToolNames []string
	AgentTools          []tool.BaseTool
	RuntimeHandler      *airuntime.RuntimeHandler
	AdditionalHandlers  []adk.ChatModelAgentMiddleware
}

// NewExecutorBuilder 创建可挂载唯一 RuntimeHandler 的薄 Eino Executor。
// Plan-Execute-Replan 拓扑仍由调用方使用官方 planexecute.New 组装。
func NewExecutorBuilder(ctx context.Context, cfg *ExecutorBuilderConfig) (adk.Agent, error) {
	agentConfig, err := newExecutorAgentConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return adk.NewChatModelAgent(ctx, agentConfig)
}

func newExecutorAgentConfig(ctx context.Context, cfg *ExecutorBuilderConfig) (*adk.ChatModelAgentConfig, error) {
	if ctx == nil {
		return nil, fmt.Errorf("executor context is required")
	}
	if cfg == nil || (cfg.Model == nil && cfg.Reliability == nil) || (cfg.Model != nil && cfg.Reliability != nil) {
		return nil, fmt.Errorf("executor requires exactly one Model or Reliability configuration")
	}

	registeredTools, err := aitools.GetManyRequired(cfg.RegisteredToolNames)
	if err != nil {
		return nil, fmt.Errorf("resolve executor Registry Tools: %w", err)
	}
	toolList := make([]tool.BaseTool, 0, len(registeredTools)+len(cfg.AgentTools))
	toolList = append(toolList, registeredTools...)
	for i, agentTool := range cfg.AgentTools {
		if agentTool == nil {
			return nil, fmt.Errorf("executor AgentTool %d is nil", i)
		}
		toolList = append(toolList, agentTool)
	}

	handlers, err := airuntime.RuntimeHandlerFirst(cfg.RuntimeHandler, cfg.AdditionalHandlers...)
	if err != nil {
		return nil, fmt.Errorf("configure executor Handlers: %w", err)
	}

	agentConfig := &adk.ChatModelAgentConfig{
		Name:        "executor",
		Description: "an executor agent",
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: toolList},
		},
		GenModelInput: executorGenModelInput,
		OutputKey:     planexecute.ExecutedStepSessionKey,
		MaxIterations: executorMaxIterations,
		Handlers:      handlers,
	}
	if cfg.Model != nil {
		agentConfig.Model = cfg.Model
	} else if err = models.ConfigureChatModelAgent(agentConfig, cfg.Reliability); err != nil {
		return nil, fmt.Errorf("configure executor ChatModelAgent: %w", err)
	}
	return agentConfig, nil
}

func executorGenModelInput(ctx context.Context, _ string, _ *adk.AgentInput) ([]adk.Message, error) {
	userInputValue, ok := adk.GetSessionValue(ctx, planexecute.UserInputSessionKey)
	if !ok {
		return nil, fmt.Errorf("executor %s is missing", planexecute.UserInputSessionKey)
	}
	userInput, ok := userInputValue.([]adk.Message)
	if !ok {
		return nil, fmt.Errorf("executor %s has type %T", planexecute.UserInputSessionKey, userInputValue)
	}

	planValue, ok := adk.GetSessionValue(ctx, planexecute.PlanSessionKey)
	if !ok {
		return nil, fmt.Errorf("executor %s is missing", planexecute.PlanSessionKey)
	}
	currentPlan, ok := planValue.(planexecute.Plan)
	if !ok {
		return nil, fmt.Errorf("executor %s has type %T", planexecute.PlanSessionKey, planValue)
	}

	var executedSteps []planexecute.ExecutedStep
	executedStepsValue, ok := adk.GetSessionValue(ctx, planexecute.ExecutedStepsSessionKey)
	if ok {
		executedSteps, ok = executedStepsValue.([]planexecute.ExecutedStep)
		if !ok {
			return nil, fmt.Errorf("executor %s has type %T", planexecute.ExecutedStepsSessionKey, executedStepsValue)
		}
	}

	return formatExecutorInput(ctx, &planexecute.ExecutionContext{
		UserInput: userInput, Plan: currentPlan, ExecutedSteps: executedSteps,
	})
}

func formatExecutorInput(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
	planContent, err := in.Plan.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var userInput strings.Builder
	for _, message := range in.UserInput {
		userInput.WriteString(message.Content)
		userInput.WriteByte('\n')
	}
	var executedSteps strings.Builder
	for _, result := range in.ExecutedSteps {
		fmt.Fprintf(&executedSteps, "Step: %s\nResult: %s\n\n", result.Step, result.Result)
	}

	return planexecute.ExecutorPrompt.Format(ctx, map[string]any{
		"input":          userInput.String(),
		"plan":           string(planContent),
		"executed_steps": executedSteps.String(),
		"step":           in.Plan.FirstStep(),
	})
}
