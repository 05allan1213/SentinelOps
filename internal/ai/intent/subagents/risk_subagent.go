// risk_subagent.go 风险评估 SubAgent：处理风险评估、威胁分析、漏洞评估、安全评分、CVE 分析。
package subagents

import (
	"SentinelOps/internal/ai/agent/risk_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent/core"
	"context"
	"errors"
	"io"
	"strings"
)

// RiskSubAgent 风险评估 SubAgent，调用 risk_pipeline。
type RiskSubAgent struct{}

func (a *RiskSubAgent) Name() core.IntentType { return core.IntentRisk }

// Execute 调用 risk_pipeline，传入会话级历史（含长期摘要），逐 token 流式推送评估结果。
func (a *RiskSubAgent) Execute(ctx context.Context, task *core.Task, callback core.StreamCallback) (*core.Result, error) {
	callback(core.IntentStatus, "[Risk Agent 评估中...]")

	runner, err := risk_pipeline.GetRiskAgent(ctx)
	if err != nil {
		return &core.Result{Intent: core.IntentRisk, Error: err}, err
	}

	mem := cache.GetSessionMemory(task.SessionId)
	input := &risk_pipeline.UserMessage{
		Query:   task.Query,
		History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
	}

	stream, err := runner.Stream(ctx, input)
	if err != nil {
		return &core.Result{Intent: core.IntentRisk, Error: err}, err
	}
	defer stream.Close()

	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &core.Result{Intent: core.IntentRisk, Error: err}, err
		}
		if msg.Content != "" {
			callback(core.IntentRisk, msg.Content)
			sb.WriteString(msg.Content)
		}
	}

	content := sb.String()
	return &core.Result{Intent: core.IntentRisk, Content: content}, nil
}

// init 在包加载时自动将 RiskSubAgent 注册到 registry
func init() { core.RegisterSubAgent(&RiskSubAgent{}) }
