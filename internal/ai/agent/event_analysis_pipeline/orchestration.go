package event_analysis_pipeline

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

const eventAnalysisMaxStep = 25

var eventAnalysisL0Tools = []string{
	"query_events", "search_similar_events", "query_subscriptions", "query_reports",
	"query_internal_docs", "get_current_time", "web_search",
}

// GetEventAnalysisAgent 返回事件分析 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：default Profile，适合实时事件分析的多步工具调用。
// 工具集：query_events / search_similar_events / query_subscriptions /
//
//	query_reports / query_internal_docs / get_current_time / web_search
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
	ToolNames:      eventAnalysisL0Tools,
})

// NewEventAnalysisAgent builds the migrated read-only L0 ChatModelAgent.
func NewEventAnalysisAgent(ctx context.Context, m model.ToolCallingChatModel, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return base.NewSpecialistAgent(ctx, base.SpecialistConfig{
		Name: "EventAnalysisAgent", Description: "Call the Event Analysis Agent to query, analyze and correlate security events. Handles: recent events listing, CVE analysis, severity distribution, event timeline, subscription status, threat correlation. Returns structured analysis results.",
		Instruction: agents.EventAnalysis, Model: m, RuntimeHandler: handler, MaxIterations: eventAnalysisMaxStep,
		RetrievalOptions: base.RetrievalOptions{RewriteEnabled: true, SplitEnabled: true},
		ToolNames:        eventAnalysisL0Tools,
	})
}

var (
	durableOnce  sync.Once
	durableAgent adk.Agent
	durableErr   error
)

// BuildDurableEventAnalysisAgent 使用调用方提供的唯一 RuntimeHandler 构建 L0 专业 Agent。
func BuildDurableEventAnalysisAgent(ctx context.Context, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	m, err := newEventModel(ctx)
	if err != nil {
		return nil, err
	}
	return NewEventAnalysisAgent(ctx, m, handler)
}

// GetDurableEventAnalysisAgent lazily constructs the ADK specialist. It is not
// connected to the outer Planner until P19.
func GetDurableEventAnalysisAgent(ctx context.Context) (adk.Agent, error) {
	durableOnce.Do(func() {
		durableAgent, durableErr = BuildDurableEventAnalysisAgent(ctx, runtime.NewRuntimeHandler())
	})
	if durableErr != nil {
		return nil, fmt.Errorf("event analysis ADK agent: %w", durableErr)
	}
	return durableAgent, nil
}
