package base

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	summaryRetryMaxAttempts    = 1
	summaryFailoverMaxAttempts = 1
	fallbackSummaryMaxRunes    = 4000
	fallbackMessageMaxRunes    = 400
)

// NewContextGovernanceMiddleware 使用 Eino 官方 Summarization 与 clear-only Reduction。
// 不配置本机 offload Backend，Worker 换机恢复只依赖 Checkpoint 和 MySQL Snapshot。
func NewContextGovernanceMiddleware(ctx context.Context, summaryModel model.BaseModel[*schema.Message]) ([]adk.ChatModelAgentMiddleware, error) {
	if ctx == nil || summaryModel == nil {
		return nil, nil
	}
	// Eino 的 Summarization 在摘要模型返回空内容时直接失败整个 Run。摘要模型
	// 偶发返回空 Assistant 消息属于可恢复的传输/解析抖动，必须先重试，再用
	// 本地截断摘要兜底，避免已提交 Effect 的 Run 因上下文压缩而误报失败。
	retryMax := summaryRetryMaxAttempts
	failoverMax := summaryFailoverMaxAttempts
	summarizer, err := summarization.New(ctx, &summarization.Config{
		Model:           summaryModel,
		Trigger:         &summarization.TriggerCondition{ContextTokens: 12000, ContextMessages: 40},
		UserInstruction: "保留未完成计划、Approval address、Evidence ID、预算和关键 Tool 结果。",
		Retry: &summarization.RetryConfig{
			MaxRetries:  &retryMax,
			ShouldRetry: summaryModelReturnedEmpty,
		},
		Failover: &summarization.FailoverConfig{
			MaxRetries:     &failoverMax,
			ShouldFailover: summaryModelReturnedEmpty,
			GetFailoverModel: func(_ context.Context, failover *summarization.FailoverContext) (model.BaseModel[*schema.Message], []*schema.Message, error) {
				return &staticSummaryModel{content: fallbackSummaryText(failover.OriginalMessages)}, failover.OriginalMessages, nil
			},
		},
		Finalize: preserveControlMessages,
	})
	if err != nil {
		return nil, err
	}
	reducer, err := reduction.New(ctx, &reduction.Config{
		SkipTruncation:            true,
		MaxTokensForClear:         12000,
		ClearRetentionSuffixLimit: 2,
	})
	if err != nil {
		return nil, err
	}
	return []adk.ChatModelAgentMiddleware{summarizer, reducer}, nil
}

// summaryModelReturnedEmpty 让官方 Summarization 把“空 Assistant 内容”视为可重试失败。
func summaryModelReturnedEmpty(_ context.Context, response *schema.Message, err error) bool {
	return err != nil || assistantSummaryText(response) == ""
}

func assistantSummaryText(message *schema.Message) string {
	if message == nil || message.Role != schema.Assistant {
		return ""
	}
	return messageText(message)
}

func messageText(message *schema.Message) string {
	if message == nil {
		return ""
	}
	parts := make([]string, 0, len(message.AssistantGenMultiContent))
	for _, part := range message.AssistantGenMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return message.Content
}

// fallbackSummaryText 在摘要模型连续返回空内容时，按时间顺序截取最近上下文。
func fallbackSummaryText(messages []*schema.Message) string {
	const header = "摘要模型本轮未返回可用内容，以下为按时间顺序保留的近期上下文摘录：\n"
	remaining := fallbackSummaryMaxRunes - len([]rune(header))
	selected := make([]string, 0, len(messages))
	for index := len(messages) - 1; index >= 0 && remaining > 0; index-- {
		text := strings.TrimSpace(messageText(messages[index]))
		if text == "" {
			continue
		}
		text = truncateRunes(text, fallbackMessageMaxRunes)
		text = truncateRunes(text, remaining)
		selected = append(selected, text)
		remaining -= len([]rune(text)) + 1
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	body := strings.Join(selected, "\n")
	if strings.TrimSpace(body) == "" {
		body = "（本段上下文没有可提取的文本消息）"
	}
	return header + body
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// staticSummaryModel 是 failover 专用模型：始终返回本地生成的确定性摘要。
type staticSummaryModel struct {
	content string
}

func (m *staticSummaryModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage(m.content, nil), nil
}

func (m *staticSummaryModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("static summary model does not support streaming")
}

func preserveControlMessages(ctx context.Context, original []*schema.Message, summary *schema.Message) ([]*schema.Message, error) {
	result, err := summarization.DefaultFinalize(ctx, original, summary)
	if err != nil {
		return nil, err
	}
	for _, message := range original {
		if message == nil || !isControlMessage(message.Content) {
			continue
		}
		found := false
		for _, existing := range result {
			if existing != nil && existing.Content == message.Content {
				found = true
				break
			}
		}
		if !found {
			copy := *message
			result = append(result, &copy)
		}
	}
	return result, nil
}

func isControlMessage(content string) bool {
	content = strings.ToLower(content)
	for _, marker := range []string{"approval", "evidence", "budget", "plan"} {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}
