package v1

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// GetGeneralReq 获取通用设置
type GetGeneralReq struct {
	g.Meta `path:"/settings/v1/general" method:"get" summary:"获取通用设置"`
}

// GeneralSettings 通用设置内容
type GeneralSettings struct {
	SiteName     string `json:"site_name"`
	AutoMarkRead bool   `json:"auto_mark_read"`
}

// GetGeneralRes 响应
type GetGeneralRes struct {
	Settings GeneralSettings `json:"settings"`
}

// SaveGeneralReq 保存通用设置
type SaveGeneralReq struct {
	g.Meta       `path:"/settings/v1/general" method:"post" summary:"保存通用设置"`
	SiteName     string `json:"site_name"`
	AutoMarkRead bool   `json:"auto_mark_read"`
}

// SaveGeneralRes 保存响应
type SaveGeneralRes struct{}

// GetIngestKeyReq 获取告警接入 API Key
type GetIngestKeyReq struct {
	g.Meta `path:"/settings/v1/ingest_key" method:"get" summary:"获取告警接入 API Key"`
}

// GetIngestKeyRes 响应
type GetIngestKeyRes struct {
	APIKey string `json:"api_key"`
}

// ResetIngestKeyReq 重置告警接入 API Key
type ResetIngestKeyReq struct {
	g.Meta `path:"/settings/v1/ingest_key/reset" method:"post" summary:"重置告警接入 API Key"`
}

// ResetIngestKeyRes 响应
type ResetIngestKeyRes struct {
	APIKey string `json:"api_key"`
}

type GetRetentionReq struct {
	g.Meta `path:"/settings/v1/retention" method:"get" summary:"获取数据保留期"`
}

type RetentionSettings struct {
	PayloadDays int `json:"payload_days"`
	AuditDays   int `json:"audit_days"`
}

type GetRetentionRes struct {
	Settings RetentionSettings `json:"settings"`
}

type SaveRetentionReq struct {
	g.Meta      `path:"/settings/v1/retention" method:"post" summary:"修改数据保留期"`
	PayloadDays int    `json:"payload_days"`
	AuditDays   int    `json:"audit_days"`
	Reason      string `json:"reason"`
}

type SaveRetentionRes struct{}

type ListRetentionAuditReq struct {
	g.Meta `path:"/settings/v1/retention/audit" method:"get" summary:"查询数据保留期审计"`
	Limit  int `json:"limit" d:"100"`
}

type RetentionAuditItem struct {
	ActorID        string    `json:"actor_id"`
	OldPayloadDays int       `json:"old_payload_days"`
	OldAuditDays   int       `json:"old_audit_days"`
	NewPayloadDays int       `json:"new_payload_days"`
	NewAuditDays   int       `json:"new_audit_days"`
	Reason         string    `json:"reason"`
	ChangedAt      time.Time `json:"changed_at"`
}

type ListRetentionAuditRes struct {
	Items []RetentionAuditItem `json:"items"`
}
