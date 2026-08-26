package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/google/uuid"
)

// ── Tracer 公开入口 ────────────────────────────────────────────────────────────
//
// tracer.go 是 trace 系统对外暴露的公开入口，包含两类使用场景：
//
//  1. HTTP 请求链路（Controller 层）：
//     StartRun → [Eino Graph 执行，callbacks 自动追踪] → FinishRun（defer）
//     代表链路：chat.intent、event.pipeline
//
//  2. 后台异步任务：
//     某些重要的后台操作（如对话历史摘要）无法挂载到 HTTP 请求链路，
//     可独立调用 StartRun 创建独立链路，在 Traces 页面以 trace_name 区分。
//     代表链路：memory.summarize（memory.go 的 summarizeOldMessages）
//
// 调用模式（以 chat controller 为例）：
//
//	func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (*v1.ChatRes, error) {
//	    ctx = trace.StartRun(ctx, "chat.intent", "/api/chat/v1/chat", req.SessionId, req.Query, tags)
//	    var execErr error
//	    defer func() { trace.FinishRun(ctx, execErr) }()
//	    ...
//	}
//
// 设计细节：
//   - StartRun 注入的 ctx（携带 ActiveTrace）必须贯穿整个请求调用链，
//     Eino Graph、Worker Agent、GORM Plugin 均通过 Extract(ctx) 获取同一个 ActiveTrace
//   - FinishRun 使用 defer + 具名返回变量（execErr）捕获最终错误状态，
//     保证 panic recovery、正常返回、错误返回三种路径均能触发

// StartRun 创建本次请求的 TraceRun，将 ActiveTrace 注入 ctx，并异步写入初始记录。
//
// 参数：
//   - name         : 链路语义名称，如 "chat.intent" / "chat.file_upload"，用于 Traces 页面分类
//   - entryPoint   : HTTP 路径，如 "/api/chat/v1/chat"，便于关联到接口维度
//   - sessionID    : 前端传入的会话 ID，用于在 Traces 中按会话过滤
//   - messageIndex : 消息在会话中的序号，用于关联用户反馈
//   - query        : 用户原始查询文本（截断至 2000 字符），不含 Prompt 模板
//   - tags         : 额外标签（如 deep_thinking:true），序列化为 JSON 存储
func StartRun(ctx context.Context, name, entryPoint, sessionID string, messageIndex int, query string, tags map[string]any) context.Context {
	if !IsEnabled() {
		return ctx
	}
	traceID := uuid.New().String()
	now := time.Now()

	// 截断 query 到 2000 字符（rune 安全截断，避免中文乱码）
	queryText := query
	if len([]rune(queryText)) > 2000 {
		runes := []rune(queryText)
		queryText = string(runes[:2000])
	}

	// 序列化 tags 为 JSON 字符串存储；服务端身份覆盖任何同名调用方标签。
	tags = authoritativeTraceTags(ctx, tags)
	tagsJSON := ""
	if len(tags) > 0 {
		redactedTags, redactErr := redactTraceValue(tags)
		if b, err := json.Marshal(redactedTags); redactErr == nil && err == nil {
			tagsJSON = string(b)
		}
	}

	at, err := newActiveTrace(ctx, AttemptMetadata{TraceID: traceID, SessionID: sessionID, Query: queryText})
	if err != nil {
		return ctx
	}
	at.StartTime = now

	submitInsertRun(at, &dao.TraceRun{
		TraceID:      traceID,
		TraceName:    name,
		EntryPoint:   entryPoint,
		SessionID:    sessionID,
		MessageIndex: messageIndex,
		QueryText:    at.Attempt.Query,
		Status:       StatusRunning,
		StartTime:    now,
		Tags:         tagsJSON,
	})

	return Inject(ctx, at)
}

// StartAttempt 为 Query/Resume/Replay 的每次实际执行创建独立 MySQL Trace 和 barrier。
func StartAttempt(ctx context.Context, metadata AttemptMetadata, runtimes ...*LangfuseRuntime) (context.Context, *AttemptBarrier, error) {
	if len(runtimes) > 1 {
		return ctx, nil, fmt.Errorf("at most one Attempt Langfuse runtime is accepted")
	}
	var langfuseRuntime *LangfuseRuntime
	if len(runtimes) == 1 {
		langfuseRuntime = runtimes[0]
	}
	initialized := false
	if langfuseRuntime != nil {
		defer func() {
			if !initialized {
				shutdownCtx := ctx
				if shutdownCtx == nil {
					shutdownCtx = context.Background()
				}
				_ = langfuseRuntime.Shutdown(shutdownCtx)
			}
		}()
	}
	at, err := newActiveTrace(ctx, metadata)
	if err != nil {
		return ctx, nil, err
	}
	tags := authoritativeTraceTags(ctx, map[string]any{
		"run_id": metadata.RunID, "attempt": metadata.Attempt,
		"lease_generation": metadata.LeaseGeneration, "runtime_version": metadata.RuntimeVersion,
		"trace_quality": "pending",
	})
	redactedTags, err := redactTraceValue(tags)
	if err != nil {
		return ctx, nil, err
	}
	tagsJSON, err := json.Marshal(redactedTags)
	if err != nil {
		return ctx, nil, err
	}
	submitInsertRun(at, &dao.TraceRun{
		TraceID: metadata.TraceID, TraceName: "durable.attempt", EntryPoint: "worker",
		SessionID: metadata.SessionID, QueryText: at.Attempt.Query, Status: StatusRunning,
		StartTime: at.StartTime, Tags: string(tagsJSON),
	})
	attemptCtx := Inject(ctx, at)
	barrier := &AttemptBarrier{active: at, langfuseOwned: langfuseRuntime != nil}
	if langfuseRuntime != nil {
		userID, _ := policy.UserID(ctx)
		attemptCtx = langfuseRuntime.StartAttempt(attemptCtx, metadata, userID)
		barrier.langfuse = langfuseRuntime
		barrier.langfuseCtx = attemptCtx
	}
	initialized = true
	return attemptCtx, barrier, nil
}

func authoritativeTraceTags(ctx context.Context, tags map[string]any) map[string]any {
	serverTags := make(map[string]any, len(tags)+1)
	for key, value := range tags {
		serverTags[key] = value
	}
	if identity, err := policy.IdentityFromContext(ctx); err == nil {
		serverTags["server_user_id"] = identity.UserID
	} else {
		delete(serverTags, "server_user_id")
	}
	return serverTags
}

// FinishRun 将 TraceRun 更新为终态，汇总全链路 Token 消耗和估算费用。
//
// 调用时机：在 Controller 函数的 defer 中调用，传入本次执行的最终错误（nil 表示成功）。
// err != nil 时记录截断后的错误消息并分类错误码，便于 Traces 页面统计错误分布。
//
// 若 ctx 中无 ActiveTrace（trace 未启用或非 trace 链路），此函数立即返回，零开销。
func FinishRun(ctx context.Context, err error) {
	at := Extract(ctx)
	if at == nil {
		return
	}
	barrier := &AttemptBarrier{active: at}
	barrier.Finish(err)
	go func() {
		flushCtx, cancel := context.WithTimeout(at.writeCtx, 10*time.Second)
		defer cancel()
		_ = barrier.Flush(flushCtx)
	}()
}
