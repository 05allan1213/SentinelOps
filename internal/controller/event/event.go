// Package event 提供安全事件 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求 → 调用 eventsvc → 映射响应 DTO。
// 业务逻辑（severity 默认值、risk_score 自动推算）已下沉至 internal/service/event。
// PipelineStream 直接调用 AI 管道，不含 DB 操作，保留在控制器层。
package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	v1 "SentinelOps/api/event/v1"
	"SentinelOps/internal/ai/agent/event_analysis_pipeline"
	"SentinelOps/internal/ai/models"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/trace"
	eventsvc "SentinelOps/internal/service/event"
	"SentinelOps/utility/sse"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

const legacyCompatibilityDisabledMessage = "legacy compatibility is disabled"

// streamEventPayload 与前端解析协议对齐：data 行必须是 JSON envelope。
// content 帧早已使用该协议，error/done 帧若继续发送纯文本会被前端的
// JSON.parse 失败分支静默丢弃，让失败的流式分析伪装成“空结果成功”。
func streamEventPayload(kind, content string) string {
	payload := map[string]string{"type": kind}
	if content != "" {
		payload["content"] = content
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return `{"type":"` + kind + `"}`
	}
	return string(encoded)
}

type streamClient interface {
	Send(string, string)
	Done()
}

type ControllerV1 struct {
	legacyGate airuntime.LegacyCompatibilityGate
	loadAgent  func(context.Context) (compose.Runnable[*event_analysis_pipeline.UserMessage, *schema.Message], error)
	loadModel  func(context.Context) (model.ToolCallingChatModel, error)
	newStream  func(context.Context) streamClient
}

func NewV1(gates ...airuntime.LegacyCompatibilityGate) *ControllerV1 {
	var gate airuntime.LegacyCompatibilityGate
	if len(gates) > 0 {
		gate = gates[0]
	}
	return &ControllerV1{
		legacyGate: gate,
		loadAgent:  event_analysis_pipeline.GetEventAnalysisAgent,
		loadModel:  models.ChatDefault,
		newStream: func(ctx context.Context) streamClient {
			return sse.NewClient(g.RequestFromCtx(ctx))
		},
	}
}

// List 返回事件列表，委托 eventsvc.List 查询，结果映射为 API DTO。
// 数据库不可用时返回空列表而非报错，保证前端不崩溃。
func (c *ControllerV1) List(ctx context.Context, req *v1.ListReq) (*v1.ListRes, error) {
	events, total, err := eventsvc.List(ctx, req.Limit, req.Offset, req.Severity, req.Status, req.Keyword, req.OrderBy, req.OrderDir)
	if err != nil {
		// 数据库不可用时优雅降级，返回空列表
		return &v1.ListRes{Total: 0, Events: []v1.EventItem{}}, nil
	}
	items := make([]v1.EventItem, 0, len(events))
	for _, e := range events {
		// 从 metadata JSON 中提取原始链接
		sourceURL := ""
		if e.Metadata != "" {
			var meta map[string]string
			if err2 := json.Unmarshal([]byte(e.Metadata), &meta); err2 == nil {
				sourceURL = meta["link"]
			}
		}
		items = append(items, v1.EventItem{
			ID:        e.ID,
			Title:     e.Title,
			Content:   "",
			EventType: e.EventType,
			Severity:  e.Severity,
			Source:    e.Source,
			SourceURL: sourceURL,
			Status:    e.Status,
			CVEID:     e.CVEID,
			RiskScore: e.RiskScore,
			CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return &v1.ListRes{Total: total, Events: items}, nil
}

// Stats 返回事件统计数据：总数、今日新增、高危数量、按严重程度分组、近7天新增、待处置数。
func (c *ControllerV1) Stats(ctx context.Context, req *v1.StatsReq) (*v1.StatsRes, error) {
	stats, err := eventsvc.Stats(ctx)
	if err != nil {
		return &v1.StatsRes{BySeverity: map[string]int64{}}, nil
	}
	return &v1.StatsRes{
		Total:         stats.Total,
		TodayCount:    stats.TodayCount,
		CriticalCount: stats.CriticalCount,
		BySeverity:    stats.BySeverity,
		New7Days:      stats.New7Days,
		Pending:       stats.Pending,
	}, nil
}

// Trend 返回最近 N 天的事件趋势，按日期分组，数据库不可用时返回空列表。
func (c *ControllerV1) Trend(ctx context.Context, req *v1.TrendReq) (*v1.TrendRes, error) {
	items, err := eventsvc.Trend(ctx, req.Days)
	if err != nil {
		return &v1.TrendRes{Items: []v1.TrendItem{}}, nil
	}
	result := make([]v1.TrendItem, 0, len(items))
	for _, it := range items {
		result = append(result, v1.TrendItem{
			Date:     it.Date,
			Total:    it.Total,
			Critical: it.Critical,
			High:     it.High,
			Medium:   it.Medium,
			Low:      it.Low,
		})
	}
	return &v1.TrendRes{Items: result}, nil
}

// Create 手动创建安全事件，委托 eventsvc.Create 处理默认值和入库。
func (c *ControllerV1) Create(ctx context.Context, req *v1.CreateReq) (*v1.CreateRes, error) {
	id, err := eventsvc.Create(ctx, req.Title, req.Content, req.Severity, req.Source, req.CVEID, req.RiskScore)
	if err != nil {
		return nil, err
	}
	return &v1.CreateRes{ID: id}, nil
}

// UpdateStatus 更新单条事件状态，委托 eventsvc.UpdateStatus 处理。
func (c *ControllerV1) UpdateStatus(ctx context.Context, req *v1.UpdateStatusReq) (*v1.UpdateStatusRes, error) {
	if err := eventsvc.UpdateStatus(ctx, req.ID, req.Status); err != nil {
		return nil, err
	}
	return &v1.UpdateStatusRes{}, nil
}

// Delete 软删除安全事件，委托 eventsvc.Delete 处理。
func (c *ControllerV1) Delete(ctx context.Context, req *v1.DeleteReq) (*v1.DeleteRes, error) {
	if err := eventsvc.Delete(ctx, req.ID); err != nil {
		return nil, err
	}
	return &v1.DeleteRes{}, nil
}

// BatchDelete 批量软删除安全事件，委托 eventsvc.BatchDelete 处理。
func (c *ControllerV1) BatchDelete(ctx context.Context, req *v1.BatchDeleteReq) (*v1.BatchDeleteRes, error) {
	if err := eventsvc.BatchDelete(ctx, req.IDs); err != nil {
		return nil, err
	}
	return &v1.BatchDeleteRes{}, nil
}

// BatchUpdateStatus 批量更新安全事件状态，委托 eventsvc.BatchUpdateStatus 处理。
func (c *ControllerV1) BatchUpdateStatus(ctx context.Context, req *v1.BatchUpdateStatusReq) (*v1.BatchUpdateStatusRes, error) {
	if err := eventsvc.BatchUpdateStatus(ctx, req.IDs, req.Status); err != nil {
		return nil, err
	}
	return &v1.BatchUpdateStatusRes{}, nil
}

// PipelineStream 触发 Event Analysis Agent（ReAct 智能体）进行流式事件分析，SSE 逐 chunk 推送结果。
func (c *ControllerV1) PipelineStream(ctx context.Context, req *v1.PipelineStreamReq) (*v1.PipelineStreamRes, error) {
	client := c.stream(ctx)
	if !c.allowLegacyCompatibility(ctx, client) {
		return nil, nil
	}

	// 启动链路追踪（标记为独立事件分析，非会话内操作）
	tags := map[string]any{"context": "standalone_event_analysis"}
	ctx = trace.StartRun(ctx, "event.pipeline", "/api/event/v1/pipeline/stream", "", 0, req.Query, tags)
	var pipelineErr error
	defer func() { trace.FinishRun(ctx, pipelineErr) }()

	// 获取事件分析 Agent 单例（内含 RAG 管道 + ReAct 推理循环）
	runner, err := c.agent(ctx)
	if err != nil {
		pipelineErr = err
		client.Send("error", streamEventPayload("error", err.Error()))
		client.Done()
		return nil, nil
	}

	// AGENT span：使内部 LLM/TOOL/RETRIEVER 节点归属到 EventAnalysisAgent 下，
	// 而非直接挂在链路根节点，保持与 intent/executor.go 路径的一致性。
	spanCtx, spanID := trace.StartSpan(ctx, trace.NodeTypeAgent, "EventAnalysisAgent")

	// 以流式模式调用 Agent，逐 chunk 接收推理过程和最终结论
	sr, err := runner.Stream(spanCtx, &event_analysis_pipeline.UserMessage{
		ID:      "stream",
		Query:   req.Query,
		History: nil, // 事件分析不携带历史上下文，每次独立分析
	})
	if err != nil {
		pipelineErr = err
		trace.FinishSpan(spanCtx, spanID, err, nil)
		client.Send("error", streamEventPayload("error", err.Error()))
		client.Done()
		return nil, nil
	}
	defer sr.Close()

	var spanErr error
	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			// 流式输出结束，通知前端关闭 SSE 连接
			client.Send("done", streamEventPayload("done", "Stream completed"))
			break
		}
		if err != nil {
			pipelineErr = err
			spanErr = err
			client.Send("error", streamEventPayload("error", err.Error()))
			break
		}
		// 过滤纯空白 chunk，避免无意义推送
		// 将内容包装为 JSON 格式，与前端解析协议对齐：{"type":"content","content":"..."}
		if chunk != nil && strings.TrimSpace(chunk.Content) != "" {
			jsonBytes, _ := json.Marshal(map[string]string{"type": "content", "content": chunk.Content})
			client.Send("message", string(jsonBytes))
		}
	}
	trace.FinishSpan(spanCtx, spanID, spanErr, nil)
	client.Done()
	return nil, nil
}

// AnalyzeSingleStream 针对单条安全事件直接调用 LLM，SSE 流式返回结构化 AI 解决方案。
// 直接调用模型（无 ReAct 工具调用），避免多步工具循环导致的延迟或步数耗尽问题，
// 响应更快，可靠性更高。
func (c *ControllerV1) AnalyzeSingleStream(ctx context.Context, req *v1.AnalyzeSingleStreamReq) (*v1.AnalyzeSingleStreamRes, error) {
	client := c.stream(ctx)
	if !c.allowLegacyCompatibility(ctx, client) {
		return nil, nil
	}

	g.Log().Infof(ctx, "[AnalyzeSingleStream] 收到请求 | event_id=%s | title=%s | severity=%s", req.EventID, req.Title, req.Severity)

	m, err := c.model(ctx)
	if err != nil {
		g.Log().Errorf(ctx, "[AnalyzeSingleStream] 模型初始化失败 | err=%v", err)
		client.Send("error", streamEventPayload("error", err.Error()))
		client.Done()
		return nil, nil
	}

	cveInfo := ""
	if req.CVEID != "" {
		cveInfo = "CVE 编号：" + req.CVEID + "\n"
	}

	systemContent := `你是安全事件应急响应专家。针对用户提供的安全事件，基于安全知识和行业最佳实践，生成一份结构化的应急响应方案。

## 输出格式（严格 Markdown，必须包含以下三节）

### 应急响应步骤
- 立即需要采取的行动（分点列出，按优先级排序）

### 修复方案
- 具体的技术修复措施（版本升级、配置变更、补丁路径等）

### 长期防护建议
- 系统性安全加固措施（架构改进、监控策略、定期审计等）

输出使用中文，技术术语保留英文。`

	userContent := fmt.Sprintf(
		"针对以下安全事件，请提供详细的 AI 解决方案：\n\n事件标题：%s\n严重程度：%s\n%s数据来源：%s",
		req.Title, req.Severity, cveInfo, req.Source,
	)

	messages := []*schema.Message{
		schema.SystemMessage(systemContent),
		schema.UserMessage(userContent),
	}

	sr, err := m.Stream(ctx, messages)
	if err != nil {
		g.Log().Errorf(ctx, "[AnalyzeSingleStream] Stream 调用失败 | err=%v", err)
		client.Send("error", streamEventPayload("error", err.Error()))
		client.Done()
		return nil, nil
	}
	defer sr.Close()

	chunkCount := 0
	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			g.Log().Debugf(ctx, "[AnalyzeSingleStream] Stream 结束 | chunks=%d", chunkCount)
			client.Send("done", streamEventPayload("done", "Stream completed"))
			break
		}
		if err != nil {
			// context.Canceled 是客户端主动断开连接（前端关闭面板），属正常行为，降级为 info 避免误报
			if errors.Is(err, context.Canceled) {
				g.Log().Warningf(ctx, "[AnalyzeSingleStream] 客户端主动断开连接 | chunks=%d", chunkCount)
			} else {
				g.Log().Errorf(ctx, "[AnalyzeSingleStream] Stream 接收异常 | chunks=%d | err=%v", chunkCount, err)
				client.Send("error", streamEventPayload("error", err.Error()))
			}
			break
		}
		if chunk == nil {
			g.Log().Debugf(ctx, "[AnalyzeSingleStream] 收到空 chunk，跳过")
			continue
		}
		if strings.TrimSpace(chunk.Content) != "" {
			chunkCount++
			jsonBytes, _ := json.Marshal(map[string]string{"type": "content", "content": chunk.Content})
			client.Send("message", string(jsonBytes))
		}
	}
	client.Done()
	return nil, nil
}

func (c *ControllerV1) allowLegacyCompatibility(ctx context.Context, client streamClient) bool {
	if c != nil && c.legacyGate != nil && c.legacyGate.AllowLegacyCompatibility(ctx) {
		return true
	}
	client.Send("error", streamEventPayload("error", legacyCompatibilityDisabledMessage))
	client.Done()
	return false
}

func (c *ControllerV1) stream(ctx context.Context) streamClient {
	if c != nil && c.newStream != nil {
		return c.newStream(ctx)
	}
	return sse.NewClient(g.RequestFromCtx(ctx))
}

func (c *ControllerV1) agent(ctx context.Context) (compose.Runnable[*event_analysis_pipeline.UserMessage, *schema.Message], error) {
	if c != nil && c.loadAgent != nil {
		return c.loadAgent(ctx)
	}
	return event_analysis_pipeline.GetEventAnalysisAgent(ctx)
}

func (c *ControllerV1) model(ctx context.Context) (model.ToolCallingChatModel, error) {
	if c != nil && c.loadModel != nil {
		return c.loadModel(ctx)
	}
	return models.ChatDefault(ctx)
}
