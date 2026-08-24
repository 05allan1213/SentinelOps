// Package risk_pipeline 风险评估 Agent。
package risk_pipeline

import (
	"SentinelOps/internal/ai/agent"
	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
)

// GetRiskAgent 返回风险评估 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：Qwen3.7 Max reasoning profile（深度推理版，适合深度分析 CVE、评估攻击路径和影响范围）
// 工具集：query_events / query_reports / search_similar_events /
//
//	query_internal_docs / query_subscriptions / get_current_time
var GetRiskAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:      "RiskAgent",
	SystemPrompt:   agents.Risk,
	ModelFactory:   models.ChatReasoning,
	MaxStep:        25,
	RewriteEnabled: true,
	SplitEnabled:   true,
	ToolNames: []string{
		"query_events",
		"query_reports",
		"search_similar_events",
		"query_internal_docs",
		"query_subscriptions",
		"get_current_time",
		"web_search",
	},
})
