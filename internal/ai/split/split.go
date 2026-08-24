// Package split 子问题拆分服务：检测多维查询并分解为独立子查询，再对每个子查询独立检索后聚合。
// 任何失败均静默回退到原始查询，不影响主流程。
package split

import (
	"context"
	"encoding/json"
	"strings"

	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/rag"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// SplitQuestions 检测复杂查询并分解为独立子查询（最多 3 个）。
// 若是单一问题或调用失败，返回 []string{query}。
func SplitQuestions(ctx context.Context, query string) []string {
	m, err := models.ChatDefault(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "[Split] 模型初始化失败，使用原始查询: %v", err)
		return []string{query}
	}

	resp, err := m.Generate(ctx, buildSplitMessages(query))
	if err != nil || resp.Content == "" {
		g.Log().Warningf(ctx, "[Split] 子问题拆分失败，使用原始查询: %v", err)
		return []string{query}
	}

	var result struct {
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &result); err != nil {
		g.Log().Debugf(ctx, "[Split] JSON 解析失败，使用原始查询 | raw=%q", resp.Content)
		return []string{query}
	}

	var questions []string
	for _, q := range result.Questions {
		if q = strings.TrimSpace(q); q != "" {
			questions = append(questions, q)
		}
	}
	if len(questions) == 0 {
		return []string{query}
	}

	g.Log().Debugf(ctx, "[Split] 拆分完成 | 原始=%q | 子问题数=%d", query, len(questions))
	return questions
}

// buildSplitMessages 构建子问题拆分的消息列表。
func buildSplitMessages(query string) []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage(rag.Split),
		schema.UserMessage(query),
	}
}
