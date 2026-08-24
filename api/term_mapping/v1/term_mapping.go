package v1

import "github.com/gogf/gf/v2/frame/g"

// TermMappingItem 术语映射规则响应条目
type TermMappingItem struct {
	ID         uint   `json:"id"`
	SourceTerm string `json:"source_term"`
	TargetTerm string `json:"target_term"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// ListReq 获取规则列表
type ListReq struct {
	g.Meta `path:"/term_mapping/v1/list" method:"get" summary:"获取术语规则列表"`
}

// ListRes 规则列表响应
type ListRes struct {
	Items []TermMappingItem `json:"items"`
	Total int               `json:"total"`
}

// CreateReq 创建规则
type CreateReq struct {
	g.Meta     `path:"/term_mapping/v1/create" method:"post" summary:"创建术语规则"`
	SourceTerm string `json:"source_term" v:"required|max-length:128"`
	TargetTerm string `json:"target_term" v:"required|max-length:256"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
}

// CreateRes 创建响应
type CreateRes struct {
	ID uint `json:"id"`
}

// UpdateReq 更新规则
type UpdateReq struct {
	g.Meta     `path:"/term_mapping/v1/update" method:"post" summary:"更新术语规则"`
	ID         uint   `json:"id" v:"required"`
	TargetTerm string `json:"target_term" v:"required|max-length:256"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
}

// UpdateRes 更新响应
type UpdateRes struct{}

// DeleteReq 删除规则
type DeleteReq struct {
	g.Meta `path:"/term_mapping/v1/delete" method:"post" summary:"删除术语规则"`
	ID     uint `json:"id" v:"required"`
}

// DeleteRes 删除响应
type DeleteRes struct{}

// ReloadReq 热重载进程内规则缓存
type ReloadReq struct {
	g.Meta `path:"/term_mapping/v1/reload" method:"post" summary:"热重载术语规则缓存"`
}

// ReloadRes 热重载响应
type ReloadRes struct {
	Count int `json:"count"`
}
