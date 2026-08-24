// Package chat 提供对话相关 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求、管理 SSE 连接、调用 chatsvc、映射响应 DTO。
// 业务逻辑（Agent 调用、记忆管理、Prompt 拼装、意图识别）已下沉至 internal/service/chat。
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	apichat "SentinelOps/api/chat"
	v1 "SentinelOps/api/chat/v1"
	aidoc "SentinelOps/internal/ai/document"
	"SentinelOps/internal/ai/memory"
	toolsintelligence "SentinelOps/internal/ai/tools/intelligence"
	"SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	chatsvc "SentinelOps/internal/service/chat"
	"SentinelOps/utility/sse"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gfile"
)

type ControllerV1 struct{}

func NewV1() apichat.IChatV1 {
	return &ControllerV1{}
}

// replayWorkflowEvents 按持久化事件序号补发 SSE 事件，不重复写入事件表。
func replayWorkflowEvents(client *sse.Client, events []workflow.StreamEvent) {
	for _, event := range events {
		client.SendEvent(event.ID, event.Type, workflowPayloadToString(event.Payload))
	}
}

// workflowPayloadToString 将工作流事件载荷转换为 SSE data 字符串。
func workflowPayloadToString(payload any) string {
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

// initWorkflowStore 尽力初始化工作流事件存储；失败时返回 nil，聊天主流程继续执行。
func initWorkflowStore(ctx context.Context) workflow.Store {
	defer func() {
		if r := recover(); r != nil {
			g.Log().Warningf(ctx, "[Chat] 初始化 workflow store 发生 panic，降级为非持久化 SSE | err=%v", r)
		}
	}()
	db, err := mysql.DB(ctx)
	if err != nil || db == nil {
		if err != nil {
			g.Log().Warningf(ctx, "[Chat] 初始化 workflow store 失败，降级为非持久化 SSE | err=%v", err)
		}
		return nil
	}
	return workflow.NewGORMStore(db)
}

// workflowPersistContext 为 SSE 工作流持久化构造隔离取消但保留截止时间的上下文。
// HTTP 客户端断连会取消请求 context；若直接透传到 GORM，会导致事件落库和完成状态写回报 context canceled。
func workflowPersistContext(ctx context.Context) context.Context {
	persistCtx := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		persistCtx, cancel = context.WithDeadline(persistCtx, deadline)
		_ = cancel
	}
	return persistCtx
}

// normalizeDeepThinkingTimeoutSec 规范化深度思考超时时间。
// 深度思考包含预思考 + Planner + 多轮 Executor/Replanner + 多 Worker 串行调用，
// 复杂场景下 300 秒容易在正常执行过程中被打满，因此设置 600 秒保底值。
func normalizeDeepThinkingTimeoutSec(sec int) int {
	if sec < 600 {
		return 600
	}
	return sec
}

// FileUpload 上传知识文档并构建向量索引，单次上限 50 MB。
// 支持格式：.txt .md .markdown .pdf .docx .pptx
// 文件解析和保存依赖 GoFrame API，必须保留在 HTTP 层；向量索引构建委托 chatsvc.BuildFileIndex。
func (c *ControllerV1) FileUpload(ctx context.Context, req *v1.FileUploadReq) (*v1.FileUploadRes, error) {
	const maxUploadBytes int64 = 50 << 20 // 最大上传大小为 50 MB

	fileDir, err := g.Cfg().Get(ctx, "file_dir")
	if err != nil {
		return nil, gerror.Wrap(err, "读取文件目录配置失败")
	}
	fileDirPath := fileDir.String()

	r := g.RequestFromCtx(ctx)
	uploadFile := r.GetUploadFile("file")
	if uploadFile == nil {
		return nil, gerror.New("请上传文件")
	}
	if uploadFile.Size > maxUploadBytes {
		return nil, gerror.Newf("文件过大（%.1f MB），单次上传上限为 50 MB", float64(uploadFile.Size)/(1<<20))
	}

	// 扩展名白名单校验
	allowedExt := map[string]bool{
		".txt": true, ".md": true, ".markdown": true,
		".pdf": true, ".docx": true, ".pptx": true,
	}
	ext := filepath.Ext(uploadFile.Filename)
	if !allowedExt[ext] {
		return nil, gerror.Newf("不支持的文件格式 %s，支持：.txt .md .markdown .pdf .docx .pptx", ext)
	}

	// 启动链路追踪（文件验证通过后再启动，避免记录无效请求）
	ctx = trace.StartRun(ctx, "chat.file_upload", "/api/chat/v1/file_upload", "", 0,
		uploadFile.Filename, map[string]any{"file_size": uploadFile.Size})
	var uploadErr error
	defer func() { trace.FinishRun(ctx, uploadErr) }()

	// 存储目录不存在时自动创建
	if !gfile.Exists(fileDirPath) {
		if err := gfile.Mkdir(fileDirPath); err != nil {
			uploadErr = err
			return nil, gerror.Wrapf(err, "创建目录失败: %s", fileDirPath)
		}
	}
	savePath := filepath.Join(fileDirPath)
	// 保存文件到磁盘，false 表示不覆盖同名文件
	if _, err := uploadFile.Save(savePath, false); err != nil {
		uploadErr = err
		return nil, gerror.Wrapf(err, "保存文件失败")
	}
	fileInfo, err := os.Stat(savePath)
	if err != nil {
		uploadErr = err
		return nil, gerror.Wrapf(err, "获取文件信息失败")
	}

	// 从请求参数构造分块配置（零值字段使用默认值）
	cfg := aidoc.DefaultChunkConfig()
	if req.Strategy != "" {
		cfg.Strategy = aidoc.ChunkStrategy(req.Strategy)
	}
	if req.ChunkSize > 0 {
		cfg.ChunkSize = req.ChunkSize
	}
	if req.OverlapSize > 0 {
		cfg.OverlapSize = req.OverlapSize
	}

	// 构建向量索引（Milvus 去重 + 嵌入写入）
	if err = chatsvc.BuildFileIndex(ctx, fileDirPath+"/"+uploadFile.Filename, cfg); err != nil {
		uploadErr = err
		return nil, gerror.Wrapf(err, "构建知识库失败")
	}
	return &v1.FileUploadRes{
		FileName: uploadFile.Filename,
		FilePath: savePath,
		FileSize: fileInfo.Size(),
	}, nil
}

// Chat 多 Agent 对话入口，SSE 流式推送 type+content。
//
// ┌─ deep_thinking=false（标准模式）─────────────────────────────────────────────────┐
// │  Router LLM 识别意图（chat/event/report/risk/solve）→ Executor 分发 SubAgent    │
// │  SSE 事件：status + chat/event/report/risk/solve                               │
// └─────────────────────────────────────────────────────────────────────────────────┘
// ┌─ deep_thinking=true（深度思考模式）──────────────────────────────────────────────┐
// │  跳过路由，直接进入 Plan Agent（Supervisor-Worker）                              │
// │  Planner 规划步骤 → Executor 按步调用 Worker（event/report/risk/solve Agent）    │
// │  SSE 事件：status + plan_step（中间步骤）+ plan（最终答案）                      │
// └─────────────────────────────────────────────────────────────────────────────────┘
func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (*v1.ChatRes, error) {
	// 确保 query 不为空（防止前端传空字符串绕过验证）
	if req.Query == "" {
		g.Log().Warningf(ctx, "[Chat] 收到空查询 | session_id=%s", req.SessionId)
		return nil, gerror.New("查询内容不能为空")
	}

	// 记录查询内容
	g.Log().Debugf(ctx, "[Chat] 收到请求 | session_id=%s | query=%s | deep_thinking=%v",
		req.SessionId, req.Query[:min(50, len(req.Query))], req.DeepThinking)

	// 启动链路追踪
	tags := map[string]any{"deep_thinking": req.DeepThinking}
	ctx = trace.StartRun(ctx, "chat.intent", "/api/chat/v1/intent", req.SessionId, req.MessageIndex, req.Query, tags)
	var execErr error
	defer func() { trace.FinishRun(ctx, execErr) }()

	client := sse.NewClient(g.RequestFromCtx(ctx))

	// 启用工作流事件存储；不可用时仅影响断线补发与落库，不影响聊天主流程。
	workflowStore := initWorkflowStore(ctx)
	workflowRunID := req.RunID
	var workflowSeq atomic.Int64
	if workflowStore != nil && workflowRunID == "" {
		inputPayload := workflowPayloadToString(map[string]any{
			"query":        req.Query,
			"deepThinking": req.DeepThinking,
			"webSearch":    req.WebSearch,
		})
		run, err := workflowStore.CreateRun(ctx, workflow.WorkflowRunInput{
			WorkflowKey:  "chat.intent",
			SessionID:    req.SessionId,
			InputPayload: inputPayload,
		})
		if err != nil {
			g.Log().Warningf(ctx, "[Chat] 创建 workflow run 失败，降级为非持久化 SSE | err=%v", err)
			workflowStore = nil
		} else {
			workflowRunID = run.ID
		}
	}

	// ── 流式中断恢复能力 ────────────────────────────────
	// 问题根因：
	//   SSE 是单向长连接，网络抖动、代理超时、浏览器刷新均会导致连接中断。
	//   中断后前端重新发起请求，若后端重新执行 Agent，会产生重复 LLM 调用和重复费用，
	//   且已流出的内容无法续接，用户看到的回复从头开始，体验割裂。
	//
	// 设计思路：
	//   每条 SSE 事件写入 workflow_events 表（run_id + seq 唯一索引），
	//   前端通过 SSE id 字段持久化 last_seq 到 sessionStorage。
	//   重连时携带 run_id + last_seq，后端调用 ListEventsAfter 查出缺失事件，
	//   通过 replayWorkflowEvents 补发给前端，Agent 无需重新执行。
	//   AppendEvent 使用 ON CONFLICT DO NOTHING，保证重放幂等，不产生重复记录。
	// 重连请求携带 run_id 与 last_seq 时，先补发历史事件。
	if workflowStore != nil && req.RunID != "" && req.LastSeq > 0 {
		events, err := workflowStore.ListEventsAfter(ctx, req.RunID, req.LastSeq)
		if err != nil {
			g.Log().Warningf(ctx, "[Chat] 补发 workflow 事件失败 | run_id=%s | last_seq=%d | err=%v", req.RunID, req.LastSeq, err)
		} else {
			replayWorkflowEvents(client, events)
			workflowSeq.Store(req.LastSeq)
			if len(events) > 0 {
				workflowSeq.Store(events[len(events)-1].ID)
			}
		}
	}

	// sendEvent 统一发送 SSE；工作流存储可用时使用原子递增 seq 作为 SSE id 并落库。
	// atomic.Add 保证心跳 goroutine 与主 goroutine 并发调用时 seq 全局唯一、单调递增，
	// 前端凭 id 字段追踪 last_seq，断线重连时后端据此精确补发缺失事件（见问题三）。
	sendEvent := func(eventType, data string) {
		if workflowStore == nil || workflowRunID == "" {
			client.Send(eventType, data)
			return
		}
		seq := workflowSeq.Add(1)
		event := workflow.StreamEvent{
			ID:        seq,
			RunID:     workflowRunID,
			Type:      eventType,
			Payload:   data,
			CreatedAt: time.Now(),
		}
		if err := workflowStore.AppendEvent(workflowPersistContext(ctx), event); err != nil {
			g.Log().Warningf(ctx, "[Chat] 追加 workflow 事件失败 | run_id=%s | seq=%d | type=%s | err=%v", workflowRunID, seq, eventType, err)
		}
		client.SendEvent(seq, eventType, data)
	}

	defer func() {
		if workflowStore == nil || workflowRunID == "" || req.RunID != "" {
			return
		}
		status := workflow.RunStatusSuccess
		errorMessage := ""
		if execErr != nil {
			status = workflow.RunStatusFailed
			errorMessage = execErr.Error()
		}
		if err := workflowStore.FinishRun(workflowPersistContext(ctx), workflowRunID, status, "", errorMessage); err != nil {
			g.Log().Warningf(ctx, "[Chat] 完成 workflow run 失败 | run_id=%s | err=%v", workflowRunID, err)
		}
	}()

	// 推送 meta 事件：携带会话元数据，供前端显示 Agent 状态和调试追踪
	metaData := fmt.Sprintf(`{"sessionId":%q,"runId":%q,"timestamp":%q,"deepThinking":%v}`,
		req.SessionId, workflowRunID, time.Now().Format(time.RFC3339), req.DeepThinking)
	sendEvent("meta", metaData)

	// 心跳 goroutine：每 15 秒推送一次 keepalive 注释行，防止 Plan Agent 长时间执行期间
	// 代理或浏览器因空闲超时关闭 SSE 连接。
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				client.SendHeartbeat()
			case <-heartbeatDone:
				return
			}
		}
	}()

	var err error
	// Agent 执行超时
	// 通过 context.WithTimeout 为整个 Agent 执行链路设置最长运行时间：
	//   - 标准模式：90s（Router + SubAgent 单轮，含 RAG 检索）
	//   - 深度思考：300s（Planner 规划 + 多 Worker 串行执行）
	// 超时后 agentCtx.Done() 触发，LLM 调用中断，SSE 推送 error 事件给前端。
	timeout := time.Duration(g.Cfg().MustGet(ctx, "limiter.agent_timeout_sec", 90).Int()) * time.Second
	if req.DeepThinking {
		deepTimeoutSec := normalizeDeepThinkingTimeoutSec(g.Cfg().MustGet(ctx, "limiter.agent_deep_timeout_sec", 600).Int())
		timeout = time.Duration(deepTimeoutSec) * time.Second
	}
	agentCtx, agentCancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer agentCancel()
	if req.WebSearch {
		agentCtx = context.WithValue(agentCtx, toolsintelligence.WebSearchEnabledKey{}, true)
	}
	if req.UserID != "" {
		agentCtx = context.WithValue(agentCtx, memory.UserIdCtxKey{}, req.UserID)
	}
	if req.DeepThinking {
		err = chatsvc.ExecuteDeepThink(agentCtx, req.SessionId, req.Query, req.MessageIndex, func(intentType, chunk string) {
			sendEvent(intentType, chunk)
		})
	} else {
		err = chatsvc.ExecuteIntent(agentCtx, req.SessionId, req.Query, req.MessageIndex, func(intentType, chunk string) {
			sendEvent(intentType, chunk)
		})
	}
	execErr = err

	// 意图执行结束（正常或异常），停止心跳
	close(heartbeatDone)

	if err != nil {
		g.Log().Errorf(ctx, "[Intent] 执行失败 | deep_thinking=%v | err=%v", req.DeepThinking, err)
		sendEvent("error", err.Error())
	}

	client.Done()
	return nil, nil
}
