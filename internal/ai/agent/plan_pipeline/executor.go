package plan_pipeline

import (
	"context"

	"SentinelOps/internal/ai/models"
	airuntime "SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/adk"
)

// NewExecutor 构建执行器，每次调用只执行 Plan 中的当前第一个步骤，执行完将结果交给 Replanner 评估。
// 使用快速响应模型：执行阶段以工具调用为主，速度优先。
//
// ── 工具集（Supervisor 模式，领域能力委托给 Worker Agent）────────────────────
//
//   - event_analysis_agent：事件分析 Worker（含 RAG pipeline）
//   - report_agent：报告生成 Worker（含 RAG pipeline）
//   - risk_assessment_agent：风险评估 Worker（含 RAG pipeline）
//   - solve_agent：应急响应 Worker（含 RAG pipeline）
//   - intelligence_agent：威胁情报 Worker（联网搜索 + 情报沉淀）
//   - ops_agent：智能运维 Worker（封禁/通知/状态更新）
//   - query_internal_docs：内部知识库文档检索（跨域基础工具）
//   - get_current_time：实时时间戳（时间范围查询辅助）
//
// ── 底层实现原理（P15 NewExecutorBuilder + 官方 planexecute）───────────────
//
//  1. 每次调用时从 Session 读取三项共享状态：
//     UserInput       - 原始任务描述（key="UserInput"）
//     Plan            - 当前完整步骤列表，取 FirstStep() 作为本次执行目标（key="Plan"）
//     ExecutedSteps   - 已完成步骤及结果列表，注入 Prompt 供 LLM 了解上下文（key="ExecutedSteps"）
//  2. 将以上内容渲染为 ExecutorPrompt，以 ReAct 循环（ChatModelAgent）驱动工具调用完成该步骤。
//  3. 执行结果存入 Session（key="ExecutedStep"），供 Replanner 在下一轮读取做重规划决策。
func NewExecutor(ctx context.Context) (adk.Agent, error) {
	return NewExecutorWithRuntimeHandler(ctx, airuntime.NewRuntimeHandler())
}

// NewExecutorWithRuntimeHandler 把同一个 RuntimeHandler 注入外层 Executor 和全部 AgentTool 叶子。
func NewExecutorWithRuntimeHandler(ctx context.Context, handler *airuntime.RuntimeHandler) (adk.Agent, error) {
	execModel, err := models.ChatDefault(ctx)
	if err != nil {
		return nil, err
	}
	return NewExecutorBuilder(ctx, &ExecutorBuilderConfig{
		Model:               execModel,
		RegisteredToolNames: []string{"query_internal_docs", "get_current_time"},
		AgentTools:          newWorkerAgentTools(ctx, handler),
		RuntimeHandler:      handler,
	})
}
