package summary_pipeline

import (
	"context"

	"SentinelOps/internal/ai/prompt/memory"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
)

// SummaryTemplateConfig 定义摘要 Prompt 模板的渲染配置
type SummaryTemplateConfig struct {
	FormatType schema.FormatType
	Templates  []schema.MessagesTemplate
}

// newSummaryTemplate 构建摘要 Prompt 渲染节点
//
// 渲染后 LLM 收到的 Messages：
//
//	[0] SystemMessage - 角色设定（专业的对话总结助手）
//	[1] UserMessage   - 摘要任务（包含对话内容和输出要求）
func newSummaryTemplate(ctx context.Context) (ctp prompt.ChatTemplate, err error) {
	config := &SummaryTemplateConfig{
		FormatType: schema.FString,
		Templates: []schema.MessagesTemplate{
			schema.SystemMessage(memory.SummarySystem),
			schema.UserMessage(memory.SummaryUser),
		},
	}
	ctp = prompt.FromMessages(config.FormatType, config.Templates...)
	return ctp, nil
}
