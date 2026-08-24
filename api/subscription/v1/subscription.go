package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ListReq 订阅列表
type ListReq struct {
	g.Meta  `path:"/subscription/v1/list" method:"get" summary:"订阅列表"`
	Enabled *bool `json:"enabled"`
}

// ListRes 订阅列表响应
type ListRes struct {
	Subscriptions []SubscriptionItem `json:"subscriptions"`
}

// SubscriptionItem 订阅项
type SubscriptionItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Type        string `json:"type"`
	Enabled     bool   `json:"enabled"`
	CronExpr    string `json:"cron_expr"`
	LastFetchAt string `json:"last_fetch_at"`
	TotalEvents int64  `json:"total_events"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// CreateReq 创建订阅
type CreateReq struct {
	g.Meta   `path:"/subscription/v1/create" method:"post" summary:"创建订阅"`
	Name     string `json:"name" v:"required"`
	URL      string `json:"url" v:"required"`
	Type     string `json:"type" d:"rss"`
	CronExpr string `json:"cron_expr"`
}

// CreateRes 创建响应
type CreateRes struct {
	ID string `json:"id"`
}

// UpdateReq 更新订阅
type UpdateReq struct {
	g.Meta   `path:"/subscription/v1/update" method:"post" summary:"更新订阅"`
	ID       string `json:"id" v:"required"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	CronExpr string `json:"cron_expr"`
}

// UpdateRes 更新响应
type UpdateRes struct{}

// DeleteReq 删除订阅
type DeleteReq struct {
	g.Meta `path:"/subscription/v1/delete" method:"post" summary:"删除订阅"`
	ID     string `json:"id" v:"required"`
}

// DeleteRes 删除响应
type DeleteRes struct{}

// PauseReq 暂停订阅
type PauseReq struct {
	g.Meta `path:"/subscription/v1/pause" method:"post" summary:"暂停订阅"`
	ID     string `json:"id" v:"required"`
}

// PauseRes 暂停响应
type PauseRes struct{}

// ResumeReq 恢复订阅
type ResumeReq struct {
	g.Meta `path:"/subscription/v1/resume" method:"post" summary:"恢复订阅"`
	ID     string `json:"id" v:"required"`
}

// ResumeRes 恢复响应
type ResumeRes struct{}

type FetchReq struct {
	g.Meta `path:"/subscription/v1/fetch" method:"post" summary:"手动触发抓取"`
	ID     string `json:"id" v:"required"`
}

// FetchRes 抓取响应
type FetchRes struct {
	FetchedCount int    `json:"fetched_count"`
	NewCount     int    `json:"new_count"`
	TotalEvents  int    `json:"total_events"`
	DurationMs   int64  `json:"duration_ms"`
	Message      string `json:"message"`
}
