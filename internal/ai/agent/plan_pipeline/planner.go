package plan_pipeline

import (
	"context"
	"fmt"

	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
	airuntime "SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
)

// customPlannerGenInput 使用安全领域感知的系统提示词替换默认 Planner 提示词。
func customPlannerGenInput(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
	customPrompt := prompt.FromMessages(schema.FString,
		schema.SystemMessage(agents.Planner),
		schema.MessagesPlaceholder("input", false),
	)
	return customPrompt.Format(ctx, map[string]any{
		"input": userInput,
	})
}

// NewPlanner 构建规划器，将初始任务拆解为有序步骤清单交由 Executor 逐步执行。
// 使用深度思考模型：规划只发生一次，需要全局推理，质量优先于延迟。
//
// 底层实现原理（planexecute.NewPlanner）：
//  1. 将 ToolCallingChatModel 绑定到内置的 PlanTool（JSON Schema: {"steps": [string...]}），
//     并设置 ToolChoiceForced，强制 LLM 必须调用 PlanTool 而非自由回答，保证输出为结构化 JSON。
//  2. 构建 Chain：PlannerPrompt 渲染 → LLM 调用（强制 Tool Call）→ 提取 ToolCall.Arguments → JSON 反序列化为 Plan 结构体。
//  3. 将生成的 Plan 存入 Session（key="Plan"），后续 Executor 和 Replanner 通过 Session 读取，无需参数传递。
func NewPlanner(ctx context.Context) (adk.Agent, error) {
	planModel, err := models.ChatReasoning(ctx)
	if err != nil {
		return nil, err
	}
	// ToolCallingChatModel：Planner 通过 Tool Call 输出结构化 Plan，
	// 比自由文本生成更稳定，JSON 解析成功率更高。
	return planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: planModel,
		GenInputFn:           customPlannerGenInput,
	})
}

// NewPlannerWithRuntimeHandler 构建 durable Planner，并复用已绑定的物理模型候选。
func NewPlannerWithRuntimeHandler(ctx context.Context, handler *airuntime.RuntimeHandler) (adk.Agent, error) {
	if handler == nil {
		return nil, fmt.Errorf("durable Planner RuntimeHandler is required")
	}
	reliability, err := models.BuildReliability(ctx, "reasoning", airuntime.PhysicalModelBinder(handler))
	if err != nil {
		return nil, err
	}
	planModel, err := reliability.PrimaryToolCallingModel()
	if err != nil {
		return nil, err
	}
	return planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: planModel,
		GenInputFn:           customPlannerGenInput,
	})
}
