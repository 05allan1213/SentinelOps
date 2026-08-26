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
	"gorm.io/gorm/clause"
)

const (
	RetentionPayloadDaysKey = "retention.payload_days"
	RetentionAuditDaysKey   = "retention.audit_days"
	retentionAuditPrefix    = "audit.retention."
	runtimeGateAuditPrefix  = "audit.runtime-gates."
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

// RuntimeGatePolicyChange 是一次九项动态 Gate 原子更新的审计事实。
type RuntimeGatePolicyChange struct {
	ActorID   string          `json:"actor_id"`
	OldValues map[string]bool `json:"old_values"`
	NewValues map[string]bool `json:"new_values"`
	Reason    string          `json:"reason"`
	ChangedAt time.Time       `json:"changed_at"`
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

// SeedRuntimeGateSettings 仅为调用方给出的 canonical key 创建 fail-closed 默认值。
func SeedRuntimeGateSettings(ctx context.Context, keys []string) {
	if globalDB == nil || len(keys) == 0 {
		return
	}
	rows := make([]Setting, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, Setting{Key: key, Value: "false"})
	}
	_ = globalDB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// UpdateRuntimeGatesWithAudit 在同一事务锁定旧值、更新九项值并追加审计记录。
func UpdateRuntimeGatesWithAudit(
	ctx context.Context,
	keys []string,
	newValues map[string]bool,
	actorID string,
	reason string,
) (RuntimeGatePolicyChange, error) {
	if globalDB == nil {
		return RuntimeGatePolicyChange{}, fmt.Errorf("database not initialized")
	}
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(reason) == "" {
		return RuntimeGatePolicyChange{}, fmt.Errorf("runtime Gate actor and reason are required")
	}
	if len(keys) == 0 || len(newValues) != len(keys) {
		return RuntimeGatePolicyChange{}, fmt.Errorf("runtime Gate update requires one value per key")
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return RuntimeGatePolicyChange{}, fmt.Errorf("runtime Gate key is required")
		}
		if _, duplicate := seen[key]; duplicate {
			return RuntimeGatePolicyChange{}, fmt.Errorf("duplicate runtime Gate key %q", key)
		}
		seen[key] = struct{}{}
		if _, ok := newValues[key]; !ok {
			return RuntimeGatePolicyChange{}, fmt.Errorf("runtime Gate update is missing %s", key)
		}
	}
	for key := range newValues {
		if _, ok := seen[key]; !ok {
			return RuntimeGatePolicyChange{}, fmt.Errorf("runtime Gate update contains unknown key %q", key)
		}
	}

	change := RuntimeGatePolicyChange{
		ActorID: actorID, NewValues: cloneBoolMap(newValues),
		Reason: policy.NewRedactor().RedactText(strings.TrimSpace(reason)), ChangedAt: time.Now().UTC(),
	}
	err := globalDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []Setting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("`key` IN ?", keys).Find(&rows).Error; err != nil {
			return err
		}
		change.OldValues = make(map[string]bool, len(keys))
		for _, key := range keys {
			change.OldValues[key] = false
		}
		for _, row := range rows {
			change.OldValues[row.Key] = row.Value == "true"
		}
		encoded, err := json.Marshal(change)
		if err != nil {
			return err
		}
		for _, key := range keys {
			value := "false"
			if newValues[key] {
				value = "true"
			}
			if err := tx.Save(&Setting{Key: key, Value: value}).Error; err != nil {
				return err
			}
		}
		auditKey := runtimeGateAuditPrefix + change.ChangedAt.Format("20060102T150405.000000000Z") + "." + uuid.NewString()
		return tx.Create(&Setting{Key: auditKey, Value: string(encoded)}).Error
	})
	return change, err
}

// ListRuntimeGateAudit 按时间倒序返回动态 Gate 变更历史。
func ListRuntimeGateAudit(ctx context.Context, limit int) ([]RuntimeGatePolicyChange, error) {
	if globalDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("runtime Gate audit limit must be between 1 and 1000")
	}
	var rows []Setting
	if err := globalDB.WithContext(ctx).Where("`key` LIKE ?", runtimeGateAuditPrefix+"%").Order("`key` DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]RuntimeGatePolicyChange, 0, len(rows))
	for _, row := range rows {
		var record RuntimeGatePolicyChange
		if err := json.Unmarshal([]byte(row.Value), &record); err != nil {
			return nil, fmt.Errorf("decode runtime Gate audit %s: %w", row.Key, err)
		}
		records = append(records, record)
	}
	return records, nil
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

func cloneBoolMap(input map[string]bool) map[string]bool {
	result := make(map[string]bool, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
