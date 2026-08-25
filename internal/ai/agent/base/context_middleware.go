package base

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// NewContextGovernanceMiddleware 使用 Eino 官方 Summarization 与 clear-only Reduction。
// 不配置本机 offload Backend，Worker 换机恢复只依赖 Checkpoint 和 MySQL Snapshot。
func NewContextGovernanceMiddleware(ctx context.Context, summaryModel model.BaseModel[*schema.Message]) ([]adk.ChatModelAgentMiddleware, error) {
	if ctx == nil || summaryModel == nil {
		return nil, nil
	}
	summarizer, err := summarization.New(ctx, &summarization.Config{
		Model:           summaryModel,
		Trigger:         &summarization.TriggerCondition{ContextTokens: 12000, ContextMessages: 40},
		UserInstruction: "保留未完成计划、Approval address、Evidence ID、预算和关键 Tool 结果。",
		Finalize:        preserveControlMessages,
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
