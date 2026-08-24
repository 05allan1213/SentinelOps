package summary_pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// formatMessagesLambda 将消息列表格式化为文本
//
// 输入：*SummaryInput
// 输出：map[string]any{"conversation": string}
//
// 格式化规则：
//   - 每条消息格式：[序号] 角色: 内容
//   - 过滤空消息和系统消息
//   - 截断过长的消息（单条超过 1000 字符时截断，避免 Token 超限）
func formatMessagesLambda(ctx context.Context, input *SummaryInput, opts ...compose.Option) (map[string]any, error) {
	var builder strings.Builder

	messageCount := 0
	for _, msg := range input.Messages {
		// 过滤系统消息
		if msg.Role == "system" {
			continue
		}

		// 过滤空消息
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		// 截断过长的消息（单条超过 1000 字符时截断）
		if len(content) > 1000 {
			content = content[:1000] + "..."
		}

		messageCount++
		// 格式：[序号] 角色: 内容
		builder.WriteString(fmt.Sprintf("[%d] %s: %s\n", messageCount, msg.Role, content))
	}

	return map[string]any{
		"conversation": builder.String(),
	}, nil
}

// extractSummaryLambda 从 LLM 输出中提取摘要
//
// 输入：*schema.Message（LLM 的回复）
// 输出：*SummaryOutput
//
// 解析规则：
//  1. 支持结构化格式（背景、讨论、结论、备注）
//  2. 支持简单格式（总结：...）
//  3. 容错处理：如果格式不符合预期，尝试智能解析
//
// 容错策略：
//   - 优先解析结构化格式
//   - 如果没有结构化标记，查找"总结："标记
//   - 如果都没有，取第一段非空文本作为总结
func extractSummaryLambda(ctx context.Context, msg *schema.Message, opts ...compose.Option) (*SummaryOutput, error) {
	content := msg.Content
	lines := strings.Split(content, "\n")

	var summary strings.Builder
	hasStructuredFormat := false

	// 检查是否包含结构化格式的标记
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "**背景：**") ||
			strings.HasPrefix(line, "**讨论：**") ||
			strings.HasPrefix(line, "**结论：**") ||
			strings.HasPrefix(line, "**Context:**") ||
			strings.HasPrefix(line, "**Discussion:**") {
			hasStructuredFormat = true
			break
		}
	}

	if hasStructuredFormat {
		// 解析结构化格式：直接使用完整内容（去除示例部分）
		inExample := false
		for _, line := range lines {
			line = strings.TrimSpace(line)

			// 跳过分隔线和示例部分
			if strings.HasPrefix(line, "---") {
				inExample = !inExample
				continue
			}
			if inExample || line == "" {
				continue
			}

			// 跳过标题行
			if strings.HasPrefix(line, "## ") {
				continue
			}

			// 添加有效内容
			if summary.Len() > 0 {
				summary.WriteString("\n")
			}
			summary.WriteString(line)
		}
	} else {
		// 解析简单格式：查找"总结："标记
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// 提取总结部分（支持中英文标记）
			if strings.HasPrefix(line, "总结：") || strings.HasPrefix(line, "总结:") {
				summaryText := strings.TrimPrefix(line, "总结：")
				summaryText = strings.TrimPrefix(summaryText, "总结:")
				summary.WriteString(strings.TrimSpace(summaryText))
				break
			}
			if strings.HasPrefix(line, "Summary:") || strings.HasPrefix(line, "Summary：") {
				summaryText := strings.TrimPrefix(line, "Summary:")
				summaryText = strings.TrimPrefix(summaryText, "Summary：")
				summary.WriteString(strings.TrimSpace(summaryText))
				break
			}
		}
	}

	// 容错处理：如果没有解析到任何内容，取第一段非空文本
	if summary.Len() == 0 {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "##") && !strings.HasPrefix(line, "---") {
				summary.WriteString(line)
				break
			}
		}
	}

	// 最终兜底：如果仍然没有总结，使用默认文本
	summaryText := summary.String()
	if summaryText == "" {
		summaryText = "对话内容已总结"
	}

	return &SummaryOutput{
		Summary: summaryText,
	}, nil
}
