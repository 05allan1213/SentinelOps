package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ListReq 事件列表
type ListReq struct {
	g.Meta   `path:"/event/v1/list" method:"get" summary:"事件列表"`
	Severity string `json:"severity" v:""`
	Status   string `json:"status" v:""`
	Keyword  string `json:"keyword" v:""`
	OrderBy  string `json:"order_by" v:""`  // severity, status, source, created_at
	OrderDir string `json:"order_dir" v:""` // asc, desc
	Limit    int    `json:"limit" d:"20"`
	Offset   int    `json:"offset" d:"0"`
}

// ListRes 事件列表响应
type ListRes struct {
	Total  int64       `json:"total"`
	Events []EventItem `json:"events"`
}

// EventItem 事件项
type EventItem struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	EventType string  `json:"event_type,omitempty"`
	Severity  string  `json:"severity"`
	Source    string  `json:"source"`
	SourceURL string  `json:"source_url,omitempty"`
	Status    string  `json:"status"`
	CVEID     string  `json:"cve_id,omitempty"`
	RiskScore float64 `json:"risk_score,omitempty"`
	CreatedAt string  `json:"created_at"`
}

// CreateReq 创建事件
type CreateReq struct {
	g.Meta    `path:"/event/v1/create" method:"post" summary:"创建事件"`
	Title     string  `json:"title" v:"required"`
	Content   string  `json:"content"`
	Severity  string  `json:"severity" d:"medium"`
	Source    string  `json:"source"`
	CVEID     string  `json:"cve_id"`
	RiskScore float64 `json:"risk_score"`
}

// CreateRes 创建响应
type CreateRes struct {
	ID string `json:"id"`
}

// StatsReq 事件统计
type StatsReq struct {
	g.Meta `path:"/event/v1/stats" method:"get" summary:"事件统计"`
}

// StatsRes 事件统计响应
type StatsRes struct {
	Total         int64            `json:"total"`
	TodayCount    int64            `json:"today_count"`
	CriticalCount int64            `json:"critical_count"`
	BySeverity    map[string]int64 `json:"by_severity"`
	New7Days      int64            `json:"new_7days"` // 近7天新增
	Pending       int64            `json:"pending"`   // 待处置（status='new'）
}

// TrendReq 事件趋势
type TrendReq struct {
	g.Meta `path:"/event/v1/trend" method:"get" summary:"事件趋势"`
	Days   int `json:"days" d:"30"`
}

// TrendItem 单日事件数量
type TrendItem struct {
	Date     string `json:"date"`
	Total    int64  `json:"total"`
	Critical int64  `json:"critical"`
	High     int64  `json:"high"`
	Medium   int64  `json:"medium"`
	Low      int64  `json:"low"`
}

// TrendRes 事件趋势响应
type TrendRes struct {
	Items []TrendItem `json:"items"`
}

// UpdateStatusReq 更新事件状态
type UpdateStatusReq struct {
	g.Meta `path:"/event/v1/update_status" method:"post" summary:"更新事件状态"`
	ID     string `json:"id" v:"required"`
	Status string `json:"status" v:"required|in:new,processing,resolved,ignored"`
}

// UpdateStatusRes 更新响应
type UpdateStatusRes struct{}

// DeleteReq 删除安全事件
type DeleteReq struct {
	g.Meta `path:"/event/v1/delete" method:"post" summary:"删除安全事件"`
	ID     string `json:"id" v:"required"`
}

// DeleteRes 删除响应
type DeleteRes struct{}

// BatchDeleteReq 批量删除安全事件
type BatchDeleteReq struct {
	g.Meta `path:"/event/v1/batch_delete" method:"post" summary:"批量删除安全事件"`
	IDs    []string `json:"ids" v:"required"`
}

// BatchDeleteRes 批量删除响应
type BatchDeleteRes struct{}

// BatchUpdateStatusReq 批量更新事件状态
type BatchUpdateStatusReq struct {
	g.Meta `path:"/event/v1/batch_update_status" method:"post" summary:"批量更新事件状态"`
	IDs    []string `json:"ids" v:"required"`
	Status string   `json:"status" v:"required|in:new,processing,resolved,ignored"`
}

// BatchUpdateStatusRes 批量更新响应
type BatchUpdateStatusRes struct{}

// AnalyzeSingleStreamReq 单条事件 AI 解决方案（SSE 流式）
type AnalyzeSingleStreamReq struct {
	g.Meta   `path:"/event/v1/analyze/stream" method:"post" summary:"单条事件 AI 解决方案流式"`
	EventID  string `json:"event_id"  v:"required"`
	Title    string `json:"title"     v:"required"`
	Severity string `json:"severity"`
	CVEID    string `json:"cve_id"`
	Source   string `json:"source"`
}

// AnalyzeSingleStreamRes SSE 无 JSON body
type AnalyzeSingleStreamRes struct{}
type PipelineStreamReq struct {
	g.Meta `path:"/event/v1/pipeline/stream" method:"post" summary:"事件分析流式"`
	Query  string `json:"query" v:"required"`
}

// PipelineStreamRes SSE 无 JSON body
type PipelineStreamRes struct{}
