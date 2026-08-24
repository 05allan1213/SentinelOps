// chat_subagent.go 通用对话 SubAgent：处理安全咨询、知识问答、日志查询、订阅管理等默认场景。
package subagents

import (
	"SentinelOps/internal/ai/agent/chat_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent/core"
	"context"
	"errors"
	"io"
	"strings"
)

// ChatSubAgent 通用对话 SubAgent，调用 chat_pipeline，使用会话级 cache.SessionMemory 构建上下文。
// 作为 Router 意图识别失败时的默认降级目标。
type ChatSubAgent struct{}

func (a *ChatSubAgent) Name() core.IntentType { return core.IntentChat }

// Execute 调用 chat_pipeline.GetChatAgent，传入 task 与会话历史（含长期摘要），逐 token 流式推送回复。
func (a *ChatSubAgent) Execute(ctx context.Context, task *core.Task, callback core.StreamCallback) (*core.Result, error) {
	callback(core.IntentStatus, "[Chat Agent 处理中...]")

	runner, err := chat_pipeline.GetChatAgent(ctx)
	if err != nil {
		return &core.Result{Intent: core.IntentChat, Error: err}, err
	}

	// 按 sessionId 获取会话级记忆，构建含长期摘要的历史上下文
	mem := cache.GetSessionMemory(task.SessionId)
	history := cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary())

	input := &chat_pipeline.UserMessage{
		Query:   task.Query,
		History: history,
	}

	stream, err := runner.Stream(ctx, input)
	if err != nil {
		return &core.Result{Intent: core.IntentChat, Error: err}, err
	}
	defer stream.Close()

	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &core.Result{Intent: core.IntentChat, Error: err}, err
		}
		if msg.Content != "" {
			callback(core.IntentChat, msg.Content)
			sb.WriteString(msg.Content)
		}
	}

	content := sb.String()
	return &core.Result{Intent: core.IntentChat, Content: content}, nil
}

// init 在包加载时自动将 ChatSubAgent 注册到 registry
func init() { core.RegisterSubAgent(&ChatSubAgent{}) }
