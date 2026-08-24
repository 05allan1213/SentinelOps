package mysql

import (
	"context"
)

// TermMappingRow 用于进程内缓存和 rewrite 包读取，避免循环依赖
type TermMappingRow struct {
	ID         uint
	SourceTerm string
	TargetTerm string
	Priority   int
	Enabled    bool
}

// LoadTermMappings 加载所有启用规则（启动/热重载时调用）
func LoadTermMappings(ctx context.Context) ([]TermMappingRow, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var records []QueryTermMapping
	if err := db.Where("enabled = ?", true).Order("priority DESC, id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	rows := make([]TermMappingRow, 0, len(records))
	for _, r := range records {
		rows = append(rows, TermMappingRow{
			ID:         r.ID,
			SourceTerm: r.SourceTerm,
			TargetTerm: r.TargetTerm,
			Priority:   r.Priority,
			Enabled:    r.Enabled,
		})
	}
	return rows, nil
}

// ListTermMappings 分页查询所有规则（含禁用），供管理 API 使用
func ListTermMappings(ctx context.Context) ([]QueryTermMapping, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var records []QueryTermMapping
	if err := db.Order("priority DESC, id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// CreateTermMapping 创建规则
func CreateTermMapping(ctx context.Context, m *QueryTermMapping) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(m).Error
}

// UpdateTermMapping 更新规则（按 ID）
func UpdateTermMapping(ctx context.Context, id uint, updates map[string]any) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&QueryTermMapping{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteTermMapping 删除规则（按 ID）
func DeleteTermMapping(ctx context.Context, id uint) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Delete(&QueryTermMapping{}, id).Error
}
