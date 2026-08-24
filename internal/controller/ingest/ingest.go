// Package ingestctrl 提供多源告警接入 HTTP 控制器。
//
// 设计思想：
//   - 职责仅限 HTTP 层：读取原始 body → 调用 ingest 包解析归一化 → 返回统一响应
//   - 三种接入格式（Webhook/CEF/LEEF）共用同一套入库逻辑（ingest.Ingest）
//   - 控制器不含业务逻辑，格式解析下沉至 internal/ingest 包
//
// 主要流程：
//  1. 读取原始请求 body（CEF/LEEF 为纯文本，Webhook/Push 为 JSON）
//  2. 调用对应解析器生成 NormalizedAlert
//  3. 调用 ingest.Ingest 去重写库
//  4. 返回 {id, is_new, dedup_key}
package ingestctrl

import (
	"context"
	"encoding/json"
	"io"

	v1 "SentinelOps/api/ingest/v1"
	"SentinelOps/internal/service/ingest"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

type ControllerV1 struct{}

func NewV1() *ControllerV1 { return &ControllerV1{} }

// Webhook 接收通用 JSON Webhook 告警（Splunk / Elastic / 自定义格式）。
func (c *ControllerV1) Webhook(ctx context.Context, req *v1.WebhookReq) (*v1.WebhookRes, error) {
	body, err := io.ReadAll(g.RequestFromCtx(ctx).Body)
	if err != nil {
		return nil, gerror.Wrap(err, "读取请求体失败")
	}
	// source_name 优先从 query 参数取，其次从已绑定的 req 字段
	sourceName := g.RequestFromCtx(ctx).GetQuery("source_name", req.SourceName).String()
	alert, err := ingest.ParseWebhook(body, sourceName)
	if err != nil {
		return nil, gerror.Wrap(err, "Webhook 解析失败")
	}
	id, isNew, err := ingest.Ingest(ctx, alert)
	if err != nil {
		return nil, gerror.Wrap(err, "告警入库失败")
	}
	return &v1.WebhookRes{ID: id, IsNew: isNew, DedupKey: alert.DedupKey()}, nil
}

// CEF 接收 ArcSight CEF 格式告警（纯文本 body）。
func (c *ControllerV1) CEF(ctx context.Context, req *v1.CEFReq) (*v1.CEFRes, error) {
	body, err := io.ReadAll(g.RequestFromCtx(ctx).Body)
	if err != nil {
		return nil, gerror.Wrap(err, "读取请求体失败")
	}
	sourceName := g.RequestFromCtx(ctx).GetQuery("source_name", req.SourceName).String()
	alert, err := ingest.ParseCEF(string(body), sourceName)
	if err != nil {
		return nil, gerror.Wrap(err, "CEF 解析失败")
	}
	id, isNew, err := ingest.Ingest(ctx, alert)
	if err != nil {
		return nil, gerror.Wrap(err, "告警入库失败")
	}
	return &v1.CEFRes{ID: id, IsNew: isNew, DedupKey: alert.DedupKey()}, nil
}

// LEEF 接收 IBM QRadar LEEF 格式告警（纯文本 body）。
func (c *ControllerV1) LEEF(ctx context.Context, req *v1.LEEFReq) (*v1.LEEFRes, error) {
	body, err := io.ReadAll(g.RequestFromCtx(ctx).Body)
	if err != nil {
		return nil, gerror.Wrap(err, "读取请求体失败")
	}
	sourceName := g.RequestFromCtx(ctx).GetQuery("source_name", req.SourceName).String()
	alert, err := ingest.ParseLEEF(string(body), sourceName)
	if err != nil {
		return nil, gerror.Wrap(err, "LEEF 解析失败")
	}
	id, isNew, err := ingest.Ingest(ctx, alert)
	if err != nil {
		return nil, gerror.Wrap(err, "告警入库失败")
	}
	return &v1.LEEFRes{ID: id, IsNew: isNew, DedupKey: alert.DedupKey()}, nil
}

// AlertPush 接收标准化 REST API 告警推送（新系统对接推荐方式）。
func (c *ControllerV1) AlertPush(ctx context.Context, req *v1.AlertPushReq) (*v1.AlertPushRes, error) {
	// 重建原始 payload JSON 用于审计溯源
	rawBytes, _ := json.Marshal(req)
	alert := &ingest.NormalizedAlert{
		Title:        req.Title,
		Content:      req.Content,
		Severity:     req.Severity,
		Source:       req.Source,
		IngestSource: "api_push",
		CVEID:        req.CVEID,
		ExtraFields:  req.ExtraFields,
		RawPayload:   string(rawBytes),
	}
	if alert.ExtraFields == nil {
		alert.ExtraFields = map[string]string{}
	}
	id, isNew, err := ingest.Ingest(ctx, alert)
	if err != nil {
		return nil, gerror.Wrap(err, "告警入库失败")
	}
	return &v1.AlertPushRes{ID: id, IsNew: isNew, DedupKey: alert.DedupKey()}, nil
}
