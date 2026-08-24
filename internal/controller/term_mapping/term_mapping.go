// Package term_mapping 提供术语规则管理 HTTP 控制器。
package term_mapping

import (
	"context"

	v1 "SentinelOps/api/term_mapping/v1"
	"SentinelOps/internal/service/term_mapping"

	"github.com/gogf/gf/v2/frame/g"
)

// ControllerV1 术语规则管理控制器
type ControllerV1 struct{}

// NewV1 返回控制器实例
func NewV1() *ControllerV1 { return &ControllerV1{} }

// List 获取规则列表
func (c *ControllerV1) List(ctx context.Context, _ *v1.ListReq) (*v1.ListRes, error) {
	items, err := term_mapping.ListTermMappings(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "[TermMapping] 查询规则列表失败: %v", err)
		return &v1.ListRes{Items: []v1.TermMappingItem{}, Total: 0}, nil
	}
	apiItems := make([]v1.TermMappingItem, 0, len(items))
	for _, item := range items {
		apiItems = append(apiItems, v1.TermMappingItem{
			ID:         item.ID,
			SourceTerm: item.SourceTerm,
			TargetTerm: item.TargetTerm,
			Priority:   item.Priority,
			Enabled:    item.Enabled,
			CreatedAt:  item.CreatedAt,
			UpdatedAt:  item.UpdatedAt,
		})
	}
	return &v1.ListRes{Items: apiItems, Total: len(apiItems)}, nil
}

// Create 创建规则，写入后自动热重载
func (c *ControllerV1) Create(ctx context.Context, req *v1.CreateReq) (*v1.CreateRes, error) {
	id, err := term_mapping.CreateTermMapping(ctx, req.SourceTerm, req.TargetTerm, req.Priority, req.Enabled)
	if err != nil {
		return nil, err
	}
	return &v1.CreateRes{ID: id}, nil
}

// Update 更新规则，写入后自动热重载
func (c *ControllerV1) Update(ctx context.Context, req *v1.UpdateReq) (*v1.UpdateRes, error) {
	if err := term_mapping.UpdateTermMapping(ctx, req.ID, req.TargetTerm, req.Priority, req.Enabled); err != nil {
		return nil, err
	}
	return &v1.UpdateRes{}, nil
}

// Delete 删除规则，写入后自动热重载
func (c *ControllerV1) Delete(ctx context.Context, req *v1.DeleteReq) (*v1.DeleteRes, error) {
	if err := term_mapping.DeleteTermMapping(ctx, req.ID); err != nil {
		return nil, err
	}
	return &v1.DeleteRes{}, nil
}

// Reload 手动触发进程内规则缓存热重载
func (c *ControllerV1) Reload(ctx context.Context, _ *v1.ReloadReq) (*v1.ReloadRes, error) {
	n := term_mapping.ReloadTermMappings(ctx)
	return &v1.ReloadRes{Count: n}, nil
}
