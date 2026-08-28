// Package risk_pipeline 风险评估 Agent。
package risk_pipeline

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

var riskL0Tools = []string{
	"query_events", "query_reports", "search_similar_events", "query_internal_docs",
	"query_subscriptions", "get_current_time", "web_search",
}

const riskMaxIterations = 25

// GetRiskAgent 返回风险评估 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：reasoning Profile，适合深度分析 CVE、评估攻击路径和影响范围。
// 工具集：query_events / query_reports / search_similar_events /
//
//	query_internal_docs / query_subscriptions / get_current_time
var GetRiskAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:      "RiskAgent",
	SystemPrompt:   agents.Risk,
	ModelFactory:   models.ChatReasoning,
	MaxStep:        riskMaxIterations,
	RewriteEnabled: true,
	SplitEnabled:   true,
	ToolNames:      riskL0Tools,
})

// NewRiskAgent builds the migrated read-only L0 ChatModelAgent.
func NewRiskAgent(ctx context.Context, m model.ToolCallingChatModel, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return base.NewSpecialistAgent(ctx, base.SpecialistConfig{
		Name: "RiskAgent", Description: "Call the Risk Assessment Agent to evaluate CVE severity, attack paths, and impact scope. Handles: CVE risk scoring, vulnerability assessment, CVSS analysis, attack surface analysis, mitigation priority ranking. Returns structured risk assessment.",
		Instruction: agents.Risk, Model: m, Profile: "reasoning", RuntimeHandler: handler, MaxIterations: riskMaxIterations,
		RetrievalOptions:  base.RetrievalOptions{RewriteEnabled: true, SplitEnabled: true},
		ToolNames:         riskL0Tools,
		ContextGovernance: m == nil,
	})
}

var (
	durableOnce  sync.Once
	durableAgent adk.Agent
	durableErr   error
)

// BuildDurableRiskAgent 使用调用方提供的唯一 RuntimeHandler 构建 L0 风险 Agent。
func BuildDurableRiskAgent(ctx context.Context, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return NewRiskAgent(ctx, nil, handler)
}

// GetDurableRiskAgent lazily constructs the ADK specialist. phase19 owns outer
// Planner/AgentTool wiring.
func GetDurableRiskAgent(ctx context.Context) (adk.Agent, error) {
	durableOnce.Do(func() {
		durableAgent, durableErr = BuildDurableRiskAgent(ctx, runtime.NewRuntimeHandler())
	})
	if durableErr != nil {
		return nil, fmt.Errorf("risk ADK agent: %w", durableErr)
	}
	return durableAgent, nil
}
