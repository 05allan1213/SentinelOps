package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"gorm.io/gorm"
)

// ListEvents 查询事件列表，支持 severity/status/keyword 过滤、排序和分页。
// orderBy 可选值：severity, status, source, created_at（默认 created_at DESC）。
func ListEvents(ctx context.Context, limit, offset int, severity, status, keyword, orderBy, orderDir string) ([]Event, int64, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, 0, err
	}
	q := db.Model(&Event{})
	if severity != "" {
		q = q.Where("severity = ?", severity)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if keyword != "" {
		q = q.Where("title LIKE ? OR cve_id LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// 构建 ORDER BY 子句，默认按创建时间倒序
	order := "created_at DESC"
	if orderBy != "" {
		dir := "DESC"
		if strings.EqualFold(orderDir, "asc") {
			dir = "ASC"
		}
		switch orderBy {
		case "severity":
			// 严重程度自定义排序：critical > high > medium > low > info
			order = "CASE severity WHEN 'critical' THEN 1 WHEN 'high' THEN 2 WHEN 'medium' THEN 3 WHEN 'low' THEN 4 ELSE 5 END " + dir
		case "status":
			order = "status " + dir
		case "source":
			order = "source " + dir
		case "created_at":
			order = "created_at " + dir
		}
	}
	var events []Event
	if err = q.Limit(limit).Offset(offset).Order(order).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

// UpdateEventStatus 更新单条事件状态。
func UpdateEventStatus(ctx context.Context, id, status string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&Event{}).Where("id = ?", id).Update("status", status).Error
}

// ClaimEventForOps 原子抢占事件的 AI 运维处理权。
// 只有 new 状态能被抢占为 processing，避免多个触发入口同时启动同一事件的运维任务。
func ClaimEventForOps(ctx context.Context, id string) (bool, error) {
	db, err := DB(ctx)
	if err != nil {
		return false, err
	}
	result := db.Model(&Event{}).Where("id = ? AND status = ?", id, "new").Update("status", "processing")
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// UpdateEventDescription 将 AI 分析结论写入事件 Metadata 的 ai_analysis 字段。
func UpdateEventDescription(ctx context.Context, id, description string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	// 读取现有 Metadata，合并写入 ai_analysis
	var e Event
	if err := db.Select("metadata").First(&e, "id = ?", id).Error; err != nil {
		return err
	}
	meta := map[string]interface{}{}
	if e.Metadata != "" {
		_ = json.Unmarshal([]byte(e.Metadata), &meta)
	}
	meta["ai_analysis"] = description
	b, _ := json.Marshal(meta)
	return db.Model(&Event{}).Where("id = ?", id).Update("metadata", string(b)).Error
}

// BatchUpdateEventStatus 批量更新事件状态。
func BatchUpdateEventStatus(ctx context.Context, ids []string, status string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&Event{}).Where("id IN ?", ids).Update("status", status).Error
}

// CreateEvent 将事件持久化到数据库。
func CreateEvent(ctx context.Context, e *Event) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Create(e).Error
}

// BatchDeleteEvents 批量软删除安全事件。
func BatchDeleteEvents(ctx context.Context, ids []string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Where("id IN ?", ids).Delete(&Event{}).Error
}

// DeleteEvent 软删除安全事件（设置 deleted_at 字段）。
func DeleteEvent(ctx context.Context, id string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Where("id = ?", id).Delete(&Event{}).Error
}

// GetEventByDedupKey 按去重键查询事件，未找到时返回 (nil, nil)。
// 用于告警接入时的快速去重判断，避免重复写入相同告警。
func GetEventByDedupKey(ctx context.Context, dedupKey string) (*Event, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var e Event
	if err := db.Where("dedup_key = ?", dedupKey).Limit(1).Find(&e).Error; err != nil {
		return nil, err
	}
	if e.ID == "" {
		return nil, nil
	}
	return &e, nil
}

// FindIntelligenceByCVEID 查询 event_type=web 中是否已存在相同 CVE ID 的记录。
// 返回 (nil, nil) 表示未找到；返回非 nil Event 表示找到已有记录。
func FindIntelligenceByCVEID(ctx context.Context, cveID string) (*Event, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var e Event
	result := db.Where("cve_id = ?", cveID).First(&e)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &e, nil
}

// UpdateIntelligenceFields 更新情报记录的可变字段
// 仅更新 content 分析相关字段，不修改 id、source、created_at 等元数据。
func UpdateIntelligenceFields(ctx context.Context, id, severity string, riskScore float64, metadata string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	result := db.Model(&Event{}).Where("id = ?", id).Updates(map[string]any{
		"severity":   severity,
		"risk_score": riskScore,
		"metadata":   metadata,
		"event_type": "web",
		"status":     "new",             // 更新后重置为待处置，提示分析人员复核
		"indexed_at": gorm.Expr("NULL"), // 清空索引时间，触发 Milvus 重新向量化
		"updated_at": time.Now(),        // 显式更新时间戳，确保即使其他字段未变也触发行变更
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		g.Log().Warningf(ctx, "[DAO] UpdateIntelligenceFields: id=%s 未命中任何行，可能已被删除", id)
	}
	return nil
}

// ListIndexedEventIDsBySource 查询来自指定订阅源的所有已索引事件 ID。
// 仅返回 indexed_at IS NOT NULL 的事件，用于删除订阅时同步清理 Milvus 向量。
func ListIndexedEventIDsBySource(ctx context.Context, source string) ([]string, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	err = db.Model(&Event{}).
		Where("source = ? AND indexed_at IS NOT NULL", source).
		Pluck("id", &ids).Error
	return ids, err
}

// DeleteEventsBySource 软删除来自指定订阅源的所有事件，遇到死锁自动重试（最多 3 次）。
// 并发删除多个订阅时，MySQL 在 source 索引的 gap lock 可能导致 Error 1213 死锁，
// 重试是 MySQL 官方推荐的标准处理方式。
func DeleteEventsBySource(ctx context.Context, source string) error {
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
			g.Log().Warningf(ctx, "[DAO] DeleteEventsBySource 死锁重试 %d/%d: source=%s", attempt, maxRetries-1, source)
		}
		err = db.Where("source = ?", source).Delete(&Event{}).Error
		if err == nil || !strings.Contains(err.Error(), "1213") {
			return err
		}
	}
	return err
}

// DedupAndInsertEvents 对事件列表去重后批量插入，返回实际插入的事件列表。
// web 类型优先级最高：已有 web 记录时跳过；新事件为 web 时替换旧记录；普通来源重复则跳过。
// 单条失败不阻断其余条目。
func DedupAndInsertEvents(ctx context.Context, events []Event) ([]Event, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var inserted []Event
	for _, e := range events {
		// 用 Limit(1).Find 代替 First：Find 找不到记录时不返回 ErrRecordNotFound，
		// 避免 GORM 内部 logger 对每条新事件都打印 "record not found" 噪音日志。
		var existing Event
		if findErr := db.Where("dedup_key = ?", e.DedupKey).Limit(1).Find(&existing).Error; findErr != nil {
			g.Log().Warningf(ctx, "[DAO] 查询 dedup_key 失败（%s）: %v", e.DedupKey, findErr)
			continue
		}
		if existing.ID != "" {
			// 已存在重复记录：web 来源权威，不可覆盖；非 web 新事件跳过；web 新事件替换旧记录
			if existing.EventType == "web" {
				continue
			}
			if e.EventType != "web" {
				continue
			}
			if delErr := db.Delete(&existing).Error; delErr != nil {
				g.Log().Warningf(ctx, "[DAO] 删除旧记录失败（id=%s）: %v", existing.ID, delErr)
				continue
			}
		}
		if createErr := db.Create(&e).Error; createErr != nil {
			g.Log().Warningf(ctx, "[DAO] 插入事件失败（dedup_key=%s）: %v", e.DedupKey, createErr)
			continue
		}
		inserted = append(inserted, e)
	}
	return inserted, nil
}

// MarkEventsIndexed 批量将事件标记为已完成向量索引（更新 indexed_at 字段）。
// 仅传入实际成功写入 Milvus 的事件 ID，避免误标索引失败的记录。
func MarkEventsIndexed(ctx context.Context, ids []string, t time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	db, err := DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&Event{}).Where("id IN ?", ids).Update("indexed_at", t).Error
}

// EventTrendItem 单日事件统计。
type EventTrendItem struct {
	Date     string
	Total    int64
	Critical int64
	High     int64
	Medium   int64
	Low      int64
}

// GetEventTrend 查询最近 days 天按日期分组的事件计数，返回日期倒序结果。
func GetEventTrend(ctx context.Context, days int) ([]EventTrendItem, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 30
	}

	type row struct {
		Date     string
		Severity string
		Count    int64
	}
	var rows []row
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	if err = db.Model(&Event{}).
		Select("DATE_FORMAT(created_at, '%Y-%m-%d') AS date, severity, COUNT(*) AS count").
		Where("DATE(created_at) >= ?", since).
		Group("DATE_FORMAT(created_at, '%Y-%m-%d'), severity").
		Order("DATE_FORMAT(created_at, '%Y-%m-%d') DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	// 按日期聚合各严重程度计数
	dayMap := make(map[string]*EventTrendItem)
	for _, r := range rows {
		item, ok := dayMap[r.Date]
		if !ok {
			item = &EventTrendItem{Date: r.Date}
			dayMap[r.Date] = item
		}
		item.Total += r.Count
		switch r.Severity {
		case "critical":
			item.Critical += r.Count
		case "high":
			item.High += r.Count
		case "medium":
			item.Medium += r.Count
		case "low":
			item.Low += r.Count
		}
	}

	items := make([]EventTrendItem, 0, len(dayMap))
	for _, v := range dayMap {
		items = append(items, *v)
	}
	// 日期倒序（bubble sort，数据量小无需引入 sort.Slice）
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Date < items[j].Date {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items, nil
}

type EventStats struct {
	Total         int64            `json:"total"`
	TodayCount    int64            `json:"today_count"`
	CriticalCount int64            `json:"critical_count"`
	BySeverity    map[string]int64 `json:"by_severity"`
	New7Days      int64            `json:"new_7days"` // 近7天新增
	Pending       int64            `json:"pending"`   // 待处置（status='new'）
}

// GetEventStats 查询事件统计：总数、今日新增、高危数量、按严重程度分组、近7天新增、待处置数。
func GetEventStats(ctx context.Context) (*EventStats, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	stats := &EventStats{BySeverity: make(map[string]int64)}

	if err = db.Model(&Event{}).Count(&stats.Total).Error; err != nil {
		return nil, err
	}

	// DATE(created_at) 与 loc=Local 驱动存储格式一致，避免跨时区边界误差
	todayStr := time.Now().Format("2006-01-02")
	if err = db.Model(&Event{}).Where("DATE(created_at) = ?", todayStr).Count(&stats.TodayCount).Error; err != nil {
		return nil, err
	}

	if err = db.Model(&Event{}).Where("severity IN ?", []string{"critical", "high"}).Count(&stats.CriticalCount).Error; err != nil {
		return nil, err
	}

	type row struct {
		Severity string
		Count    int64
	}
	var rows []row
	if err = db.Model(&Event{}).Select("severity, COUNT(*) as count").Group("severity").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		stats.BySeverity[r.Severity] = r.Count
	}

	sevenDaysAgo := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	if err = db.Model(&Event{}).Where("DATE(created_at) >= ?", sevenDaysAgo).Count(&stats.New7Days).Error; err != nil {
		return nil, err
	}

	if err = db.Model(&Event{}).Where("status = ?", "new").Count(&stats.Pending).Error; err != nil {
		return nil, err
	}

	return stats, nil
}

// GetEventByID 按主键查询单个事件
func GetEventByID(ctx context.Context, id string) (*Event, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var e Event
	if err := db.First(&e, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &e, nil
}

// ListUnhandledHighSeverityEvents 查询高危/严重且从未触发过 AI 运维的事件（补偿扫描用）
func ListUnhandledHighSeverityEvents(ctx context.Context, limit int) ([]Event, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}
	var events []Event
	err = db.Where(
		"severity IN ? AND status = ? AND id NOT IN (SELECT event_id FROM ops_runs)",
		[]string{"high", "critical"}, "new",
	).Order("created_at asc").Limit(limit).Find(&events).Error
	return events, err
}
