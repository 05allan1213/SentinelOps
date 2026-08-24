package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// ListReq 报告列表
type ListReq struct {
	g.Meta `path:"/report/v1/list" method:"get" summary:"报告列表"`
	Type   string `json:"type"`
	Limit  int    `json:"limit" d:"20"`
	Offset int    `json:"offset" d:"0"`
}

// ListRes 报告列表响应
type ListRes struct {
	Total   int64        `json:"total"`
	Reports []ReportItem `json:"reports"`
}

// ReportItem 报告项
type ReportItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// CreateReq 创建报告
type CreateReq struct {
	g.Meta  `path:"/report/v1/create" method:"post" summary:"创建报告"`
	Title   string `json:"title" v:"required"`
	Content string `json:"content"`
	Type    string `json:"type" d:"custom"`
}

// CreateRes 创建响应
type CreateRes struct {
	ID string `json:"id"`
}

// TemplateListReq 模板列表
type TemplateListReq struct {
	g.Meta `path:"/report/v1/template/list" method:"get" summary:"报告模板列表"`
	Type   string `json:"type"`
}

// TemplateListRes 模板列表响应
type TemplateListRes struct {
	Templates []TemplateItem `json:"templates"`
}

// TemplateItem 模板项
type TemplateItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// TemplateCreateReq 创建模板
type TemplateCreateReq struct {
	g.Meta  `path:"/report/v1/template/create" method:"post" summary:"创建报告模板"`
	Name    string `json:"name" v:"required"`
	Type    string `json:"type" d:"custom"`
	Content string `json:"content"`
}

// TemplateCreateRes 创建模板响应
type TemplateCreateRes struct {
	ID string `json:"id"`
}

// TemplateDeleteReq 删除模板
type TemplateDeleteReq struct {
	g.Meta `path:"/report/v1/template/delete" method:"post" summary:"删除报告模板"`
	ID     string `json:"id" v:"required"`
}

// TemplateDeleteRes 删除响应
type TemplateDeleteRes struct{}

// GetReq 获取单个报告
type GetReq struct {
	g.Meta `path:"/report/v1/get" method:"get" summary:"获取报告详情"`
	ID     string `json:"id" v:"required"`
}

// GetRes 获取单个报告响应
type GetRes struct {
	Report ReportItem `json:"report"`
}

// DeleteReq 删除安全报告
type DeleteReq struct {
	g.Meta `path:"/report/v1/delete" method:"post" summary:"删除安全报告"`
	ID     string `json:"id" v:"required"`
}

// DeleteRes 删除响应
type DeleteRes struct{}
