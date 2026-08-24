// event_subagent.go 事件分析 SubAgent：处理安全事件查询、事件分析、告警关联、事件处置建议。
package subagents

import (
	"SentinelOps/internal/ai/agent/event_analysis_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent/core"
	"context"
	"errors"
	"io"
	"strings"
)

// EventSubAgent 事件分析 SubAgent，调用 event_analysis_pipeline。
type EventSubAgent struct{}

func (a *EventSubAgent) Name() core.IntentType { return core.IntentEvent }

// Execute 调用 event_analysis_pipeline，传入会话级历史（含长期摘要），逐 token 流式推送分析结果。
func (a *EventSubAgent) Execute(ctx context.Context, task *core.Task, callback core.StreamCallback) (*core.Result, error) {
	callback(core.IntentStatus, "[Event Agent 分析中...]")

	runner, err := event_analysis_pipeline.GetEventAnalysisAgent(ctx)
	if err != nil {
		return &core.Result{Intent: core.IntentEvent, Error: err}, err
	}

	mem := cache.GetSessionMemory(task.SessionId)
	input := &event_analysis_pipeline.UserMessage{
		Query:   task.Query,
		History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
	}

	stream, err := runner.Stream(ctx, input)
	if err != nil {
		return &core.Result{Intent: core.IntentEvent, Error: err}, err
	}
	defer stream.Close()

	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &core.Result{Intent: core.IntentEvent, Error: err}, err
		}
		if msg.Content != "" {
			callback(core.IntentEvent, msg.Content)
			sb.WriteString(msg.Content)
		}
	}

	content := sb.String()
	return &core.Result{Intent: core.IntentEvent, Content: content}, nil
}

// init 在包加载时自动将 EventSubAgent 注册到 registry
func init() { core.RegisterSubAgent(&EventSubAgent{}) }
