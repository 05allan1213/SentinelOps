package plan_pipeline

import (
	"context"

	"SentinelOps/internal/ai/agent/event_analysis_pipeline"
	"SentinelOps/internal/ai/agent/intelligence_pipeline"
	"SentinelOps/internal/ai/agent/ops_pipeline"
	"SentinelOps/internal/ai/agent/report_pipeline"
	"SentinelOps/internal/ai/agent/risk_pipeline"
	"SentinelOps/internal/ai/agent/solve_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/memory"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// SessionIdCtxKey 保留旧调用方的 context key 兼容性；官方 AgentTool 会通过
// shared parent session 传播 ADK state，Worker 不再自行拼接历史或创建隔离协议。
type SessionIdCtxKey struct{}

// namedWorkerAgent 保留 migration manifest 的稳定 Worker 名称，同时把执行
// 委托给 P17/P18 真实的 ChatModelAgent。它不实现独立的 Agent loop、事件或恢复协议。
type namedWorkerAgent struct {
	name        string
	description string
	getter      func(context.Context) (adk.Agent, error)
}

func (a *namedWorkerAgent) Name(context.Context) string        { return a.name }
func (a *namedWorkerAgent) Description(context.Context) string { return a.description }

func (a *namedWorkerAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	agent, err := a.getter(ctx)
	if err != nil {
		iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		generator.Send(&adk.AgentEvent{Err: err})
		generator.Close()
		return iter
	}
	return agent.Run(ctx, workerAgentInput(ctx, input), opts...)
}

func workerAgentInput(ctx context.Context, input *adk.AgentInput) *adk.AgentInput {
	if input == nil {
		return nil
	}
	current := append([]adk.Message(nil), input.Messages...)
	if len(current) > 0 {
		last := current[len(current)-1]
		if last != nil {
			lastCopy := *last
			last = &lastCopy
			userID, _ := ctx.Value(memory.UserIdCtxKey{}).(string)
			if pref := memory.GetPreference(ctx, userID); pref != nil {
				last.Content = pref.FormatPromptHint() + last.Content
			}
			current[len(current)-1] = last
		}
	}
	sessionID, _ := ctx.Value(SessionIdCtxKey{}).(string)
	if sessionID == "" {
		return &adk.AgentInput{Messages: current, EnableStreaming: input.EnableStreaming}
	}
	mem := cache.GetSessionMemory(sessionID)
	history := cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary())
	messages := make([]*schema.Message, 0, len(history)+len(current))
	messages = append(messages, history...)
	messages = append(messages, current...)
	return &adk.AgentInput{Messages: messages, EnableStreaming: input.EnableStreaming}
}

type workerSpec struct {
	name        string
	description string
	getter      func(context.Context) (adk.Agent, error)
}

func workerTool(ctx context.Context, spec workerSpec) tool.BaseTool {
	return adk.NewAgentTool(ctx, &namedWorkerAgent{name: spec.name, description: spec.description, getter: spec.getter})
}

func workerSpecs() []workerSpec {
	return []workerSpec{
		{name: "event_analysis_agent", description: "Call the Event Analysis Agent to query, analyze and correlate security events. Handles: recent events listing, CVE analysis, severity distribution, event timeline, subscription status, threat correlation. Returns structured analysis results.", getter: event_analysis_pipeline.GetDurableEventAnalysisAgent},
		{name: "report_agent", description: "Call the Report Agent to generate structured security reports (weekly/monthly/custom). Handles: creating new reports, querying existing reports, fetching report templates, summarizing event trends. Returns report content or creation confirmation.", getter: report_pipeline.GetDurableReportAgent},
		{name: "risk_assessment_agent", description: "Call the Risk Assessment Agent to evaluate CVE severity, attack paths, and impact scope. Handles: CVE risk scoring, vulnerability assessment, CVSS analysis, attack surface analysis, mitigation priority ranking. Returns structured risk assessment.", getter: risk_pipeline.GetDurableRiskAgent},
		{name: "solve_agent", description: "Call the Solve Agent to generate emergency response plans for specific security incidents. Handles: incident containment steps, patch recommendations, remediation procedures, recovery guidance for a single event. Returns structured three-phase response plan.", getter: solve_pipeline.GetDurableSolveAgent},
		{name: "intelligence_agent", description: "Call the Intelligence Agent to search and analyze the latest threat intelligence from the internet. Handles: CVE details lookup, vulnerability advisories, exploit PoC status, threat actor profiling, malicious IP/domain reputation. Automatically saves findings to the local knowledge base. Returns structured threat intelligence report.", getter: intelligence_pipeline.GetDurableIntelligenceAgent},
		{name: "ops_agent", description: "Call the Ops Agent to trigger automated incident response for a specific security event. Handles: IP blocking, multi-channel alert notifications (DingTalk/WeCom/Email), event status updates. Requires event_id in the query. Returns execution result.", getter: ops_pipeline.GetDurableOpsAgent},
	}
}

func newWorkerAgentTools(ctx context.Context) []tool.BaseTool {
	specs := workerSpecs()
	tools := make([]tool.BaseTool, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, workerTool(ctx, spec))
	}
	return tools
}

// NewEventAnalysisWorker 返回官方 AgentTool；保留旧入口供兼容调用方使用。
func NewEventAnalysisWorker() tool.BaseTool {
	return workerTool(context.Background(), workerSpecs()[0])
}

// NewReportWorker 返回官方 AgentTool；保留旧入口供兼容调用方使用。
func NewReportWorker() tool.BaseTool { return workerTool(context.Background(), workerSpecs()[1]) }

// NewRiskAssessmentWorker 返回官方 AgentTool；保留旧入口供兼容调用方使用。
func NewRiskAssessmentWorker() tool.BaseTool {
	return workerTool(context.Background(), workerSpecs()[2])
}

// NewSolveWorker 返回官方 AgentTool；保留旧入口供兼容调用方使用。
func NewSolveWorker() tool.BaseTool { return workerTool(context.Background(), workerSpecs()[3]) }

// NewIntelligenceWorker 返回官方 AgentTool；保留旧入口供兼容调用方使用。
func NewIntelligenceWorker() tool.BaseTool { return workerTool(context.Background(), workerSpecs()[4]) }

// NewOpsWorker 返回官方 AgentTool；保留旧入口供兼容调用方使用。
func NewOpsWorker() tool.BaseTool { return workerTool(context.Background(), workerSpecs()[5]) }
