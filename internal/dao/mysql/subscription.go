package mysql

import (
	"context"
	"time"
)

// ListSubscriptions 查询订阅列表，按创建时间倒序。
// enabled 为 nil 时不过滤状态，返回所有订阅。
func ListSubscriptions(ctx context.Context, enabled *bool) ([]Subscription, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var subs []Subscription
	q := db.Order("created_at DESC")
	if enabled != nil {
		q = q.Where("enabled = ?", *enabled)
	}
	if err = q.Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

// CountEventsBySource 按来源名称统计关联事件数量。
func CountEventsBySource(ctx context.Context, source string) (int64, error) {
	db, err := DB(ctx)
	if err != nil {
		return 0, err
	}
	var count int64
	err = db.Model(&Event{}).Where("source = ?", source).Count(&count).Error
	return count, err
}

// CreateSubscription 将订阅持久化到数据库。
func CreateSubscription(ctx context.Context, sub *Subscription) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(sub).Error
}

// UpdateSubscription 局部更新订阅，只更新 updates map 中包含的字段。
func UpdateSubscription(ctx context.Context, id string, updates map[string]interface{}) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&Subscription{}).Where("id = ?", id).Updates(updates).Error
}

// FindByID 按 ID 查询订阅（含软删除过滤）。
func FindByID(ctx context.Context, id string) (*Subscription, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var sub Subscription
	if err = db.Where("id = ?", id).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// FindEnabledByID 查询指定 ID 且处于启用状态的订阅，已暂停时返回 error。
func FindEnabledByID(ctx context.Context, id string) (*Subscription, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var sub Subscription
	if err = db.Where("id = ? AND enabled = ?", id, true).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// SetEnabled 更新订阅的 enabled 状态字段。
func SetEnabled(ctx context.Context, id string, enabled bool) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&Subscription{}).Where("id = ?", id).Update("enabled", enabled).Error
}

// UpdateLastFetchAt 更新订阅的最后抓取时间。
func UpdateLastFetchAt(ctx context.Context, id string, t time.Time) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&Subscription{}).Where("id = ?", id).Update("last_fetch_at", t).Error
}

// DeleteSubscription 软删除订阅（设置 deleted_at，后续查询自动过滤）。
func DeleteSubscription(ctx context.Context, id string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Where("id = ?", id).Delete(&Subscription{}).Error
}
