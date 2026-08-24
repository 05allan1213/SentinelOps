package mysql

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// GetSetting 读取单个配置项，不存在时返回空字符串
func GetSetting(ctx context.Context, key string) (string, error) {
	if globalDB == nil {
		return "", nil
	}
	var s Setting
	if err := globalDB.WithContext(ctx).First(&s, "`key` = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return s.Value, nil
}

// SetSetting 写入配置项（upsert）
func SetSetting(ctx context.Context, key, value string) error {
	if globalDB == nil {
		return nil
	}
	return globalDB.WithContext(ctx).Save(&Setting{Key: key, Value: value}).Error
}

// GetSettings 批量读取配置项
func GetSettings(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string)
	if globalDB == nil {
		return result, nil
	}
	var rows []Setting
	if err := globalDB.WithContext(ctx).Where("`key` IN ?", keys).Find(&rows).Error; err != nil {
		return result, err
	}
	for _, r := range rows {
		result[r.Key] = r.Value
	}
	return result, nil
}
