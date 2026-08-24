// Package rewrite 查询重写服务：利用 LLM 将口语化/代词式查询改写为向量检索友好的独立查询。
// 任何失败均静默回退到原始查询，不影响主流程。
package rewrite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/rag"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// RewriteQuery 将用户查询 + 对话历史改写为向量检索友好的独立查询。
// 消除代词歧义、补全时间上下文，保留领域术语原文。
// 若改写失败，静默回退到原始查询。
func RewriteQuery(ctx context.Context, query string, history []*schema.Message) string {
	m, err := models.ChatDefault(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "[Rewrite] 模型初始化失败，使用原始查询: %v", err)
		return query
	}

	resp, err := m.Generate(ctx, buildRewriteMessages(query, history))
	if err != nil || resp.Content == "" {
		g.Log().Warningf(ctx, "[Rewrite] 查询改写失败，使用原始查询: %v", err)
		return query
	}

	rewritten := strings.TrimSpace(resp.Content)
	if len(rewritten) < 3 || len(rewritten) > 500 {
		return query
	}

	g.Log().Debugf(ctx, "[Rewrite] 改写完成 | 原始=%q | 改写=%q", query, rewritten)
	return rewritten
}

// buildRewriteMessages 构建查询改写的消息列表，注入最近 3 轮对话上下文。
func buildRewriteMessages(query string, history []*schema.Message) []*schema.Message {
	messages := []*schema.Message{
		schema.SystemMessage(rag.RewriteQuery),
	}

	if len(history) > 0 {
		last := history
		if len(last) > 6 {
			last = history[len(history)-6:]
		}
		if ctxStr := buildHistoryContext(last); ctxStr != "" {
			messages = append(messages,
				schema.UserMessage(fmt.Sprintf("<history>\n%s\n</history>", ctxStr)),
				schema.AssistantMessage("已理解上下文，请提供需要改写的查询。", nil),
			)
		}
	}

	messages = append(messages, schema.UserMessage(query))
	return messages
}

// buildHistoryContext 将历史消息格式化为可读文本。
func buildHistoryContext(history []*schema.Message) string {
	var parts []string
	for _, msg := range history {
		if msg.Content == "" {
			continue
		}
		role := "用户"
		if msg.Role == schema.Assistant {
			role = "助手"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", role, msg.Content))
	}
	return strings.Join(parts, "\n")
}

// RewriteAndSplit 单次 LLM 调用，同时完成查询改写和子问题拆分。
// 返回改写后的查询和子查询列表（至少包含一个子查询）。
// 兜底：任何失败均返回 (query, []string{query})，不影响主流程。
func RewriteAndSplit(ctx context.Context, query string, history []*schema.Message) (rewritten string, subQueries []string) {
	m, err := models.ChatDefault(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "[RewriteAndSplit] 模型初始化失败，使用原始查询: %v", err)
		return query, []string{query}
	}

	resp, err := m.Generate(ctx, buildRewriteAndSplitMessages(query, history))
	if err != nil || resp.Content == "" {
		g.Log().Warningf(ctx, "[RewriteAndSplit] LLM 调用失败，使用原始查询: %v", err)
		return query, []string{query}
	}

	var result struct {
		Rewrite      string   `json:"rewrite"`
		SubQuestions []string `json:"sub_questions"`
	}
	raw := strings.TrimSpace(resp.Content)
	// 去除可能的 markdown 代码块包裹
	if strings.HasPrefix(raw, "```") {
		if idx := strings.Index(raw, "\n"); idx >= 0 {
			raw = raw[idx+1:]
		}
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
		raw = strings.TrimSpace(raw)
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		g.Log().Debugf(ctx, "[RewriteAndSplit] JSON 解析失败，使用原始查询 | raw=%q", resp.Content)
		return query, []string{query}
	}

	rewritten = strings.TrimSpace(result.Rewrite)
	if len(rewritten) < 3 {
		rewritten = query
	}

	var subQs []string
	for _, q := range result.SubQuestions {
		if q = strings.TrimSpace(q); q != "" {
			subQs = append(subQs, q)
		}
	}
	if len(subQs) == 0 {
		subQs = []string{rewritten}
	}

	g.Log().Debugf(ctx, "[RewriteAndSplit] 完成 | 原始=%q | 改写=%q | 子问题数=%d", query, rewritten, len(subQs))
	return rewritten, subQs
}

// buildRewriteAndSplitMessages 构建合并改写+拆分的消息列表。
func buildRewriteAndSplitMessages(query string, history []*schema.Message) []*schema.Message {
	messages := []*schema.Message{
		schema.SystemMessage(rag.RewriteAndSplit),
	}

	if len(history) > 0 {
		last := history
		if len(last) > 6 {
			last = history[len(history)-6:]
		}
		if ctxStr := buildHistoryContext(last); ctxStr != "" {
			messages = append(messages,
				schema.UserMessage(fmt.Sprintf("<history>\n%s\n</history>", ctxStr)),
				schema.AssistantMessage("已理解上下文，请提供需要处理的查询。", nil),
			)
		}
	}

	messages = append(messages, schema.UserMessage(query))
	return messages
}
