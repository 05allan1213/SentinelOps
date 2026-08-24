// Package subscription 提供订阅源管理 HTTP 控制器。
// 职责仅限 HTTP 层：解析请求 → 调用 subsvc → 映射响应 DTO。
// 业务逻辑（类型校验、调度器联动、抓取流水线）已下沉至 internal/service/subscription。
package subscription

import (
	"context"

	v1 "SentinelOps/api/subscription/v1"
	subsvc "SentinelOps/internal/service/subscription"
)

type ControllerV1 struct{}

func NewV1() *ControllerV1 {
	return &ControllerV1{}
}

// List 返回订阅列表，委托 subsvc.List 查询，结果映射为 API DTO。
// 数据库不可用时返回空列表而非报错，保证前端不崩溃。
func (c *ControllerV1) List(ctx context.Context, req *v1.ListReq) (*v1.ListRes, error) {
	subs, counts, err := subsvc.List(ctx, req.Enabled)
	if err != nil {
		return &v1.ListRes{Subscriptions: []v1.SubscriptionItem{}}, nil
	}
	items := make([]v1.SubscriptionItem, 0, len(subs))
	for i, s := range subs {
		lastFetchAt := ""
		if s.LastFetchAt != nil {
			lastFetchAt = s.LastFetchAt.Format("2006-01-02T15:04:05Z07:00")
		}
		items = append(items, v1.SubscriptionItem{
			ID:          s.ID,
			Name:        s.Name,
			URL:         s.URL,
			Type:        s.Type,
			Enabled:     s.Enabled,
			CronExpr:    s.CronExpr,
			LastFetchAt: lastFetchAt,
			TotalEvents: counts[i],
			CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:   s.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return &v1.ListRes{Subscriptions: items}, nil
}

// Create 创建订阅，委托 subsvc.Create 处理类型校验和调度器注册。
func (c *ControllerV1) Create(ctx context.Context, req *v1.CreateReq) (*v1.CreateRes, error) {
	id, err := subsvc.Create(ctx, req.Name, req.URL, req.Type, req.CronExpr)
	if err != nil {
		return nil, err
	}
	return &v1.CreateRes{ID: id}, nil
}

// Update 更新订阅，委托 subsvc.Update 处理局部更新和调度器刷新。
func (c *ControllerV1) Update(ctx context.Context, req *v1.UpdateReq) (*v1.UpdateRes, error) {
	if err := subsvc.Update(ctx, req.ID, req.Name, req.URL, req.Type, req.CronExpr); err != nil {
		return nil, err
	}
	return &v1.UpdateRes{}, nil
}

// Delete 删除订阅，委托 subsvc.Delete 处理软删除和调度器注销。
func (c *ControllerV1) Delete(ctx context.Context, req *v1.DeleteReq) (*v1.DeleteRes, error) {
	if err := subsvc.Delete(ctx, req.ID); err != nil {
		return nil, err
	}
	return &v1.DeleteRes{}, nil
}

// Pause 暂停订阅，委托 subsvc.Pause 处理状态变更和调度器停止。
func (c *ControllerV1) Pause(ctx context.Context, req *v1.PauseReq) (*v1.PauseRes, error) {
	if err := subsvc.Pause(ctx, req.ID); err != nil {
		return nil, err
	}
	return &v1.PauseRes{}, nil
}

// Resume 恢复订阅，委托 subsvc.Resume 处理状态变更和调度器重启。
func (c *ControllerV1) Resume(ctx context.Context, req *v1.ResumeReq) (*v1.ResumeRes, error) {
	if err := subsvc.Resume(ctx, req.ID); err != nil {
		return nil, err
	}
	return &v1.ResumeRes{}, nil
}

// Fetch 手动立即触发指定订阅的单次抓取，同步返回统计数据。
func (c *ControllerV1) Fetch(ctx context.Context, req *v1.FetchReq) (*v1.FetchRes, error) {
	fetchedCount, inserted, totalEvents, durationMs, err := subsvc.FetchNow(ctx, req.ID)
	if err != nil {
		return &v1.FetchRes{Message: err.Error()}, err
	}
	return &v1.FetchRes{
		FetchedCount: fetchedCount,
		NewCount:     inserted,
		TotalEvents:  int(totalEvents),
		DurationMs:   durationMs,
		Message:      "抓取完成",
	}, nil
}
