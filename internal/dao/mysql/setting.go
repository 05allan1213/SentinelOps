package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	RetentionPayloadDaysKey = "retention.payload_days"
	RetentionAuditDaysKey   = "retention.audit_days"
	retentionAuditPrefix    = "audit.retention."
)

// RetentionPolicyChange 是 settings 事务写入的完整 admin 审计事实。
type RetentionPolicyChange struct {
	ActorID        string    `json:"actor_id"`
	OldPayloadDays int       `json:"old_payload_days"`
	OldAuditDays   int       `json:"old_audit_days"`
	NewPayloadDays int       `json:"new_payload_days"`
	NewAuditDays   int       `json:"new_audit_days"`
	Reason         string    `json:"reason"`
	ChangedAt      time.Time `json:"changed_at"`
}

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

// UpdateRetentionPolicyWithAudit 在同一事务更新两项保留期并追加不可覆盖的审计 key。
func UpdateRetentionPolicyWithAudit(ctx context.Context, change RetentionPolicyChange) error {
	if globalDB == nil {
		return fmt.Errorf("database not initialized")
	}
	if strings.TrimSpace(change.ActorID) == "" || strings.TrimSpace(change.Reason) == "" {
		return fmt.Errorf("retention actor and reason are required")
	}
	change.Reason = policy.NewRedactor().RedactText(change.Reason)
	change.ChangedAt = time.Now().UTC()
	encoded, err := json.Marshal(change)
	if err != nil {
		return err
	}
	auditKey := retentionAuditPrefix + change.ChangedAt.Format("20060102T150405.000000000Z") + "." + uuid.NewString()
	return globalDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for key, value := range map[string]string{
			RetentionPayloadDaysKey: fmt.Sprintf("%d", change.NewPayloadDays),
			RetentionAuditDaysKey:   fmt.Sprintf("%d", change.NewAuditDays),
		} {
			if err := tx.Save(&Setting{Key: key, Value: value}).Error; err != nil {
				return err
			}
		}
		return tx.Create(&Setting{Key: auditKey, Value: string(encoded)}).Error
	})
}

// ListRetentionPolicyAudit 按时间倒序返回可查询的保留期变更记录。
func ListRetentionPolicyAudit(ctx context.Context, limit int) ([]RetentionPolicyChange, error) {
	if globalDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("retention audit limit must be between 1 and 1000")
	}
	var rows []Setting
	if err := globalDB.WithContext(ctx).Where("`key` LIKE ?", retentionAuditPrefix+"%").Order("`key` DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]RetentionPolicyChange, 0, len(rows))
	for _, row := range rows {
		var record RetentionPolicyChange
		if err := json.Unmarshal([]byte(row.Value), &record); err != nil {
			return nil, fmt.Errorf("decode retention audit %s: %w", row.Key, err)
		}
		records = append(records, record)
	}
	return records, nil
}
