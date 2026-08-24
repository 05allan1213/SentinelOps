// Package report 提供安全报告 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求 → 调用 reportsvc → 映射响应 DTO。
// 业务逻辑（type 默认值、UUID 生成）已下沉至 internal/service/report。
package report

import (
	"context"
	"fmt"

	v1 "SentinelOps/api/report/v1"
	reportsvc "SentinelOps/internal/service/report"
)

type ControllerV1 struct{}

func NewV1() *ControllerV1 {
	return &ControllerV1{}
}

// List 返回报告列表，委托 reportsvc.ListReports 查询，结果映射为 API DTO。
// 数据库不可用时返回空列表而非报错，保证前端不崩溃。
func (c *ControllerV1) List(ctx context.Context, req *v1.ListReq) (*v1.ListRes, error) {
	reports, total, err := reportsvc.ListReports(ctx, req.Limit, req.Offset, req.Type)
	if err != nil {
		return &v1.ListRes{Total: 0, Reports: []v1.ReportItem{}}, nil
	}
	items := make([]v1.ReportItem, 0, len(reports))
	for _, r := range reports {
		items = append(items, v1.ReportItem{
			ID:        r.ID,
			Title:     r.Title,
			Type:      r.Type,
			Content:   r.Content,
			CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return &v1.ListRes{Total: total, Reports: items}, nil
}

// Create 创建报告，委托 reportsvc.CreateReport 处理默认值和入库。
func (c *ControllerV1) Create(ctx context.Context, req *v1.CreateReq) (*v1.CreateRes, error) {
	id, err := reportsvc.CreateReport(ctx, req.Title, req.Content, req.Type)
	if err != nil {
		return nil, err
	}
	return &v1.CreateRes{ID: id}, nil
}

// Get 按 ID 返回单个报告详情。
func (c *ControllerV1) Get(ctx context.Context, req *v1.GetReq) (*v1.GetRes, error) {
	r, err := reportsvc.GetReport(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("报告不存在: %w", err)
	}
	return &v1.GetRes{
		Report: v1.ReportItem{
			ID:        r.ID,
			Title:     r.Title,
			Type:      r.Type,
			Content:   r.Content,
			CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		},
	}, nil
}

// TemplateList 返回内置模板列表，不依赖数据库。
func (c *ControllerV1) TemplateList(ctx context.Context, req *v1.TemplateListReq) (*v1.TemplateListRes, error) {
	templates, err := reportsvc.ListTemplates(ctx, req.Type)
	if err != nil {
		return &v1.TemplateListRes{Templates: []v1.TemplateItem{}}, nil
	}
	items := make([]v1.TemplateItem, 0, len(templates))
	for _, t := range templates {
		items = append(items, v1.TemplateItem{
			ID:        t.ID,
			Name:      t.Name,
			Type:      t.Type,
			Content:   t.Content,
			CreatedAt: "",
		})
	}
	return &v1.TemplateListRes{Templates: items}, nil
}

// TemplateCreate 模板现已内置，不支持通过 API 创建，直接返回不支持错误。
func (c *ControllerV1) TemplateCreate(_ context.Context, _ *v1.TemplateCreateReq) (*v1.TemplateCreateRes, error) {
	return nil, fmt.Errorf("模板已内置，不支持自定义创建")
}

// TemplateDelete 模板现已内置，不支持通过 API 删除，直接返回不支持错误。
func (c *ControllerV1) TemplateDelete(_ context.Context, _ *v1.TemplateDeleteReq) (*v1.TemplateDeleteRes, error) {
	return nil, fmt.Errorf("模板已内置，不支持删除")
}

// Delete 删除安全报告，委托 reportsvc.DeleteReport 执行软删除。
func (c *ControllerV1) Delete(ctx context.Context, req *v1.DeleteReq) (*v1.DeleteRes, error) {
	if err := reportsvc.DeleteReport(ctx, req.ID); err != nil {
		return nil, err
	}
	return &v1.DeleteRes{}, nil
}
