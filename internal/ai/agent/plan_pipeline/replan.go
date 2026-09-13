package plan_pipeline

import (
	"SentinelOps/internal/ai/models"
	airuntime "SentinelOps/internal/ai/runtime"
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/schema"
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
		ChatModel: withReplannerRequiredToolCalls(model, planexecute.RespondToolInfo.Name),
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
	return planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel:  withReplannerRequiredToolCalls(chatModel, planexecute.RespondToolInfo.Name),
		GenInputFn: durableReplannerInput,
	})
}

// durableReplannerInput 复用官方 Replanner 模板，并追加一条终止指令：
// 当最近一个执行步骤的结果已经是用户问题的完整答案时，Replanner 必须
// 立即调用 Respond 工具，避免简单任务反复追加“输出最终答案”的空步骤，
// 从而压缩真实 Eval 的 Token 与延迟。
func durableReplannerInput(ctx context.Context, in *planexecute.ExecutionContext) ([]adk.Message, error) {
	if in == nil || in.Plan == nil {
		return nil, fmt.Errorf("replanner execution context and plan are required")
	}
	planContent, err := in.Plan.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal replanner plan: %w", err)
	}
	msgs, err := planexecute.ReplannerPrompt.Format(ctx, map[string]any{
		"plan":           string(planContent),
		"input":          formatReplannerInput(in.UserInput),
		"executed_steps": formatReplannerSteps(in.ExecutedSteps),
		"plan_tool":      planexecute.PlanToolInfo.Name,
		"respond_tool":   planexecute.RespondToolInfo.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("format replanner prompt: %w", err)
	}
	msgs = append(msgs, schema.UserMessage(
		"Termination rules you MUST follow:\n"+
			"1. If the most recent executed step already completed the user request, call the Respond tool "+
			"immediately with that completed result. This includes steps that created, saved, archived, "+
			"or returned a final artifact.\n"+
			"2. NEVER call the plan tool with a step that repeats, paraphrases, or re-executes any step "+
			"listed in executed_steps. Duplicate or reworded duplicate steps are forbidden.\n"+
			"3. Only call the plan tool when the user request still has genuinely new, unexecuted work; "+
			"otherwise call Respond.",
	))
	return msgs, nil
}

func formatReplannerInput(input []adk.Message) string {
	var builder strings.Builder
	for _, message := range input {
		builder.WriteString(message.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func formatReplannerSteps(results []planexecute.ExecutedStep) string {
	var builder strings.Builder
	for _, result := range results {
		builder.WriteString(fmt.Sprintf("Step: %s\nResult: %s\n\n", result.Step, result.Result))
	}
	return builder.String()
}
