// Package v1 定义多源告警接入 API。
//
// 设计思想：
//   - 提供三个接入端点，覆盖主流安全设备的告警推送方式
//   - 所有端点统一返回 {id, is_new, dedup_key}，调用方可据此判断是否重复
//   - Webhook 端点兼容 Splunk / Elastic / 自定义 JSON 格式
//   - CEF/LEEF 端点兼容 ArcSight / QRadar 等传统 SIEM 设备
//   - API Push 端点提供标准化结构体，适合新系统对接
package v1

import "github.com/gogf/gf/v2/frame/g"

// WebhookReq 通用 JSON Webhook 接入（Splunk / Elastic / 自定义）
type WebhookReq struct {
	g.Meta     `path:"/ingest/v1/webhook" method:"post" summary:"通用 JSON Webhook 告警接入"`
	SourceName string `json:"source_name"` // 来源系统名称（可选，优先使用 payload 中的 source 字段）
}

// WebhookRes 接入响应
type WebhookRes struct {
	ID       string `json:"id"`        // 事件 ID
	IsNew    bool   `json:"is_new"`    // true=新事件，false=重复告警已去重
	DedupKey string `json:"dedup_key"` // 去重键（供调试）
}

// CEFReq CEF 格式告警接入（ArcSight 等设备）
// Body 为纯文本 CEF 行，Content-Type: text/plain
type CEFReq struct {
	g.Meta     `path:"/ingest/v1/cef" method:"post" summary:"CEF 格式告警接入"`
	SourceName string `json:"source_name" in:"query"` // 来源系统名称（query 参数）
}

// CEFRes 接入响应
type CEFRes struct {
	ID       string `json:"id"`
	IsNew    bool   `json:"is_new"`
	DedupKey string `json:"dedup_key"`
}

// LEEFReq LEEF 格式告警接入（IBM QRadar 等设备）
type LEEFReq struct {
	g.Meta     `path:"/ingest/v1/leef" method:"post" summary:"LEEF 格式告警接入"`
	SourceName string `json:"source_name" in:"query"`
}

// LEEFRes 接入响应
type LEEFRes struct {
	ID       string `json:"id"`
	IsNew    bool   `json:"is_new"`
	DedupKey string `json:"dedup_key"`
}

// AlertPushReq 标准化 REST API 主动推送（新系统对接推荐）
type AlertPushReq struct {
	g.Meta      `path:"/ingest/v1/push" method:"post" summary:"标准化告警推送接入"`
	Title       string            `json:"title"       v:"required"` // 告警标题
	Content     string            `json:"content"`                  // 详细描述
	Severity    string            `json:"severity"    d:"medium"`   // critical/high/medium/low/info
	Source      string            `json:"source"      v:"required"` // 来源系统名称
	CVEID       string            `json:"cve_id"`                   // CVE 编号（可选）
	ExtraFields map[string]string `json:"extra_fields"`             // 扩展字段（src_ip/dst_ip/host 等）
}

// AlertPushRes 接入响应
type AlertPushRes struct {
	ID       string `json:"id"`
	IsNew    bool   `json:"is_new"`
	DedupKey string `json:"dedup_key"`
}
