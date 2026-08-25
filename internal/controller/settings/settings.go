// Package settings 提供系统通用设置 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求 → 调用 settingssvc → 映射响应 DTO。
// 业务逻辑（默认值处理、bool ↔ string 转换）已下沉至 internal/service/settings。
package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	v1 "SentinelOps/api/settings/v1"
	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"
	settingssvc "SentinelOps/internal/service/settings"
)

type ControllerV1 struct{}

func NewV1() *ControllerV1 { return &ControllerV1{} }

// GetGeneral 读取通用设置，DB 无值时返回默认值。
func (c *ControllerV1) GetGeneral(ctx context.Context, _ *v1.GetGeneralReq) (*v1.GetGeneralRes, error) {
	s, err := settingssvc.GetGeneral(ctx)
	if err != nil {
		return nil, err
	}
	return &v1.GetGeneralRes{
		Settings: v1.GeneralSettings{
			SiteName:     s.SiteName,
			AutoMarkRead: s.AutoMarkRead,
		},
	}, nil
}

// SaveGeneral 持久化通用设置到 DB。
func (c *ControllerV1) SaveGeneral(ctx context.Context, req *v1.SaveGeneralReq) (*v1.SaveGeneralRes, error) {
	if err := settingssvc.SaveGeneral(ctx, req.SiteName, req.AutoMarkRead); err != nil {
		return nil, err
	}
	return &v1.SaveGeneralRes{}, nil
}

// GetIngestKey 获取告警接入 API Key。
func (c *ControllerV1) GetIngestKey(ctx context.Context, _ *v1.GetIngestKeyReq) (*v1.GetIngestKeyRes, error) {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return nil, err
	}
	key, err := dao.GetSetting(ctx, "ingest.api_key")
	if err != nil {
		return nil, err
	}
	return &v1.GetIngestKeyRes{APIKey: key}, nil
}

// ResetIngestKey 重新生成告警接入 API Key。
func (c *ControllerV1) ResetIngestKey(ctx context.Context, _ *v1.ResetIngestKeyReq) (*v1.ResetIngestKeyRes, error) {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return nil, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	newKey := "sk-ingest-" + hex.EncodeToString(b)
	if err := dao.SetSetting(ctx, "ingest.api_key", newKey); err != nil {
		return nil, err
	}
	return &v1.ResetIngestKeyRes{APIKey: newKey}, nil
}

func (c *ControllerV1) GetRetention(ctx context.Context, _ *v1.GetRetentionReq) (*v1.GetRetentionRes, error) {
	settings, err := settingssvc.GetRetention(ctx)
	if err != nil {
		return nil, err
	}
	return &v1.GetRetentionRes{Settings: v1.RetentionSettings{PayloadDays: settings.PayloadDays, AuditDays: settings.AuditDays}}, nil
}

func (c *ControllerV1) SaveRetention(ctx context.Context, req *v1.SaveRetentionReq) (*v1.SaveRetentionRes, error) {
	err := settingssvc.SaveRetention(ctx, settingssvc.RetentionSettings{PayloadDays: req.PayloadDays, AuditDays: req.AuditDays}, req.Reason)
	if err != nil {
		return nil, err
	}
	return &v1.SaveRetentionRes{}, nil
}

func (c *ControllerV1) ListRetentionAudit(ctx context.Context, req *v1.ListRetentionAuditReq) (*v1.ListRetentionAuditRes, error) {
	records, err := settingssvc.ListRetentionAudit(ctx, req.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]v1.RetentionAuditItem, 0, len(records))
	for _, record := range records {
		items = append(items, v1.RetentionAuditItem{
			ActorID: record.ActorID, OldPayloadDays: record.OldPayloadDays, OldAuditDays: record.OldAuditDays,
			NewPayloadDays: record.NewPayloadDays, NewAuditDays: record.NewAuditDays,
			Reason: record.Reason, ChangedAt: record.ChangedAt,
		})
	}
	return &v1.ListRetentionAuditRes{Items: items}, nil
}
