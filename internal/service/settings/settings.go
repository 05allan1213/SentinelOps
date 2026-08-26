// Package settingssvc 提供系统通用设置业务逻辑。
// 职责：读取/写入 key-value 配置，封装默认值处理和类型转换，不含 HTTP 层细节。
package settingssvc

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
)

// GeneralSettings 通用设置的业务模型（与 HTTP DTO 解耦）。
type GeneralSettings struct {
	SiteName     string
	AutoMarkRead bool
}

// RetentionSettings 是当前动态物理保留期。
type RetentionSettings struct {
	PayloadDays int
	AuditDays   int
}

// RuntimeGateSettings 返回静态、动态与当前 effective 三层精确向量。
type RuntimeGateSettings struct {
	StaticCaps       airuntime.GateVector
	DynamicCaps      airuntime.GateVector
	CurrentEffective airuntime.GateVector
}

// ValidateRuntimeGateUpdate 拒绝缺失、未知或旧别名并返回值对象。
func ValidateRuntimeGateUpdate(values map[string]bool) (airuntime.GateVector, error) {
	return airuntime.NewGateVector(values)
}

// GetRuntimeGates 只允许 admin 查询当前三层 Gate。
func GetRuntimeGates(ctx context.Context, config *appconfig.Config) (RuntimeGateSettings, error) {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return RuntimeGateSettings{}, err
	}
	evaluator, err := airuntime.NewGateEvaluator(airuntime.StaticGateCaps(config), dao.GetSettings)
	if err != nil {
		return RuntimeGateSettings{}, err
	}
	state, err := evaluator.CurrentState(ctx)
	if err != nil {
		return RuntimeGateSettings{}, err
	}
	return RuntimeGateSettings{
		StaticCaps:       state.StaticCaps,
		DynamicCaps:      state.DynamicCaps,
		CurrentEffective: state.CurrentEffective,
	}, nil
}

// SaveRuntimeGates 原子写入完整动态向量并记录 actor、旧值、新值、理由与时间。
func SaveRuntimeGates(ctx context.Context, values map[string]bool, reason string) (dao.RuntimeGatePolicyChange, error) {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return dao.RuntimeGatePolicyChange{}, err
	}
	vector, err := ValidateRuntimeGateUpdate(values)
	if err != nil {
		return dao.RuntimeGatePolicyChange{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return dao.RuntimeGatePolicyChange{}, fmt.Errorf("runtime Gate change reason is required")
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return dao.RuntimeGatePolicyChange{}, err
	}
	return dao.UpdateRuntimeGatesWithAudit(ctx, airuntime.CanonicalGateKeys(), vector.Map(), identity.UserID, reason)
}

// ListRuntimeGateAudit 只允许 admin 查询动态 Gate 变更历史。
func ListRuntimeGateAudit(ctx context.Context, limit int) ([]dao.RuntimeGatePolicyChange, error) {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return nil, err
	}
	return dao.ListRuntimeGateAudit(ctx, limit)
}

// GetRetention 读取 30/180 天动态设置；缺失时使用 Spec 默认值。
func GetRetention(ctx context.Context) (RetentionSettings, error) {
	values, err := dao.GetSettings(ctx, []string{dao.RetentionPayloadDaysKey, dao.RetentionAuditDaysKey})
	if err != nil {
		return RetentionSettings{}, err
	}
	settings := RetentionSettings{PayloadDays: 30, AuditDays: 180}
	if value := values[dao.RetentionPayloadDaysKey]; value != "" {
		settings.PayloadDays, err = strconv.Atoi(value)
		if err != nil {
			return RetentionSettings{}, fmt.Errorf("invalid payload retention setting")
		}
	}
	if value := values[dao.RetentionAuditDaysKey]; value != "" {
		settings.AuditDays, err = strconv.Atoi(value)
		if err != nil {
			return RetentionSettings{}, fmt.Errorf("invalid audit retention setting")
		}
	}
	return settings, validateRetentionSettings(settings)
}

// SaveRetention 只允许 admin 修改，并在同一 settings 事务追加审计记录。
func SaveRetention(ctx context.Context, settings RetentionSettings, reason string) error {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return err
	}
	if err := validateRetentionSettings(settings); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("retention change reason is required")
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return err
	}
	current, err := GetRetention(ctx)
	if err != nil {
		return err
	}
	return dao.UpdateRetentionPolicyWithAudit(ctx, dao.RetentionPolicyChange{
		ActorID: identity.UserID, OldPayloadDays: current.PayloadDays, OldAuditDays: current.AuditDays,
		NewPayloadDays: settings.PayloadDays, NewAuditDays: settings.AuditDays, Reason: reason,
	})
}

// ListRetentionAudit 只允许 admin 查询保留期变更历史。
func ListRetentionAudit(ctx context.Context, limit int) ([]dao.RetentionPolicyChange, error) {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return nil, err
	}
	return dao.ListRetentionPolicyAudit(ctx, limit)
}

func validateRetentionSettings(settings RetentionSettings) error {
	if settings.PayloadDays <= 0 || settings.PayloadDays > 3650 {
		return fmt.Errorf("payload retention days must be between 1 and 3650")
	}
	if settings.AuditDays < settings.PayloadDays || settings.AuditDays > 3650 {
		return fmt.Errorf("audit retention days must be between payload retention and 3650")
	}
	return nil
}

var generalKeys = []string{
	"general.site_name",
	"general.auto_mark_read",
}

// GetGeneral 读取通用设置，数据库无值时返回合理默认值。
func GetGeneral(ctx context.Context) (*GeneralSettings, error) {
	cfg, _ := dao.GetSettings(ctx, generalKeys)

	siteName := cfg["general.site_name"]
	if siteName == "" {
		siteName = "安全事件智能研判多智能体协同平台"
	}
	// 默认 true；仅当明确存储 "false" 时才关闭
	autoMarkRead := cfg["general.auto_mark_read"] != "false"

	return &GeneralSettings{
		SiteName:     siteName,
		AutoMarkRead: autoMarkRead,
	}, nil
}

// SaveGeneral 持久化通用设置到数据库。
func SaveGeneral(ctx context.Context, siteName string, autoMarkRead bool) error {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return err
	}
	autoMarkReadStr := "true"
	if !autoMarkRead {
		autoMarkReadStr = "false"
	}
	if err := dao.SetSetting(ctx, "general.site_name", siteName); err != nil {
		return err
	}
	return dao.SetSetting(ctx, "general.auto_mark_read", autoMarkReadStr)
}
