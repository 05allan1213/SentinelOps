package intelligence_pipeline

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

const intelligenceMaxIterations = 12

var intelligenceTools = []string{"query_internal_docs", "get_current_time", "web_search", "save_intelligence"}

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
	MaxStep:        intelligenceMaxIterations,
	RewriteEnabled: false, // 情报查询通常是明确的查询词（CVE 编号/漏洞名称），不需要重写
	SplitEnabled:   false, // 情报查询聚焦单一主题，无需子问题拆分
	ToolNames:      intelligenceTools,
})

// NewIntelligenceAgent 构建迁移后的情报专业 ChatModelAgent；save_intelligence 由 RuntimeHandler fail-closed。
func NewIntelligenceAgent(ctx context.Context, m model.ToolCallingChatModel, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return base.NewSpecialistAgent(ctx, base.SpecialistConfig{
		Name: "IntelligenceAgent", Description: "Call the Intelligence Agent to search and analyze the latest threat intelligence from the internet. Handles: CVE details lookup, vulnerability advisories, exploit PoC status, threat actor profiling, malicious IP/domain reputation. Automatically saves findings to the local knowledge base. Returns structured threat intelligence report.",
		Instruction: agents.Intelligence, Model: m, Profile: "default", RuntimeHandler: handler, MaxIterations: intelligenceMaxIterations,
		RetrievalOptions: base.RetrievalOptions{RewriteEnabled: false, SplitEnabled: false},
		ToolNames:        intelligenceTools,
	})
}

var (
	intelligenceDurableOnce  sync.Once
	intelligenceDurableAgent adk.Agent
	intelligenceDurableErr   error
)

// BuildDurableIntelligenceAgent 使用调用方提供的唯一 RuntimeHandler 构建情报 Agent。
func BuildDurableIntelligenceAgent(ctx context.Context, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return NewIntelligenceAgent(ctx, nil, handler)
}

// GetDurableIntelligenceAgent 懒构建 ADK 情报 Agent；P19 负责接入 Planner/AgentTool。
func GetDurableIntelligenceAgent(ctx context.Context) (adk.Agent, error) {
	intelligenceDurableOnce.Do(func() {
		intelligenceDurableAgent, intelligenceDurableErr = BuildDurableIntelligenceAgent(ctx, runtime.NewRuntimeHandler())
	})
	if intelligenceDurableErr != nil {
		return nil, fmt.Errorf("intelligence ADK agent: %w", intelligenceDurableErr)
	}
	return intelligenceDurableAgent, nil
}
