// Package eventsvc 提供安全事件业务逻辑。
// 职责：事件查询、手动创建，不含 HTTP 层细节。
package eventsvc

import (
	"context"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/pipeline"

	"github.com/google/uuid"
)

// List 查询事件列表，limit <= 0 时兜底为 20 条。返回事件列表和总记录数。
func List(ctx context.Context, limit, offset int, severity, status, keyword, orderBy, orderDir string) ([]dao.Event, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return dao.ListEvents(ctx, limit, offset, severity, status, keyword, orderBy, orderDir)
}

// Stats 查询事件统计：总数、今日新增、高危数量、按严重程度分组。
func Stats(ctx context.Context) (*dao.EventStats, error) {
	return dao.GetEventStats(ctx)
}

// Trend 查询最近 days 天的事件趋势（按日期分组）。
func Trend(ctx context.Context, days int) ([]dao.EventTrendItem, error) {
	return dao.GetEventTrend(ctx, days)
}

// UpdateStatus 更新单条事件状态。
func UpdateStatus(ctx context.Context, id, status string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	return dao.UpdateEventStatus(ctx, id, status)
}

// BatchDelete 批量软删除安全事件。
func BatchDelete(ctx context.Context, ids []string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	return dao.BatchDeleteEvents(ctx, ids)
}

// BatchUpdateStatus 批量更新事件状态。
func BatchUpdateStatus(ctx context.Context, ids []string, status string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	return dao.BatchUpdateEventStatus(ctx, ids, status)
}

// Delete 软删除安全事件（保留历史数据）。
func Delete(ctx context.Context, id string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	return dao.DeleteEvent(ctx, id)
}

// Create 手动创建安全事件，severity 为空时默认 "medium"。
// riskScore 为 0 时根据 severity 自动推算，避免调用方重复实现映射逻辑。
// 返回新事件的 ID。
func Create(ctx context.Context, title, content, severity, source, cveID string, riskScore float64) (string, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return "", err
	}
	if severity == "" {
		severity = "medium"
	}
	// risk_score 未传时按 severity 映射（critical=9.0/high=7.0/medium=5.0/low=3.0）
	if riskScore == 0 {
		riskScore = pipeline.SeverityToRiskScore(severity)
	}
	e := &dao.Event{
		ID:        uuid.New().String(),
		Title:     title,
		Content:   content,
		EventType: "manual", // 手动创建的事件来源标识，区别于 rss/github/web
		Severity:  severity,
		Source:    source,
		Status:    "new", // 手动创建的事件默认待处理
		CVEID:     cveID,
		RiskScore: riskScore,
	}
	if err := dao.CreateEvent(ctx, e); err != nil {
		return "", err
	}
	// 手动创建的事件异步向量索引（不阻塞 HTTP 响应）
	pipeline.IndexDocumentsAsync(ctx, []dao.Event{*e})
	return e.ID, nil
}
