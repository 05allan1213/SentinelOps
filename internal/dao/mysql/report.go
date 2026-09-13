package mysql

import (
	"context"

	"gorm.io/gorm"
)

// ListReports 查询报告列表，按创建时间倒序，支持分页（limit+offset）。
// 同时返回符合过滤条件的总记录数。
func ListReports(ctx context.Context, limit, offset int, reportType string) ([]Report, int64, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, 0, err
	}
	q := db.Model(&Report{})
	if reportType != "" {
		q = q.Where("type = ?", reportType)
	}
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var reports []Report
	if err = q.Limit(limit).Offset(offset).Order("created_at DESC").Find(&reports).Error; err != nil {
		return nil, 0, err
	}
	return reports, total, nil
}

// CreateReport 将报告持久化到数据库。
func CreateReport(ctx context.Context, r *Report) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(r).Error
}

// GetReportByID 按主键查询单条报告。
func GetReportByID(ctx context.Context, id string) (*Report, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var r Report
	if err = db.Where("id = ?", id).First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteReport 软删除安全报告（设置 deleted_at 字段）。
// 目标不存在时必须返回 gorm.ErrRecordNotFound，HTTP 层据此返回 404，
// 避免调用方把「没有删除任何记录」当成删除成功。
func DeleteReport(ctx context.Context, id string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	result := db.Where("id = ?", id).Delete(&Report{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
