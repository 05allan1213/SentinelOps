package intelligence_pipeline

import (
	"SentinelOps/internal/ai/agent"
	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
)

// GetIntelligenceAgent 返回威胁情报 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：default Profile，适合实时情报检索的多步工具调用。
// 工具集：query_internal_docs / get_current_time /
//
//	web_search / save_intelligence
//
// 设计原则：工具集精简，职责单一——专注"联网采集 → 分析 → 沉淀"三段式情报流程，
// 不包含 query_events 等本地事件工具（事件分析由 Event Agent 负责）。
var GetIntelligenceAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:      "IntelligenceAgent",
	SystemPrompt:   agents.Intelligence,
	ModelFactory:   models.ChatDefault,
	MaxStep:        12,
	RewriteEnabled: false, // 情报查询通常是明确的查询词（CVE 编号/漏洞名称），不需要重写
	SplitEnabled:   false, // 情报查询聚焦单一主题，无需子问题拆分
	ToolNames: []string{
		"query_internal_docs",
		"get_current_time",
		"web_search",
		"save_intelligence",
	},
})
