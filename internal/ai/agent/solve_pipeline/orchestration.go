// Package solve_pipeline 单事件解决方案生成 Agent。
// 区别于 event_analysis_pipeline（批量综合报告），本 Agent 专注于单条安全事件的
// 应急处置方案生成：相似历史事件检索 → 内部知识库查询 → 结构化三段式输出。
package solve_pipeline

import (
	"SentinelOps/internal/ai/agent"
	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
)

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
	MaxStep:      10,
	ToolNames: []string{
		"search_similar_events",
		"query_internal_docs",
		"web_search",
	},
})
