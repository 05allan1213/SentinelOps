package plan_pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const requiredToolCallAttempts = 2

// requiredToolCallModel 保证 Planner/Replanner 这类“必须用 Tool 输出结构化决策”
// 的调用即使遇到 provider 瞬时返回纯文本，也能拿到 ToolCall：
//  1. 复用同一个 ToolChoiceForced 请求重试；
//  2. Replanner 场景下把非空纯文本兜底包装成 respond ToolCall，避免官方
//     planexecute 直接以 "no tool call" 终止整个 Run。
type requiredToolCallModel struct {
	inner        model.ToolCallingChatModel
	attempts     int
	fallbackTool string
	fallbackKey  string
}

func withPlannerRequiredToolCalls(inner model.ToolCallingChatModel) model.ToolCallingChatModel {
	return &requiredToolCallModel{inner: inner, attempts: requiredToolCallAttempts}
}

func withReplannerRequiredToolCalls(inner model.ToolCallingChatModel, respondTool string) model.ToolCallingChatModel {
	return &requiredToolCallModel{
		inner: inner, attempts: requiredToolCallAttempts,
		fallbackTool: respondTool, fallbackKey: "response",
	}
}

func (m *requiredToolCallModel) Generate(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.Message, error) {
	attempts := m.attempts
	if attempts < 1 {
		attempts = 1
	}
	var last *schema.Message
	for attempt := 0; attempt < attempts; attempt++ {
		message, err := m.inner.Generate(ctx, input, options...)
		if err != nil {
			return nil, err
		}
		if hasToolCall(message) {
			return message, nil
		}
		last = message
	}
	if m.fallbackTool != "" {
		if fallback := respondFallbackMessage(m.fallbackTool, m.fallbackKey, last); fallback != nil {
			return fallback, nil
		}
	}
	// 保留最后一次响应，让官方 planexecute 继续给出明确的 "no tool call" 错误。
	return last, nil
}

// Stream 与 Generate 保持同一约束：官方 compose ChatModel 节点会走 Stream，
// 因此必须在这里完成“缓冲 → 校验 tool call → 重试/兜底”，否则官方 planexecute
// 仍会在 CollectableLambda 中直接报 "no tool call"。
func (m *requiredToolCallModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	attempts := m.attempts
	if attempts < 1 {
		attempts = 1
	}
	var last []*schema.Message
	for attempt := 0; attempt < attempts; attempt++ {
		stream, err := m.inner.Stream(ctx, input, options...)
		if err != nil {
			return nil, err
		}
		chunks, err := drainMessageStream(stream)
		if err != nil {
			return nil, err
		}
		if hasToolCall(concatenatedMessage(chunks)) {
			return schema.StreamReaderFromArray(chunks), nil
		}
		last = chunks
	}
	if m.fallbackTool != "" {
		if fallback := respondFallbackMessage(m.fallbackTool, m.fallbackKey, concatenatedMessage(last)); fallback != nil {
			return schema.StreamReaderFromArray([]*schema.Message{fallback}), nil
		}
	}
	return schema.StreamReaderFromArray(last), nil
}

func (m *requiredToolCallModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	configured, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &requiredToolCallModel{
		inner: configured, attempts: m.attempts,
		fallbackTool: m.fallbackTool, fallbackKey: m.fallbackKey,
	}, nil
}

func hasToolCall(message *schema.Message) bool {
	return message != nil && len(message.ToolCalls) > 0
}

func drainMessageStream(stream *schema.StreamReader[*schema.Message]) ([]*schema.Message, error) {
	if stream == nil {
		return nil, nil
	}
	chunks := make([]*schema.Message, 0, 8)
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, message)
	}
	return chunks, nil
}

func concatenatedMessage(chunks []*schema.Message) *schema.Message {
	if len(chunks) == 0 {
		return nil
	}
	if len(chunks) == 1 {
		return chunks[0]
	}
	message, err := schema.ConcatMessages(chunks)
	if err != nil {
		return nil
	}
	return message
}

func respondFallbackMessage(toolName, argumentKey string, message *schema.Message) *schema.Message {
	if message == nil || strings.TrimSpace(message.Content) == "" {
		return nil
	}
	arguments, err := json.Marshal(map[string]string{argumentKey: message.Content})
	if err != nil {
		return nil
	}
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID:   fmt.Sprintf("fallback-%s", toolName),
		Type: "function",
		Function: schema.FunctionCall{
			Name:      toolName,
			Arguments: string(arguments),
		},
	}})
}
