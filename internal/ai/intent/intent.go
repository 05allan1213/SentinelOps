// Package intent intent.go 意图调度器：封装标准模式的 Router→Executor 流程，管理 cache.SessionMemory 的消息读写。
//
// 使用场景：仅用于 deep_thinking=false（标准模式）。
//   - deep_thinking=true（深度思考模式）由 service/chat.ExecuteIntentDeepThink 直接调用 plan_pipeline，不经过此调度器。
//
// controller 层只需调用 NewIntent(ctx, sessionId) + Execute，无需感知 Graph、Router、Executor 等内部细节。
package intent

import (
	"context"

	"SentinelOps/internal/ai/cache"
	_ "SentinelOps/internal/ai/intent/subagents"
	"SentinelOps/internal/ai/memory"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// Intent 意图调度器，对外暴露 Execute 方法，内部驱动 Router→Executor DAG 流程。
// 持有 cache.SessionMemory 引用，按 sessionId 隔离会话，并通过 Redis 持久化对话历史。
type Intent struct {
	ctx       context.Context
	sessionId string
	memory    *cache.SessionMemory
}

// NewIntent 创建 Intent 实例，绑定 context 与按 sessionId 隔离的 SessionMemory。
// 启动时尝试从 Redis 恢复历史状态，Redis 不可用时静默降级（仅使用进程内存）。
func NewIntent(ctx context.Context, sessionId string, messageIndex int) *Intent {
	mem := cache.GetSessionMemory(sessionId)

	if messageIndex <= 1 {
		// 新会话第一条消息，强制清空历史，不加载 Redis
		mem.SetState([]*schema.Message{}, "")
	} else {
		// 从 Redis 恢复历史状态（短期消息 + 长期摘要）
		recent, summary, err := cache.LoadSession(ctx, sessionId)
		if err == nil && (len(recent) > 0 || summary != "") {
			mem.SetState(recent, summary)
		} else if err != nil {
			g.Log().Warningf(ctx, "[Intent] 从 Redis 恢复会话状态失败，降级为进程内存 | session=%s | err=%v", sessionId, err)
		}
	}

	return &Intent{ctx: ctx, sessionId: sessionId, memory: mem}
}

// Execute 执行完整意图调度流程：
//  1. 将用户 query 写入 SessionMemory（提供历史上下文给 SubAgent）
//  2. 构建并编译 IntentGraph（每次新建，隔离请求状态）
//  3. Invoke DAG：Router 识别意图 → Executor 分发并执行 SubAgent
//  4. 将助手回复写入 SessionMemory，异步保存到 Redis
//
// callback 为流式回调，可传 nil；返回 Result 包含 TaskID、Intent、Content、Error。
func (s *Intent) Execute(query string, callback StreamCallback) (*Result, error) {
	s.memory.SetMessages(schema.UserMessage(query))

	graph, err := BuildIntentGraph(s.ctx)
	if err != nil {
		return &Result{Error: err}, err
	}

	input := &IntentInput{
		Query:     query,
		SessionId: s.sessionId,
		Callback:  callback,
	}

	output, err := graph.Invoke(s.ctx, input)
	if err != nil {
		return &Result{Error: err}, err
	}

	if output.Content != "" {
		s.memory.SetMessages(schema.AssistantMessage(output.Content, nil))

		// 异步保存最新状态到 Redis，不阻塞主链路
		msgs := s.memory.GetRecentMessages()
		summary := s.memory.GetLongTermSummary()
		userID, _ := s.ctx.Value(memory.UserIdCtxKey{}).(string)
		go func() {
			bgCtx := context.Background()
			if persistErr := cache.SaveSessionWithRetry(bgCtx, s.sessionId, msgs, summary); persistErr != nil {
				g.Log().Errorf(bgCtx, "[Intent] 保存会话状态失败（已重试） | session=%s | err=%v", s.sessionId, persistErr)
			}
			// 消息数 >= 6 时触发隐式偏好推断；canInfer 内部 1 小时防抖，无需在此计数
			if userID != "" && len(msgs) >= 6 {
				memory.TriggerInferPreference(bgCtx, userID, msgs)
			}
		}()
	}

	return &Result{
		TaskID:  output.TaskID,
		Intent:  output.Intent,
		Content: output.Content,
		Error:   output.Error,
	}, output.Error
}
