// Package solve_pipeline 单事件解决方案生成 Agent。
// 区别于 event_analysis_pipeline（批量综合报告），本 Agent 专注于单条安全事件的
// 应急处置方案生成：相似历史事件检索 → 内部知识库查询 → 结构化三段式输出。
package solve_pipeline

import (
	"context"
	"fmt"
	"sync"

	"SentinelOps/internal/ai/agent"
	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
	"SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
)

var solveL0Tools = []string{"search_similar_events", "query_internal_docs", "web_search"}

const solveMaxIterations = 10

// UserMessage 复用 base.UserMessage（type alias）。
type UserMessage = base.UserMessage

// GetSolveAgent 返回解决方案生成 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：reasoning Profile，单事件方案需充分推导攻击路径和修复措施。
// 工具集（最小化，专注于单事件）：
//   - search_similar_events：检索相似历史事件和处置记录
//   - query_internal_docs：查询内部安全知识库
var GetSolveAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:    "SolveAgent",
	SystemPrompt: agents.Solve,
	ModelFactory: models.ChatReasoning,
	MaxStep:      solveMaxIterations,
	ToolNames:    solveL0Tools,
})

// NewSolveAgent builds the migrated read-only L0 ChatModelAgent.
func NewSolveAgent(ctx context.Context, m model.ToolCallingChatModel, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return base.NewSpecialistAgent(ctx, base.SpecialistConfig{
		Name: "SolveAgent", Description: "Call the Solve Agent to generate emergency response plans for specific security incidents. Handles: incident containment steps, patch recommendations, remediation procedures, recovery guidance for a single event. Returns structured three-phase response plan.",
		Instruction: agents.Solve, Model: m, Profile: "reasoning", RuntimeHandler: handler, MaxIterations: solveMaxIterations,
		RetrievalOptions:  base.RetrievalOptions{RewriteEnabled: false, SplitEnabled: false},
		ToolNames:         solveL0Tools,
		ContextGovernance: m == nil,
	})
}

var (
	durableOnce  sync.Once
	durableAgent adk.Agent
	durableErr   error
)

// BuildDurableSolveAgent 使用调用方提供的唯一 RuntimeHandler 构建 L0 处置 Agent。
func BuildDurableSolveAgent(ctx context.Context, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return NewSolveAgent(ctx, nil, handler)
}

// GetDurableSolveAgent lazily constructs the ADK specialist. P19 owns outer
// Planner/AgentTool wiring.
func GetDurableSolveAgent(ctx context.Context) (adk.Agent, error) {
	durableOnce.Do(func() {
		durableAgent, durableErr = BuildDurableSolveAgent(ctx, runtime.NewRuntimeHandler())
	})
	if durableErr != nil {
		return nil, fmt.Errorf("solve ADK agent: %w", durableErr)
	}
	return durableAgent, nil
}
