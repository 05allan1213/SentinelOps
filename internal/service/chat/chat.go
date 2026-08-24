// chat.go 对话核心业务逻辑：标准意图路由 + 深度思考模式（Plan Agent）。
package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/agent/plan_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/intent"
	"SentinelOps/internal/ai/intent/core"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const chatWorkflowKey = "chat.intent"

const maxThinkTimeout = 30 * time.Second

type workflowRunCursorCtxKey struct{}

// WithWorkflowRunCursor 将 controller 创建的 workflow run id 和已持久化序号注入 context，供 service 复用同一条运行记录。
func WithWorkflowRunCursor(ctx context.Context, runID string, seq int64) context.Context {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, workflowRunCursorCtxKey{}, workflowRunCursor{runID: runID, seq: seq})
}

type workflowRunCursor struct {
	runID string
	seq   int64
}

func workflowRunCursorFromContext(ctx context.Context) (string, int64) {
	cursor, _ := ctx.Value(workflowRunCursorCtxKey{}).(workflowRunCursor)
	return strings.TrimSpace(cursor.runID), cursor.seq
}

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

// chatWorkflowRun 封装一次聊天请求的工作流生命周期，负责串联事件、检查点和最终状态。
type chatWorkflowRun struct {
	store        workflow.Store
	runID        string
	workflowKey  string
	sessionID    string
	query        string
	deepThinking bool
	startedAt    time.Time
	seq          int64
	enabled      bool
}

// resolveChatWorkflowStore 尽力初始化工作流存储；失败时返回 nil，不影响主对话流程。
func resolveChatWorkflowStore(ctx context.Context) workflow.Store {
	defer func() {
		if r := recover(); r != nil {
			g.Log().Warningf(ctx, "[Chat] 初始化 workflow store 发生 panic，降级为非持久化工作流 | err=%v", r)
		}
	}()

	db, err := mysql.DB(ctx)
	if err != nil || db == nil {
		if err != nil {
			g.Log().Warningf(ctx, "[Chat] 初始化 workflow store 失败，降级为非持久化工作流 | err=%v", err)
		}
		return nil
	}
	return workflow.NewGORMStore(db)
}

// findLatestRunningWorkflowRun 尽量复用 controller 已创建的运行，避免重复写入 workflow_runs。
func findLatestRunningWorkflowRun(ctx context.Context, db *gorm.DB, sessionID, workflowKey string) (string, int64, bool) {
	var run mysql.WorkflowRun
	err := db.WithContext(ctx).
		Where("session_id = ? AND workflow_key = ? AND status = ?", sessionID, workflowKey, workflow.RunStatusRunning).
		Order("started_at DESC").
		First(&run).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			g.Log().Warningf(ctx, "[Chat] 查询最近运行中的 workflow run 失败 | session=%s | err=%v", sessionID, err)
		}
		return "", 0, false
	}

	var event mysql.WorkflowEvent
	seq := int64(0)
	if err := db.WithContext(ctx).
		Where("run_id = ?", run.ID).
		Order("seq DESC").
		First(&event).Error; err == nil {
		seq = int64(event.Seq)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		g.Log().Warningf(ctx, "[Chat] 查询 workflow 事件序号失败 | run_id=%s | err=%v", run.ID, err)
	}
	return run.ID, seq, true
}

// marshalWorkflowPayload 将结构化内容转成稳定的字符串，便于写入 workflow_runs 的 JSON 字段。
func marshalWorkflowPayload(payload any) string {
	if payload == nil {
		return ""
	}
	if text, ok := payload.(string); ok {
		return text
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprint(payload)
	}
	return string(data)
}

// workflowPersistContext 为工作流持久化构造隔离取消但保留截止时间的上下文。
// HTTP 客户端断连会取消 request context；若直接透传到 GORM，SSE 事件与检查点落库会收到 context canceled。
func workflowPersistContext(ctx context.Context) context.Context {
	persistCtx := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		persistCtx, cancel = context.WithDeadline(persistCtx, deadline)
		_ = cancel
	}
	return persistCtx
}

// newChatWorkflowRun 创建或复用一次聊天工作流运行；如果存储不可用，则返回禁用状态的空壳对象。
func newChatWorkflowRun(ctx context.Context, sessionID, query string, deepThinking bool) *chatWorkflowRun {
	r := &chatWorkflowRun{
		runID:        uuid.NewString(),
		workflowKey:  chatWorkflowKey,
		sessionID:    sessionID,
		query:        query,
		deepThinking: deepThinking,
		startedAt:    time.Now(),
	}

	store := resolveChatWorkflowStore(ctx)
	if store == nil {
		return r
	}

	db, err := mysql.DB(ctx)
	if err != nil || db == nil {
		if err != nil {
			g.Log().Warningf(ctx, "[Chat] 获取 workflow 数据库失败，降级为非持久化工作流 | session=%s | err=%v", sessionID, err)
		}
		return r
	}

	if existingRunID, lastSeq, found := findLatestRunningWorkflowRun(ctx, db, sessionID, r.workflowKey); found {
		r.runID = existingRunID
		r.seq = lastSeq
		r.store = store
		r.enabled = true
		return r
	}

	run, err := store.CreateRun(ctx, workflow.WorkflowRunInput{
		ID:          r.runID,
		WorkflowKey: r.workflowKey,
		SessionID:   sessionID,
		Status:      workflow.RunStatusRunning,
		InputPayload: marshalWorkflowPayload(map[string]any{
			"sessionId":    sessionID,
			"query":        query,
			"deepThinking": deepThinking,
		}),
		StartedAt: r.startedAt,
	})
	if err != nil {
		g.Log().Warningf(ctx, "[Chat] 创建 workflow run 失败，降级为非持久化工作流 | session=%s | err=%v", sessionID, err)
		return r
	}

	r.runID = run.ID
	r.store = store
	r.enabled = true
	return r
}

// appendEvent 追加一条工作流事件；存储不可用时静默跳过。
func (r *chatWorkflowRun) appendEvent(ctx context.Context, eventType string, payload any) {
	if r == nil || !r.enabled || r.store == nil {
		return
	}
	event := workflow.BuildNextEvent(r.runID, r.seq, eventType, marshalWorkflowPayload(payload))
	r.seq = event.ID
	if err := r.store.AppendEvent(workflowPersistContext(ctx), event); err != nil {
		g.Log().Warningf(ctx, "[Chat] 追加 workflow 事件失败 | run_id=%s | seq=%d | type=%s | err=%v", r.runID, event.ID, eventType, err)
	}
}

// saveCheckpoint 保存工作流检查点；失败时仅记录日志，不中断对话。
func (r *chatWorkflowRun) saveCheckpoint(ctx context.Context, step string, values map[string]any) {
	if r == nil || !r.enabled || r.store == nil {
		return
	}
	if err := r.store.SaveCheckpoint(workflowPersistContext(ctx), workflow.CheckpointSnapshot{
		RunID:        r.runID,
		CheckpointID: step,
		Step:         step,
		State:        values,
		CreatedAt:    time.Now(),
	}); err != nil {
		g.Log().Warningf(ctx, "[Chat] 保存 workflow checkpoint 失败 | run_id=%s | step=%s | err=%v", r.runID, step, err)
	}
}

// finish 结束工作流运行；如果 store 不可用，则保持原有对话流程不受影响。
func (r *chatWorkflowRun) finish(ctx context.Context, status, outputPayload, errorMessage string) {
	if r == nil || !r.enabled || r.store == nil {
		return
	}
	if err := r.store.FinishRun(workflowPersistContext(ctx), r.runID, status, outputPayload, errorMessage); err != nil {
		g.Log().Warningf(ctx, "[Chat] 完成 workflow run 失败 | run_id=%s | err=%v", r.runID, err)
	}
}

// recordUserInput 记录用户输入事件与检查点，串起会话起点和后续回复。
func (r *chatWorkflowRun) recordUserInput(ctx context.Context, query string) {
	r.appendEvent(ctx, "workflow.user_message", map[string]any{
		"sessionId":    r.sessionID,
		"query":        query,
		"deepThinking": r.deepThinking,
	})
	r.saveCheckpoint(ctx, "user_input", map[string]any{
		"sessionId":    r.sessionID,
		"query":        query,
		"deepThinking": r.deepThinking,
	})
}

// recordAssistantOutput 记录助手最终回复与检查点。
func (r *chatWorkflowRun) recordAssistantOutput(ctx context.Context, output string) {
	if strings.TrimSpace(output) == "" {
		return
	}
	r.appendEvent(ctx, "workflow.assistant_reply", map[string]any{
		"sessionId": r.sessionID,
		"output":    output,
	})
	r.saveCheckpoint(ctx, "assistant_reply", map[string]any{
		"sessionId": r.sessionID,
		"output":    output,
	})
}

// recordRunStatus 记录运行状态，便于后续分析成功、失败与回退场景。
func (r *chatWorkflowRun) recordRunStatus(ctx context.Context, status, message string) {
	r.appendEvent(ctx, "workflow.status", map[string]any{
		"status":  status,
		"message": message,
	})
}

// Event 实现 plan_pipeline.WorkflowRecorder 接口，用于记录 Plan Agent 的流式事件。
func (r *chatWorkflowRun) Event(ctx context.Context, eventType, payload string) error {
	r.appendEvent(ctx, eventType, payload)
	return nil
}

// Checkpoint 实现 plan_pipeline.WorkflowRecorder 接口，用于记录 Plan Agent 的检查点。
func (r *chatWorkflowRun) Checkpoint(ctx context.Context, stepName string, values map[string]any) error {
	r.saveCheckpoint(ctx, stepName, values)
	return nil
}

// ExecuteIntent 标准意图路由。
// Router 只在 chat / event / report / risk / solve 5 类意图中识别。
func ExecuteIntent(ctx context.Context, sessionId, query string, messageIndex int, onOutput func(intentType, chunk string)) error {
	g.Log().Infof(ctx, "[Intent] 收到请求 | session=%s | query=%q", sessionId, query)

	workflowRun := newChatWorkflowRun(ctx, sessionId, query, false)
	recCtx := plan_pipeline.WithWorkflowRecorder(ctx, workflowRun)
	workflowRun.recordUserInput(ctx, query)
	workflowRun.recordRunStatus(ctx, "running", "标准意图路由执行中")

	ig := intent.NewIntent(recCtx, sessionId, messageIndex)
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

	output := assistantOutput.String()
	if err != nil {
		workflowRun.appendEvent(ctx, "workflow.error", map[string]any{
			"sessionId": sessionId,
			"error":     err.Error(),
		})
		workflowRun.saveCheckpoint(ctx, "failed", map[string]any{
			"sessionId": sessionId,
			"error":     err.Error(),
			"query":     query,
		})
		workflowRun.finish(ctx, workflow.RunStatusFailed, output, err.Error())
		g.Log().Errorf(ctx, "[Intent] 执行失败 | session=%s | err=%v", sessionId, err)
		return err
	}

	workflowRun.recordAssistantOutput(ctx, output)
	workflowRun.saveCheckpoint(ctx, "completed", map[string]any{
		"sessionId": sessionId,
		"query":     query,
		"output":    output,
	})
	workflowRun.recordRunStatus(ctx, workflow.RunStatusSuccess, "标准意图路由执行完成")
	workflowRun.finish(ctx, workflow.RunStatusSuccess, output, "")
	return nil
}

// ExecuteDeepThink 深度思考模式对话。
// 直接调用 Plan Agent（Supervisor-Worker 架构），跳过意图识别路由层，无 LLM 路由开销。
//
// sessionId 注入机制：
//
//	context.WithValue(ctx, SessionIdCtxKey{}, sessionId) 将 sessionId 嵌入 context，
//	随后整个调用树（BuildPlanAgent → adk.Runner → Executor → Worker 工具 Invoke）共享同一个 ctx。
//	Worker 工具（agent_worker.go）在 Invoke 时通过 ctx.Value(SessionIdCtxKey{}) 取出 sessionId，
//	调用 cache.GetSessionMemory(sessionId) 读取进程内会话历史（零 Redis 开销），
//	将最近 3 条历史拼成上下文前缀注入给专业 Agent，使 Worker 感知多轮对话上下文。
//	这是 Go 惯用的跨层透明传递方式，中间层（BuildPlanAgent、adk.Runner）无需感知。
func ExecuteDeepThink(ctx context.Context, sessionId, query string, messageIndex int, onOutput func(intentType, chunk string)) error {
	g.Log().Infof(ctx, "[Intent] 深度思考请求 | session=%s | query=%q", sessionId, query)

	workflowRun := newChatWorkflowRun(ctx, sessionId, query, true)
	recCtx := plan_pipeline.WithWorkflowRecorder(ctx, workflowRun)
	workflowRun.recordUserInput(ctx, query)
	workflowRun.recordRunStatus(ctx, "running", "深度思考处理中")

	// 注入 sessionId：Worker 工具通过 ctx.Value(SessionIdCtxKey{}) 取出，读取会话历史后注入专业 Agent。
	// 传递路径：此处注入 → BuildPlanAgent → adk.Runner.Query → Executor → Worker.Invoke → buildWorkerContext
	recCtx = context.WithValue(recCtx, plan_pipeline.SessionIdCtxKey{}, sessionId)

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
	thinkCtx, thinkCancel := context.WithTimeout(context.WithoutCancel(recCtx), normalizeThinkTimeout(time.Until(deadlineFromContext(recCtx))))
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
		workflowRun.appendEvent(ctx, "workflow.error", map[string]any{
			"sessionId": sessionId,
			"error":     execErr.Error(),
		})
		workflowRun.saveCheckpoint(ctx, "failed", map[string]any{
			"sessionId": sessionId,
			"error":     execErr.Error(),
			"query":     query,
		})
		workflowRun.finish(ctx, workflow.RunStatusFailed, content, execErr.Error())
		g.Log().Errorf(ctx, "[Intent] 深度思考执行失败 | session=%s | err=%v", sessionId, execErr)
		return execErr
	}

	// 将助手回复写入会话记忆并异步持久化到 Redis
	if content != "" {
		workflowRun.recordAssistantOutput(ctx, content)
		workflowRun.saveCheckpoint(ctx, "completed", map[string]any{
			"sessionId": sessionId,
			"query":     query,
			"output":    content,
		})
		workflowRun.recordRunStatus(ctx, workflow.RunStatusSuccess, "深度思考执行完成")
		workflowRun.finish(ctx, workflow.RunStatusSuccess, content, "")

		mem.SetMessages(schema.AssistantMessage(content, nil))
		go func() {
			bgCtx := context.Background()
			if persistErr := cache.SaveSessionWithRetry(bgCtx, sessionId,
				mem.GetRecentMessages(), mem.GetLongTermSummary()); persistErr != nil {
				g.Log().Errorf(bgCtx, "[Intent] 深度思考保存会话失败（已重试） | session=%s | err=%v", sessionId, persistErr)
			}
		}()
	} else {
		workflowRun.saveCheckpoint(ctx, "completed", map[string]any{
			"sessionId": sessionId,
			"query":     query,
			"output":    "",
		})
		workflowRun.recordRunStatus(ctx, workflow.RunStatusSuccess, "深度思考执行完成但无最终内容")
		workflowRun.finish(ctx, workflow.RunStatusSuccess, "", "")
	}

	return nil
}
