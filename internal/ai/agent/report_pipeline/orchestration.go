package report_pipeline

import (
	"SentinelOps/internal/ai/agent"
	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
)

// GetReportAgent 返回报告生成 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：Qwen3.7 Max reasoning profile（深度推理版，适合生成结构完整、内容丰富的长篇报告）
// 工具集：query_events / query_reports / query_report_templates /
//
//	search_similar_events / get_current_time / create_report
var GetReportAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:      "ReportAgent",
	SystemPrompt:   agents.Report,
	ModelFactory:   models.ChatReasoning,
	MaxStep:        30,
	RewriteEnabled: true,
	SplitEnabled:   true,
	ToolNames: []string{
		"query_events",
		"query_reports",
		"query_report_templates",
		"search_similar_events",
		"get_current_time",
		"create_report",
		"web_search",
	},
})
