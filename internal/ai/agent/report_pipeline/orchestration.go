package report_pipeline

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

const reportMaxIterations = 30

var reportTools = []string{
	"query_events", "query_reports", "query_report_templates", "search_similar_events",
	"get_current_time", "create_report", "web_search",
}

// GetReportAgent 返回报告生成 Agent 单例（懒初始化，线程安全）。
//
// DAG 拓扑（由 base.BuildReactAgentGraph 统一实现）：
//
//	START → [InputToRag, InputToChat] → MilvusRetriever → Template → ReactAgent → END
//
// 模型：reasoning Profile，适合生成结构完整、内容丰富的长篇报告。
// 工具集：query_events / query_reports / query_report_templates /
//
//	search_similar_events / get_current_time / create_report
var GetReportAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:      "ReportAgent",
	SystemPrompt:   agents.Report,
	ModelFactory:   models.ChatReasoning,
	MaxStep:        reportMaxIterations,
	RewriteEnabled: true,
	SplitEnabled:   true,
	ToolNames:      reportTools,
})

// NewReportAgent 构建迁移后的报告专业 ChatModelAgent；Mutation Tool 保持可见但由 RuntimeHandler 拒绝执行。
func NewReportAgent(ctx context.Context, m model.ToolCallingChatModel, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return base.NewSpecialistAgent(ctx, base.SpecialistConfig{
		Name: "ReportAgent", Description: "Call the Report Agent to generate structured security reports (weekly/monthly/custom). Handles: creating new reports, querying existing reports, fetching report templates, summarizing event trends. Returns report content or creation confirmation.",
		Instruction: agents.Report, Model: m, Profile: "reasoning", RuntimeHandler: handler, MaxIterations: reportMaxIterations,
		RetrievalOptions:  base.RetrievalOptions{RewriteEnabled: true, SplitEnabled: true},
		ToolNames:         reportTools,
		ContextGovernance: m == nil,
	})
}

var (
	reportDurableOnce  sync.Once
	reportDurableAgent adk.Agent
	reportDurableErr   error
)

// BuildDurableReportAgent 使用调用方提供的唯一 RuntimeHandler 构建报告 Agent。
func BuildDurableReportAgent(ctx context.Context, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return NewReportAgent(ctx, nil, handler)
}

// GetDurableReportAgent 懒构建 ADK 报告 Agent；phase19 负责接入 Planner/AgentTool。
func GetDurableReportAgent(ctx context.Context) (adk.Agent, error) {
	reportDurableOnce.Do(func() {
		reportDurableAgent, reportDurableErr = BuildDurableReportAgent(ctx, runtime.NewRuntimeHandler())
	})
	if reportDurableErr != nil {
		return nil, fmt.Errorf("report ADK agent: %w", reportDurableErr)
	}
	return reportDurableAgent, nil
}
