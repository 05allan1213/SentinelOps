package plan_pipeline

import (
	"SentinelOps/internal/ai/models"
	airuntime "SentinelOps/internal/ai/runtime"
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
)

// NewRePlanAgent 构建重构规划器，在 Executor 完成一个步骤后决策：继续执行还是终止输出。
// 使用深度思考模型（与 Planner 同级推理要求）。
//
// 底层实现原理（planexecute.NewReplanner）：
//  1. 将 ChatModel 同时绑定两个 Tool：PlanTool（继续）和 RespondTool（终止），设置 ToolChoiceForced，
//     强制 LLM 必须二选一调用，避免输出游离文本导致流程无法继续。
//  2. 从 Session 读取 ExecutedStep（刚完成的步骤结果）并追加到 ExecutedSteps 列表，
//     连同 UserInput、原始 Plan 一起渲染为 ReplannerPrompt 后调用 LLM。
//  3. 根据 LLM 调用的工具名分叉处理：
//     调用 RespondTool → 任务已完成，向事件流发送 BreakLoopAction 打破外层循环，流程终止。
//     调用 PlanTool   → 需要调整，将新的剩余步骤列表反序列化为 Plan 写回 Session（key="Plan"），
//     外层 LoopAgent 进入下一轮，Executor 继续执行新 Plan 的 FirstStep。
//
// 注意：使用 ChatModel（ToolCallingChatModel），因为 Replanner 自身不需要调用业务工具，
// 只需通过 Tool Call 输出结构化决策（Plan JSON 或 Response JSON）。
func NewRePlanAgent(ctx context.Context) (adk.Agent, error) {
	model, err := models.ChatReasoning(ctx)
	if err != nil {
		return nil, err
	}
	return planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: model,
	})
}

// NewRePlanAgentWithRuntimeHandler 构建 durable Replanner，并复用已绑定的物理模型候选。
func NewRePlanAgentWithRuntimeHandler(ctx context.Context, handler *airuntime.RuntimeHandler) (adk.Agent, error) {
	if handler == nil {
		return nil, fmt.Errorf("durable Replanner RuntimeHandler is required")
	}
	reliability, err := models.BuildReliability(ctx, "reasoning", airuntime.PhysicalModelBinder(handler))
	if err != nil {
		return nil, err
	}
	chatModel, err := reliability.PrimaryToolCallingModel()
	if err != nil {
		return nil, err
	}
	return planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{ChatModel: chatModel})
}
