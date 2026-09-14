// Package chat 提供对话相关 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求、管理 SSE 连接、调用 chatsvc、映射响应 DTO。
// 业务逻辑（Agent 调用、记忆管理、Prompt 拼装、意图识别）已下沉至 internal/service/chat。
package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	apichat "SentinelOps/api/chat"
	v1 "SentinelOps/api/chat/v1"
	aidoc "SentinelOps/internal/ai/document"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	chatsvc "SentinelOps/internal/service/chat"
	"SentinelOps/utility/sse"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gfile"
)

type ControllerV1 struct {
	durable *chatsvc.DurableService
}

func NewV1(durable ...*chatsvc.DurableService) apichat.IChatV1 {
	controller := &ControllerV1{}
	if len(durable) > 0 {
		controller.durable = durable[0]
	}
	return controller
}

// FileUpload 上传知识文档并构建向量索引，单次上限 50 MB。
// 支持格式：.txt .md .markdown .pdf .docx .pptx
// 文件解析和保存依赖 GoFrame API，必须保留在 HTTP 层；向量索引构建委托 chatsvc.BuildFileIndex。
// fileUploadEntryPoint 是 Trace 记录的入口路径，必须与注册的 `/api/upload` 一致：
// 旧值 `/api/chat/v1/file_upload` 已不存在，会让 Traces 页面展示无法对应的入口。
const fileUploadEntryPoint = "/api/upload"

func (c *ControllerV1) FileUpload(ctx context.Context, req *v1.FileUploadReq) (*v1.FileUploadRes, error) {
	const maxUploadBytes int64 = 50 << 20 // 最大上传大小为 50 MB
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return nil, err
	}

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
	ctx = trace.StartRun(ctx, "chat.file_upload", fileUploadEntryPoint, "", 0,
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
	// GoFrame 的 Save 接收目录路径并返回实际落盘文件名；这里必须用返回的文件名
	// 组装完整路径，否则后续 Stat/索引都会指向目录（filePath=目录、fileSize=目录大小）。
	savedName, err := uploadFile.Save(fileDirPath, false)
	if err != nil {
		uploadErr = err
		return nil, gerror.Wrapf(err, "保存文件失败")
	}
	savePath := filepath.Join(fileDirPath, savedName)
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
	if err = chatsvc.BuildFileIndex(ctx, savePath, cfg); err != nil {
		uploadErr = err
		return nil, gerror.Wrapf(err, "构建知识库失败")
	}
	return &v1.FileUploadRes{
		FileName: uploadFile.Filename,
		FilePath: savePath,
		FileSize: fileInfo.Size(),
	}, nil
}

// Chat 是旧 /chat/v1 的创建兼容适配器，只返回 durable Run 身份。
// Agent 执行与事件 tail 分别由独立 Worker 和 /chat/v2/runs/{run_id}/events 承担。
func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (*v1.ChatRes, error) {
	if c.durable == nil {
		return nil, gerror.New("durable chat adapter is not initialized")
	}
	run, afterSeq, err := c.resolveRun(ctx, req)
	if err != nil {
		if status := durableHTTPStatus(err); status != 0 {
			g.RequestFromCtx(ctx).Response.WriteHeader(status)
		}
		return nil, err
	}
	client := sse.NewClient(g.RequestFromCtx(ctx))
	meta, _ := json.Marshal(map[string]any{"sessionId": run.SessionID, "runId": run.ID, "status": run.Status, "after_seq": afterSeq})
	client.SendEvent(durableIdentityEventID(strings.TrimSpace(req.RunID) != ""), workflow.EventRunCreated, string(meta))
	client.Done()
	return nil, nil
}

// durableIdentityEventID keeps synthetic v1 identity metadata outside the persisted event cursor on every reconnect.
func durableIdentityEventID(reconnect bool) int64 {
	if reconnect {
		return 0
	}
	return 1
}

// resolveRun 按 v1 兼容契约区分首次创建与已有 Run 重连。
// 重连只读取既有身份，不写 run.created，也不触发 Worker/Agent/Effect。
func (c *ControllerV1) resolveRun(ctx context.Context, req *v1.ChatReq) (*mysql.WorkflowRun, int64, error) {
	if strings.TrimSpace(req.RunID) == "" {
		if req.LastSeq != 0 {
			return nil, 0, chatsvc.ErrDurableReconnectInvalid
		}
		run, err := c.durable.CreateRun(ctx, chatsvc.CreateDurableRunRequest{
			SessionID: req.SessionId, Query: req.Query, Agent: chatsvc.DurableAgentPlan,
		})
		return run, 0, err
	}
	run, err := c.durable.ResumeRun(ctx, req.RunID, req.SessionId, req.LastSeq)
	return run, req.LastSeq, err
}
