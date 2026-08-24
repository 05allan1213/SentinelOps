// report_subagent.go 报告生成 SubAgent：处理报告生成、查看报告、报告数据分析。
package subagents

import (
	"SentinelOps/internal/ai/agent/report_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent/core"
	"context"
	"errors"
	"io"
	"strings"
)

// ReportSubAgent 报告生成 SubAgent，调用 report_pipeline。
type ReportSubAgent struct{}

func (a *ReportSubAgent) Name() core.IntentType { return core.IntentReport }

// Execute 调用 report_pipeline，传入会话级历史（含长期摘要），逐 token 流式推送报告内容。
func (a *ReportSubAgent) Execute(ctx context.Context, task *core.Task, callback core.StreamCallback) (*core.Result, error) {
	callback(core.IntentStatus, "[Report Agent 生成中...]")

	runner, err := report_pipeline.GetReportAgent(ctx)
	if err != nil {
		return &core.Result{Intent: core.IntentReport, Error: err}, err
	}

	mem := cache.GetSessionMemory(task.SessionId)
	input := &report_pipeline.UserMessage{
		Query:   task.Query,
		History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
	}

	stream, err := runner.Stream(ctx, input)
	if err != nil {
		return &core.Result{Intent: core.IntentReport, Error: err}, err
	}
	defer stream.Close()

	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &core.Result{Intent: core.IntentReport, Error: err}, err
		}
		if msg.Content != "" {
			callback(core.IntentReport, msg.Content)
			sb.WriteString(msg.Content)
		}
	}

	content := sb.String()
	return &core.Result{Intent: core.IntentReport, Content: content}, nil
}

// init 在包加载时自动将 ReportSubAgent 注册到 registry
func init() { core.RegisterSubAgent(&ReportSubAgent{}) }
