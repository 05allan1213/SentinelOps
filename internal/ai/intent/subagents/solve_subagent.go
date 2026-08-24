// solve_subagent.go 单事件应急响应 SubAgent：针对指定安全事件生成结构化处置方案，调用 solve_pipeline。
package subagents

import (
	"context"
	"errors"
	"io"
	"strings"

	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/agent/solve_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent/core"
)

// SolveSubAgent 单事件应急响应 SubAgent，调用 solve_pipeline.GetSolveAgent。
// 区别于 EventSubAgent（批量分析），本 Agent 聚焦于单条事件的应急处置方案生成。
type SolveSubAgent struct{}

func (a *SolveSubAgent) Name() core.IntentType { return core.IntentSolve }

// Execute 调用 solve_pipeline，传入会话级历史（含长期摘要），逐 token 流式推送处置方案。
func (a *SolveSubAgent) Execute(ctx context.Context, task *core.Task, callback core.StreamCallback) (*core.Result, error) {
	callback(core.IntentSolve, "[Solve Agent 应急响应生成中...]\n")

	runner, err := solve_pipeline.GetSolveAgent(ctx)
	if err != nil {
		return &core.Result{Intent: core.IntentSolve, Error: err}, err
	}

	mem := cache.GetSessionMemory(task.SessionId)
	input := &base.UserMessage{
		ID:      task.SessionId,
		Query:   task.Query,
		History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
	}

	stream, err := runner.Stream(ctx, input)
	if err != nil {
		return &core.Result{Intent: core.IntentSolve, Error: err}, err
	}
	defer stream.Close()

	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &core.Result{Intent: core.IntentSolve, Error: err}, err
		}
		if msg.Content != "" {
			callback(core.IntentSolve, msg.Content)
			sb.WriteString(msg.Content)
		}
	}

	return &core.Result{TaskID: task.ID, Intent: core.IntentSolve, Content: sb.String()}, nil
}

// init 在包加载时自动将 SolveSubAgent 注册到 registry
func init() { core.RegisterSubAgent(&SolveSubAgent{}) }
