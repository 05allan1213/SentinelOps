package eventsvc

import (
	"context"
	"testing"
	"time"

	dao "SentinelOps/internal/dao/mysql"
)

// 事件搜索必须覆盖原始告警正文；事件“今日/近 7 天”统计必须与 UTC 存储一致
// （连接由 normalizeApplicationDSN 固定 loc=UTC，本地时区会在 00:00-08:00 之间漏计）。
func TestEventSearchMatchesRawPayloadAndStatsUseUTCDay(t *testing.T) {
	db := fixture26HumanCRUDDatabase(t)
	ctx := context.Background()
	// 让“本地今天”必然与“UTC 今天”不是同一天：只有真正按 UTC 统计才能通过，
	// 否则测试只在本地 00:00-08:00 之间才会暴露时区缺陷。
	originalLocal := time.Local
	time.Local = fixedOffsetOppositeUTCDate(time.Now().UTC())
	defer func() { time.Local = originalLocal }()
	// 生产 DSN 由 normalizeApplicationDSN 固定 MySQL 会话为 UTC；被测替身库也必须是
	// UTC 会话，否则 DATETIME 与 UTC 日断言的组合没有意义。
	var sessionOffsetSeconds int
	if err := db.Raw("SELECT TIMESTAMPDIFF(SECOND, UTC_TIMESTAMP(), NOW())").Scan(&sessionOffsetSeconds).Error; err != nil {
		t.Fatalf("read disposable MySQL session offset: %v", err)
	}
	if sessionOffsetSeconds > 60 || sessionOffsetSeconds < -60 {
		t.Skipf("disposable MySQL session is not UTC (offset=%ds)", sessionOffsetSeconds)
	}

	now := time.Now().UTC()
	events := []dao.Event{
		{
			ID: "search-raw-payload-hit", Title: "phase-search hit", Severity: "high",
			EventType: "api_push", Source: "phase-search", Status: "new",
			RawPayload: `{"content":"检测到来自 10.0.0.9 的 SSH 暴力破解尝试"}`, Metadata: `{}`,
			CreatedAt: now,
		},
		{
			ID: "search-raw-payload-miss", Title: "phase-search miss", Severity: "low",
			EventType: "api_push", Source: "phase-search", Status: "new",
			RawPayload: `{"content":"正常登录成功"}`, Metadata: `{}`,
			CreatedAt: now,
		},
	}
	for index := range events {
		if err := db.Create(&events[index]).Error; err != nil {
			t.Fatalf("create event %s: %v", events[index].ID, err)
		}
	}

	list, total, err := dao.ListEvents(ctx, 20, 0, "", "", "暴力破解", "", "")
	if err != nil {
		t.Fatalf("search events: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != "search-raw-payload-hit" {
		t.Fatalf("raw payload search total=%d list=%v, want only the hit event", total, list)
	}

	stats, err := dao.GetEventStats(ctx)
	if err != nil {
		t.Fatalf("event stats: %v", err)
	}
	if stats.TodayCount != 2 {
		t.Fatalf("today_count = %d, want 2 for events created in the current UTC day", stats.TodayCount)
	}
	if stats.New7Days < 2 {
		t.Fatalf("new_7days = %d, want >= 2", stats.New7Days)
	}
}

// fixedOffsetOppositeUTCDate 返回一个固定时区，使当前 UTC 时刻在该时区落在不同日期。
func fixedOffsetOppositeUTCDate(nowUTC time.Time) *time.Location {
	if nowUTC.Hour() >= 10 {
		return time.FixedZone("test-utc-plus-14", 14*60*60)
	}
	return time.FixedZone("test-utc-minus-12", -12*60*60)
}
