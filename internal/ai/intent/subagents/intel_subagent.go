// intel_subagent.go 威胁情报 SubAgent：处理联网情报检索、CVE 详情查询、威胁组织分析、情报沉淀。
package subagents

import (
	"SentinelOps/internal/ai/agent/intelligence_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent/core"
	"context"
	"errors"
	"io"
	"strings"
)

// IntelSubAgent 威胁情报 SubAgent，调用 intelligence_pipeline。
// 负责"联网搜索 → 抓取原文 → 分析 → 沉淀"四段式情报收集流程。
type IntelSubAgent struct{}

func (a *IntelSubAgent) Name() core.IntentType { return core.IntentIntel }

// Execute 调用 intelligence_pipeline，传入会话级历史，逐 token 流式推送情报分析结果。
func (a *IntelSubAgent) Execute(ctx context.Context, task *core.Task, callback core.StreamCallback) (*core.Result, error) {
	callback(core.IntentStatus, "[Intelligence Agent 联网情报检索中...]")

	runner, err := intelligence_pipeline.GetIntelligenceAgent(ctx)
	if err != nil {
		return &core.Result{Intent: core.IntentIntel, Error: err}, err
	}

	mem := cache.GetSessionMemory(task.SessionId)
	input := &intelligence_pipeline.UserMessage{
		Query:   task.Query,
		History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
	}

	stream, err := runner.Stream(ctx, input)
	if err != nil {
		return &core.Result{Intent: core.IntentIntel, Error: err}, err
	}
	defer stream.Close()

	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &core.Result{Intent: core.IntentIntel, Error: err}, err
		}
		if msg.Content != "" {
			callback(core.IntentIntel, msg.Content)
			sb.WriteString(msg.Content)
		}
	}

	content := sb.String()
	return &core.Result{Intent: core.IntentIntel, Content: content}, nil
}

// init 在包加载时自动将 IntelSubAgent 注册到 registry
func init() { core.RegisterSubAgent(&IntelSubAgent{}) }
