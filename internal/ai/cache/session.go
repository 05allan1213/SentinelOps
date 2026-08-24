// Package cache 提供会话记忆和语义缓存功能
//
// 会话记忆（session.go）：管理对话历史的持久化，支持最近消息和长期摘要的分离存储
// 语义缓存（semantic.go）：基于向量相似度的检索结果缓存，减少重复的 Embedding 和 Milvus 调用
package cache

import (
	"context"
	"fmt"
	"time"

	aitrace "SentinelOps/internal/ai/trace"
	redisdao "SentinelOps/internal/dao/redis"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// LoadSession 从 Redis 加载指定会话的记忆状态
//
// 返回值：
//   - recent: 最近的消息列表（短期记忆）
//   - summary: 历史对话摘要（长期记忆）
//   - err: 加载失败时的错误信息
//
// 追踪：记录 CACHE 节点，包含消息数量和摘要状态
func LoadSession(ctx context.Context, sessionID string) (recent []*schema.Message, summary string, err error) {
	spanCtx, spanID := aitrace.StartSpan(ctx, aitrace.NodeTypeCache, "SESSION_LOAD")
	recent, summary, err = redisdao.LoadSession(spanCtx, sessionID)
	aitrace.FinishSpan(spanCtx, spanID, err, map[string]any{
		"op":           "LOAD",
		"recent_count": len(recent),
		"has_summary":  summary != "",
	})
	return recent, summary, err
}

// SaveSession 将会话记忆状态保存到 Redis
//
// 参数：
//   - sessionID: 会话唯一标识
//   - recent: 最近的消息列表（短期记忆）
//   - summary: 历史对话摘要（长期记忆）
//
// 优化：数据为空时跳过保存，避免无效的 Redis 写入
// 追踪：记录 CACHE 节点，包含消息数量
func SaveSession(ctx context.Context, sessionID string, recent []*schema.Message, summary string) error {
	if len(recent) == 0 && summary == "" {
		g.Log().Debugf(ctx, "[Session] 数据为空，跳过保存 | session=%s", sessionID)
		return nil
	}

	spanCtx, spanID := aitrace.StartSpan(ctx, aitrace.NodeTypeCache, "SESSION_SAVE")
	err := redisdao.SaveSession(spanCtx, sessionID, recent, summary)
	aitrace.FinishSpan(spanCtx, spanID, err, map[string]any{
		"op":           "SAVE",
		"recent_count": len(recent),
	})
	return err
}

// SaveRecentMessages 仅保存最近消息列表
//
// 用途：摘要未变化时，只更新最近消息，避免重复写入摘要
// 优化：消息列表为空时跳过保存
func SaveRecentMessages(ctx context.Context, sessionID string, recent []*schema.Message) error {
	if len(recent) == 0 {
		g.Log().Debugf(ctx, "[Session] 消息列表为空，跳过保存 | session=%s", sessionID)
		return nil
	}
	// 先加载已有摘要，避免覆盖为空字符串
	_, summary, err := redisdao.LoadSession(ctx, sessionID)
	if err != nil {
		summary = ""
	}
	return redisdao.SaveSession(ctx, sessionID, recent, summary)
}

// SaveSummary 仅保存长期摘要
//
// 用途：Summary Agent 压缩历史后，只更新摘要，避免重复写入消息列表
// 优化：摘要为空时跳过保存
func SaveSummary(ctx context.Context, sessionID string, summary string) error {
	if summary == "" {
		g.Log().Debugf(ctx, "[Session] 摘要为空，跳过保存 | session=%s", sessionID)
		return nil
	}
	return redisdao.SaveSession(ctx, sessionID, nil, summary)
}

// SaveSessionWithRetry 带重试的会话保存（最多重试 3 次，指数退避）
//
// 重试策略：
//   - 最大重试次数：3 次
//   - 退避时间：1s, 2s, 4s（指数增长）
//   - 失败处理：记录错误日志，返回最后一次错误
//
// 用途：网络不稳定或 Redis 短暂不可用时，提高保存成功率
func SaveSessionWithRetry(ctx context.Context, sessionID string, recent []*schema.Message, summary string) error {
	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		err := SaveSession(ctx, sessionID, recent, summary)
		if err == nil {
			if i > 0 {
				g.Log().Infof(ctx, "[Session] 重试保存成功 | session=%s | retry=%d", sessionID, i)
			}
			return nil
		}

		lastErr = err
		if i < maxRetries-1 {
			backoff := time.Duration(1<<uint(i)) * time.Second // 退避间隔：1s、2s、4s
			g.Log().Warningf(ctx, "[Session] 保存失败，%v 后重试 | session=%s | retry=%d/%d | err=%v",
				backoff, sessionID, i+1, maxRetries, err)
			time.Sleep(backoff)
		}
	}

	g.Log().Errorf(ctx, "[Session] 保存失败，已达最大重试次数 | session=%s | err=%v", sessionID, lastErr)
	return fmt.Errorf("save session after %d retries: %w", maxRetries, lastErr)
}
