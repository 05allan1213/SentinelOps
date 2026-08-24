// Package term_mapping 提供术语规则管理业务逻辑。
package term_mapping

import (
	"context"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/rule"
	dao "SentinelOps/internal/dao/mysql"
)

// TermMappingItem 术语规则项
type TermMappingItem struct {
	ID         uint
	SourceTerm string
	TargetTerm string
	Priority   int
	Enabled    bool
	CreatedAt  string
	UpdatedAt  string
}

// ListTermMappings 获取规则列表
func ListTermMappings(ctx context.Context) ([]TermMappingItem, error) {
	records, err := dao.ListTermMappings(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]TermMappingItem, 0, len(records))
	for _, r := range records {
		items = append(items, TermMappingItem{
			ID:         r.ID,
			SourceTerm: r.SourceTerm,
			TargetTerm: r.TargetTerm,
			Priority:   r.Priority,
			Enabled:    r.Enabled,
			CreatedAt:  r.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:  r.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return items, nil
}

// CreateTermMapping 创建规则，写入后自动热重载
func CreateTermMapping(ctx context.Context, sourceTerm, targetTerm string, priority int, enabled bool) (uint, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return 0, err
	}
	m := &dao.QueryTermMapping{
		SourceTerm: sourceTerm,
		TargetTerm: targetTerm,
		Priority:   priority,
		Enabled:    enabled,
	}
	if err := dao.CreateTermMapping(ctx, m); err != nil {
		return 0, err
	}
	rule.ReloadTermMappings(ctx)
	return m.ID, nil
}

// UpdateTermMapping 更新规则，写入后自动热重载
func UpdateTermMapping(ctx context.Context, id uint, targetTerm string, priority int, enabled bool) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	updates := map[string]any{
		"target_term": targetTerm,
		"priority":    priority,
		"enabled":     enabled,
	}
	if err := dao.UpdateTermMapping(ctx, id, updates); err != nil {
		return err
	}
	rule.ReloadTermMappings(ctx)
	return nil
}

// DeleteTermMapping 删除规则，写入后自动热重载
func DeleteTermMapping(ctx context.Context, id uint) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	if err := dao.DeleteTermMapping(ctx, id); err != nil {
		return err
	}
	rule.ReloadTermMappings(ctx)
	return nil
}

// ReloadTermMappings 手动触发进程内规则缓存热重载
func ReloadTermMappings(ctx context.Context) (int, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return 0, err
	}
	return rule.ReloadTermMappings(ctx), nil
}
