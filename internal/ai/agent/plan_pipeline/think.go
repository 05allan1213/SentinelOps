package plan_pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// ThinkChunkMsg 是思考阶段推送给前端的事件结构，通过 plan_step SSE 事件发送。
type ThinkChunkMsg struct {
	Type    string `json:"type"`    // 固定为 "think"
	Content string `json:"content"` // 思考文本片段（增量）
}

// MarshalThinkChunk 序列化思考片段为 JSON 字符串，失败时直接返回内容原文。
func MarshalThinkChunk(content string) string {
	b, err := json.Marshal(ThinkChunkMsg{Type: "think", Content: content})
	if err != nil {
		return content
	}
	return string(b)
}

// StreamThinkChunks 调用 Qwen3.7 Max reasoning profile 模型对用户查询进行预思考，
// 实时流式推送推理内容，每个文本片段触发一次 onChunk 回调。
//
// 支持三种模式（自动检测）：
//   - reasoning 模式：模型通过 ReasoningContent 字段输出推理（如 reasoning-capable model API）
//   - think 模式：模型在文本中输出 <think>...</think> 标签
//   - plain 模式：普通文本输出（如 Qwen3.7 Max），直接实时流式推送
//
// 错误不中断主流程：调用方应忽略返回值，继续执行 Plan Agent。
func StreamThinkChunks(ctx context.Context, query string, onChunk func(string)) error {
	cm, err := models.ChatReasoning(ctx)
	if err != nil {
		return fmt.Errorf("初始化 reasoning Profile: %w", err)
	}

	msgs := []*schema.Message{
		schema.SystemMessage(agents.ThinkingSystem),
		schema.UserMessage(query),
	}

	stream, err := cm.Stream(ctx, msgs)
	if err != nil {
		return fmt.Errorf("reasoning Profile 调用失败: %w", err)
	}
	defer stream.Close()

	// mode 自动检测：
	//   ""          — 检测中（等待足够内容判断模型类型）
	//   "reasoning" — ReasoningContent 字段模式（reasoning-capable model API）
	//   "think"     — <think> 标签模式
	//   "plain"     — 普通文本，直接实时推送
	var (
		mode       string
		buf        strings.Builder
		thinkStart int
		lastPushed int
	)

	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			g.Log().Warningf(ctx, "[StreamThink] 流读取中断: %v", recvErr)
			break
		}
		if msg == nil {
			continue
		}

		// ── ReasoningContent 模式：直接实时推送 ──────────────────────────
		if msg.ReasoningContent != "" {
			if mode == "" {
				mode = "reasoning"
			}
		}
		if mode == "reasoning" {
			if msg.ReasoningContent != "" {
				onChunk(msg.ReasoningContent)
			}
			continue
		}

		chunk := msg.Content
		if chunk == "" {
			continue
		}
		buf.WriteString(chunk)
		cur := buf.String()

		// ── 检测阶段：判断是 <think> 标签还是普通文本 ───────────────────
		if mode == "" {
			if i := strings.Index(cur, "<think>"); i >= 0 {
				// 检测到 <think> 标签，切换到 think 过滤模式
				mode = "think"
				thinkStart = i + len("<think>")
				lastPushed = 0
			} else if len([]rune(cur)) >= 15 {
				// 15 字后仍无 <think> 标签 → 普通文本模式，推送已缓冲内容后实时流式
				mode = "plain"
				onChunk(cur)
				lastPushed = buf.Len()
			}
			// 仍在检测中，继续缓冲
			continue
		}

		// ── 普通文本模式：每个 chunk 实时推送 ───────────────────────────
		if mode == "plain" {
			onChunk(chunk)
			lastPushed = buf.Len()
			continue
		}

		// ── <think> 标签模式：仅推送标签内内容 ─────────────────────────
		if mode == "think" {
			thinkBody := cur[thinkStart:]
			endIdx := strings.Index(thinkBody, "</think>")
			done := endIdx >= 0
			if done {
				thinkBody = thinkBody[:endIdx]
			}
			if len(thinkBody) > lastPushed {
				onChunk(thinkBody[lastPushed:])
				lastPushed = len(thinkBody)
			}
			if done {
				break
			}
		}
	}

	// 兜底：检测阶段结束时仍有未推送的缓冲内容
	if mode == "" && buf.Len() > 0 {
		onChunk(buf.String())
	}

	return nil
}
