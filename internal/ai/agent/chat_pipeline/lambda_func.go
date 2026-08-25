package chat_pipeline

import (
	"context"
	"time"
)

// newInputToRagLambda 只把当前任务 Query 传给唯一 Retriever，绝不拼接 History。
func newInputToRagLambda(_ context.Context, input *UserMessage, _ ...any) (string, error) {
	if input == nil {
		return "", nil
	}
	return input.Query, nil
}

// newInputToChatLambda 构建一次性的 Prompt 输入；上下文压缩只由新 ADK 官方 Middleware 负责。
func newInputToChatLambda(_ context.Context, input *UserMessage, _ ...any) (map[string]any, error) {
	if input == nil {
		return nil, nil
	}
	return map[string]any{
		"content": input.Query,
		"history": input.History,
		"date":    time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}
