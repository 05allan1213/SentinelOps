package event_analysis_pipeline

import (
	"SentinelOps/internal/ai/agent"
	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
)

const eventAnalysisMaxStep = 25

// GetEventAnalysisAgent 返回事件分析 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：Qwen3.7 Max default profile（低延迟，适合实时事件分析的多步工具调用）
// 工具集：query_events / search_similar_events / query_subscriptions /
//
//	query_reports / query_internal_docs / get_current_time /
//	web_search / save_intelligence
//
// 深度思考模式下，EventAnalysis Worker 会承接多子问题综合分析任务，
// 15 步在“检索 + 多次事件工具调用 + 汇总”场景下容易触发 exceeds max steps。
var GetEventAnalysisAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:      "EventAnalysisAgent",
	SystemPrompt:   agents.EventAnalysis,
	ModelFactory:   models.ChatDefault,
	MaxStep:        eventAnalysisMaxStep,
	RewriteEnabled: true,
	SplitEnabled:   true,
	ToolNames: []string{
		"query_events",
		"search_similar_events",
		"query_subscriptions",
		"query_reports",
		"query_internal_docs",
		"get_current_time",
		"web_search",
		"save_intelligence",
	},
})
