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

// GetRuntimeGatesReq 请求当前 Runtime Gate 三层视图。
type GetRuntimeGatesReq struct {
	g.Meta `path:"/settings/v1/runtime-gates" method:"get" summary:"获取 Runtime Gates"`
}

// RuntimeGateVector 是精确九项 canonical Gate 的 HTTP 值对象。
type RuntimeGateVector map[string]bool

// GetRuntimeGatesRes 返回静态上限、动态开关与当前 effective 值。
type GetRuntimeGatesRes struct {
	StaticCaps       RuntimeGateVector `json:"static_caps"`
	DynamicCaps      RuntimeGateVector `json:"dynamic_caps"`
	CurrentEffective RuntimeGateVector `json:"current_effective"`
}

// SaveRuntimeGatesReq 提交完整动态 Gate 向量及审计理由。
type SaveRuntimeGatesReq struct {
	g.Meta      `path:"/settings/v1/runtime-gates" method:"post" summary:"修改 Runtime Gates"`
	DynamicCaps RuntimeGateVector `json:"dynamic_caps"`
	Reason      string            `json:"reason"`
}

// SaveRuntimeGatesRes 表示动态 Gate 已原子更新并写入审计。
type SaveRuntimeGatesRes struct{}

// ListRuntimeGateAuditReq 请求最近的 Runtime Gate 变更记录。
type ListRuntimeGateAuditReq struct {
	g.Meta `path:"/settings/v1/runtime-gates/audit" method:"get" summary:"查询 Runtime Gate 审计"`
	Limit  int `json:"limit" d:"100"`
}

// RuntimeGateAuditItem 是一次动态 Gate 变更的脱敏审计事实。
type RuntimeGateAuditItem struct {
	ActorID   string            `json:"actor_id"`
	OldValues RuntimeGateVector `json:"old_values"`
	NewValues RuntimeGateVector `json:"new_values"`
	Reason    string            `json:"reason"`
	ChangedAt time.Time         `json:"changed_at"`
}

// ListRuntimeGateAuditRes 返回按时间倒序排列的 Gate 审计列表。
type ListRuntimeGateAuditRes struct {
	Items []RuntimeGateAuditItem `json:"items"`
}
