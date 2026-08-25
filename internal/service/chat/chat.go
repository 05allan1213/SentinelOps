// chat.go 对话核心业务逻辑：标准意图路由 + 深度思考模式（Plan Agent）。
package chatsvc

import (
	"context"
	"strings"
	"time"

	"SentinelOps/internal/ai/agent/plan_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent"
	"SentinelOps/internal/ai/intent/core"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

const maxThinkTimeout = 30 * time.Second

func normalizeThinkTimeout(parentBudget time.Duration) time.Duration {
	if parentBudget <= 0 {
		return maxThinkTimeout
	}
	if parentBudget < maxThinkTimeout {
		return parentBudget
	}
	return maxThinkTimeout
}

func deadlineFromContext(ctx context.Context) time.Time {
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Now().Add(maxThinkTimeout)
	}
	return deadline
}

// ExecuteIntent 标准意图路由。
// Router 只在 chat / event / report / risk / solve 5 类意图中识别。
func ExecuteIntent(ctx context.Context, sessionId, query string, messageIndex int, onOutput func(intentType, chunk string)) error {
	g.Log().Infof(ctx, "[Intent] 收到请求 | session=%s | query=%q", sessionId, query)

	ig := intent.NewIntent(ctx, sessionId, messageIndex)
	var assistantOutput strings.Builder
	_, err := ig.Execute(query, func(intentType intent.IntentType, chunk string) {
		onOutput(string(intentType), chunk)
		if chunk == "" {
			return
		}
		if intentType != core.IntentStatus {
			assistantOutput.WriteString(chunk)
		}
	})

	if err != nil {
		g.Log().Errorf(ctx, "[Intent] 执行失败 | session=%s | err=%v", sessionId, err)
		return err
	}
	return nil
}

// ExecuteDeepThink 深度思考模式对话。
// 直接调用 Plan Agent（Supervisor-Worker 架构），跳过意图识别路由层，无 LLM 路由开销。
//
// sessionId 注入机制：
//
//	context.WithValue(ctx, SessionIdCtxKey{}, sessionId) 将 sessionId 嵌入 context，
//	随后整个调用树（BuildPlanAgent → adk.Runner → Executor → AgentTool）共享同一个 ctx。
//	AgentTool 的薄 Agent 委托通过 ctx.Value(SessionIdCtxKey{}) 取出 sessionId，
//	将 SessionMemory 历史作为 ADK messages 注入一次后调用真实专业 ChatModelAgent。
//	中间层不隔离 context，Eino state、interrupt 和 recursive cancel 可按官方协议传播。
func ExecuteDeepThink(ctx context.Context, sessionId, query string, messageIndex int, onOutput func(intentType, chunk string)) error {
	g.Log().Infof(ctx, "[Intent] 深度思考请求 | session=%s | query=%q", sessionId, query)

	// 注入 sessionId：AgentTool 委托读取会话历史并作为 ADK messages 注入专业 Agent。
	// 传递路径：此处注入 → BuildPlanAgent → adk.Runner.Query → Executor → AgentTool → workerAgentInput
	recCtx := context.WithValue(ctx, plan_pipeline.SessionIdCtxKey{}, sessionId)

	// 加载并初始化会话记忆；messageIndex==0 表示新会话第一条消息，强制清空历史
	mem := cache.GetSessionMemory(sessionId)
	if messageIndex <= 1 {
		mem.SetState([]*schema.Message{}, "")
	} else {
		recent, summary, err := cache.LoadSession(recCtx, sessionId)
		if err == nil && (len(recent) > 0 || summary != "") {
			mem.SetState(recent, summary)
		} else if err != nil {
			g.Log().Warningf(ctx, "[Intent] 深度思考加载会话失败，使用进程内存 | session=%s | err=%v", sessionId, err)
		}
	}
	// ── 问题二：多路调用内部回滚 ─────────────────────────────────────────────
	// 问题根因：
	//   ExecuteDeepThink 在调用 Plan Agent 之前已将 UserMessage 写入 SessionMemory，
	//   若 Plan Agent 执行失败（LLM 超时、Worker 报错等），该 UserMessage 仍残留在
	//   进程内存和 Redis 中，导致下一轮对话的历史上下文包含一条"孤立的用户消息"
	//   （没有对应的 AssistantMessage），破坏 user/assistant 交替的消息结构，
	//   后续 LLM 调用可能因消息格式非法而报错或产生错误推理。
	//
	// 设计思路：
	//   在写入 UserMessage 前记录当前消息数 preWriteLen，作为回滚游标。
	//   失败时调用 RollbackSession(ctx, sessionId, preWriteLen-1)，
	//   该函数同时回滚 Redis（LoadSession → 截断 → SaveSession）和
	//   进程内 SessionMemory（mem.SetState），保证两层存储一致。
	//   若写入前消息为空（preWriteLen==0），直接 SetState 清空，避免 index=-1 越界。
	// 记录写入 UserMessage 前的消息数，失败时用于回滚
	preWriteLen := len(mem.GetRecentMessages())
	mem.SetMessages(schema.UserMessage(query))

	// 阶段一：预思考（流式推送 think 事件，错误不中断主流程）
	// 预思考只用于增强用户可见性，不应吞掉正式 Plan Agent 的总预算。
	onOutput(string(core.IntentStatus), "深度思考中...")
	thinkCtx, thinkCancel := context.WithTimeout(recCtx, normalizeThinkTimeout(time.Until(deadlineFromContext(recCtx))))
	thinkErr := plan_pipeline.StreamThinkChunks(thinkCtx, query, func(chunk string) {
		onOutput(string(core.IntentPlanStep), plan_pipeline.MarshalThinkChunk(chunk))
	})
	thinkCancel()
	if thinkErr != nil {
		g.Log().Warningf(ctx, "[Intent] 预思考阶段失败，继续执行 Plan Agent | session=%s | err=%v", sessionId, thinkErr)
	}

	// 阶段二：Plan Agent（任务规划 + 执行）
	onOutput(string(core.IntentStatus), "[Plan Agent 规划执行...]")
	content, execErr := plan_pipeline.BuildPlanAgent(recCtx, query,
		func(chunk string) { onOutput(string(core.IntentPlanStep), chunk) }, // 中间步骤 → 规划过程块
		func(chunk string) { onOutput(string(core.IntentPlan), chunk) },     // 最终答案 → 正式内容
	)
	if execErr != nil {
		// 回滚本轮写入的 UserMessage，恢复到写入前的状态
		if rollbackIdx := preWriteLen - 1; rollbackIdx >= 0 {
			if _, rbErr := RollbackSession(ctx, sessionId, rollbackIdx); rbErr != nil {
				g.Log().Warningf(ctx, "[Intent] 深度思考失败后回滚会话失败 | session=%s | err=%v", sessionId, rbErr)
			}
		} else {
			// 写入前为空，直接清空
			mem.SetState([]*schema.Message{}, mem.GetLongTermSummary())
		}
		g.Log().Errorf(ctx, "[Intent] 深度思考执行失败 | session=%s | err=%v", sessionId, execErr)
		return execErr
	}

	// 将助手回复写入会话记忆并异步持久化到 Redis
	if content != "" {
		mem.SetMessages(schema.AssistantMessage(content, nil))
		go func() {
			bgCtx := context.Background()
			if persistErr := cache.SaveSessionWithRetry(bgCtx, sessionId,
				mem.GetRecentMessages(), mem.GetLongTermSummary()); persistErr != nil {
				g.Log().Errorf(bgCtx, "[Intent] 深度思考保存会话失败（已重试） | session=%s | err=%v", sessionId, persistErr)
			}
		}()
	}

	return nil
}
