// Package subsvc 提供订阅源管理业务逻辑。
// 职责：订阅 CRUD、启用/暂停/恢复，并与调度器（scheduler）联动。
// 所有写操作执行顺序：先更新数据库，再通知调度器，防止竞态。
package subsvc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	milvus "SentinelOps/internal/dao/milvus"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/pipeline"
	"SentinelOps/internal/service/scheduler"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// List 返回订阅列表，每条附带关联事件总数和最后抓取时间。
// enabled 为 nil 时不过滤状态；数据库不可用时返回空列表，保证前端不崩溃。
func List(ctx context.Context, enabled *bool) ([]dao.Subscription, []int64, error) {
	subs, err := dao.ListSubscriptions(ctx, enabled)
	if err != nil {
		return nil, nil, err
	}
	// 批量统计每条订阅的关联事件数
	counts := make([]int64, len(subs))
	for i, s := range subs {
		counts[i], _ = dao.CountEventsBySource(ctx, s.Name)
	}
	return subs, counts, nil
}

// Create 创建订阅并立即注册调度任务。
// 新订阅默认 enabled=true，CronExpr 为空时调度器使用全局默认间隔。
// 返回新订阅的 ID。
func Create(ctx context.Context, name, url, subType, cronExpr string) (string, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return "", err
	}
	normalizedType, err := normalizeType(subType)
	if err != nil {
		return "", err
	}
	sub := &dao.Subscription{
		ID:       uuid.New().String(),
		Name:     name,
		URL:      url,
		Type:     normalizedType,
		CronExpr: cronExpr,
		Enabled:  true, // 新订阅默认启用
	}
	if err = dao.CreateSubscription(ctx, sub); err != nil {
		return "", err
	}
	// 注册到调度器，立即启动抓取 goroutine
	scheduler.Register(sub)
	return sub.ID, nil
}

// Update 局部更新订阅，仅更新非空字段，已暂停的订阅不自动恢复调度。
func Update(ctx context.Context, id, name, url, subType, cronExpr string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	updates := make(map[string]interface{})
	if name != "" {
		updates["name"] = name
	}
	if url != "" {
		updates["url"] = url
	}
	if subType != "" {
		normalizedType, err := normalizeType(subType)
		if err != nil {
			return err
		}
		updates["type"] = normalizedType
	}
	if cronExpr != "" {
		updates["cron_expr"] = cronExpr
	}
	if len(updates) > 0 {
		if err := dao.UpdateSubscription(ctx, id, updates); err != nil {
			return err
		}
	}
	// 仅对 enabled=true 的订阅刷新调度任务（CronExpr 变更需重建 ticker）
	if sub, err := dao.FindEnabledByID(ctx, id); err == nil {
		scheduler.Register(sub)
	}
	return nil
}

// Delete 删除订阅，并级联清理关联数据：
// 1. 删除 Milvus 中来自该订阅源的所有向量（分批执行，防止表达式过长）
// 2. 软删除 MySQL 中来自该订阅源的事件（源已删除，对应事件无意义保留）
// 3. 软删除订阅本身
// 4. 注销调度任务
// Milvus/事件清理失败不阻断订阅删除，记录 warning 后继续执行。
func Delete(ctx context.Context, id string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	// 先查出订阅信息（需要 Name 作为事件来源标识）
	sub, err := dao.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("查询订阅失败: %w", err)
	}

	// 查询来自该订阅源的已索引事件 ID
	eventIDs, err := dao.ListIndexedEventIDsBySource(ctx, sub.Name)
	if err != nil {
		g.Log().Warningf(ctx, "[Subscription] 查询已索引事件失败，跳过 Milvus 清理: source=%s, err=%v", sub.Name, err)
	} else if len(eventIDs) > 0 {
		// 委托 dao/milvus 层执行分批向量删除
		if err = milvus.DeleteEventsByIDs(ctx, eventIDs); err != nil {
			g.Log().Warningf(ctx, "[Subscription] Milvus 向量删除失败，继续执行: source=%s, count=%d, err=%v", sub.Name, len(eventIDs), err)
		} else {
			g.Log().Infof(ctx, "[Subscription] Milvus 向量删除成功: source=%s, count=%d", sub.Name, len(eventIDs))
		}
	}

	// 软删除 MySQL 中来自该订阅源的事件
	if err = dao.DeleteEventsBySource(ctx, sub.Name); err != nil {
		g.Log().Warningf(ctx, "[Subscription] MySQL 事件软删除失败: source=%s, err=%v", sub.Name, err)
	}

	// 软删除订阅本身
	if err = dao.DeleteSubscription(ctx, id); err != nil {
		return err
	}
	// 注销调度任务，cancel 对应订阅的抓取 goroutine
	scheduler.Unregister(id)
	return nil
}

// Pause 暂停订阅：先将 enabled 置 false，再注销调度器，防止竞态。
func Pause(ctx context.Context, id string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	if err := dao.SetEnabled(ctx, id, false); err != nil {
		return err
	}
	scheduler.Unregister(id)
	return nil
}

// Resume 恢复订阅：先将 enabled 置 true，再注册调度器，确保 doFetch 中的 enabled 检查通过。
func Resume(ctx context.Context, id string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	if err := dao.SetEnabled(ctx, id, true); err != nil {
		return err
	}
	// 重新加载最新配置（含最新 CronExpr）再注册
	sub, err := dao.FindByID(ctx, id)
	if err != nil {
		return err
	}
	scheduler.Register(sub)
	return nil
}

// FetchNow 手动立即触发单次抓取，同步返回统计结果。支持对已暂停订阅手动触发。
func FetchNow(ctx context.Context, id string) (fetchedCount, inserted int, totalEvents int64, durationMs int64, err error) {
	if err = policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return
	}
	start := time.Now()
	sub, err := dao.FindByID(ctx, id)
	if err != nil {
		return
	}
	items, fetchErr := pipeline.Fetch(ctx, sub)
	durationMs = time.Since(start).Milliseconds()
	if fetchErr != nil {
		err = fetchErr
		return
	}
	fetchedCount = len(items)
	events := pipeline.Extract(items)
	newEvents, dedupErr := pipeline.DedupAndInsert(ctx, events)
	if dedupErr != nil {
		err = dedupErr
		return
	}
	inserted = len(newEvents)
	_ = dao.UpdateLastFetchAt(ctx, id, time.Now())
	if len(newEvents) > 0 {
		pipeline.IndexDocumentsAsync(ctx, newEvents)
	}
	totalEvents, _ = dao.CountEventsBySource(ctx, sub.Name)
	durationMs = time.Since(start).Milliseconds()
	return
}

// normalizeType 规范化并验证订阅类型，统一转为小写。
// 支持 github_repo 作为 github 的别名（向后兼容旧数据格式）。
// 空值默认 rss，无效类型返回错误。
func normalizeType(t string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(t))
	switch normalized {
	case "rss", "github":
		return normalized, nil
	case "github_repo":
		return "github", nil // 兼容旧版别名
	case "":
		return "rss", nil // 未指定类型时默认 RSS
	default:
		return "", fmt.Errorf("invalid subscription type: %s (allowed: rss, github)", t)
	}
}
