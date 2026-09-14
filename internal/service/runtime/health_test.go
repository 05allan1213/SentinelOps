package runtime

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestWorkerHealthMissingSnapshotIsUnavailable(t *testing.T) {
	s := NewRuntimeService(nil)
	res, err := s.GetWorkerHealth(context.Background())
	if err != nil || res.Availability != "unavailable" || res.ReasonCode != "not_observed" || len(res.Items) != 0 || res.Aggregate == nil || res.Aggregate.Total != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestWorkerHealthStaleHeartbeat(t *testing.T) {
	now := time.Now().UTC()
	row := mysql.RuntimeWorkerSnapshot{WorkerID: "w1", HeartbeatAt: ptrTime(now.Add(-time.Minute)), Status: ptrString(airuntime.WorkerStatusRunning)}
	item, _ := workerObservationDTO(row, now, 30*time.Second)
	if item.Status != airuntime.WorkerStatusStale {
		t.Fatalf("status=%q", item.Status)
	}
}

func TestWorkerHealthGenerationAndRuntimeVersion(t *testing.T) {
	row := mysql.RuntimeWorkerSnapshot{WorkerID: "w1", HeartbeatAt: ptrTime(time.Now()), RuntimeVersion: ptrString("v1"), ActiveGeneration: ptrU64(7), RuntimeCompatibilityHash: ptrString(strings.Repeat("a", 64))}
	item, _ := workerObservationDTO(row, time.Now(), time.Minute)
	if item.RuntimeVersion != "v1" || item.ActiveGeneration != 7 || item.RuntimeCompatibilityHash == "" {
		t.Fatalf("item=%+v", item)
	}
}

func TestWorkerHealthMissingObservedComponentsAreUnavailable(t *testing.T) {
	now := time.Now().UTC()
	row := mysql.RuntimeWorkerSnapshot{WorkerID: "w1", HeartbeatAt: ptrTime(now), Status: ptrString(airuntime.WorkerStatusIdle)}
	item, meta := workerObservationDTO(row, now, time.Minute)
	if item.ObservedMCPCount != 0 || item.ObservedSkillCount != 0 || meta.Availability != "unavailable" || meta.ReasonCode != "not_observed" {
		t.Fatalf("item=%+v meta=%+v", item, meta)
	}
}

func TestWorkerHealthRedactsPersistedLastError(t *testing.T) {
	now := time.Now().UTC()
	errText := `authorization: Bearer super-secret-token`
	row := mysql.RuntimeWorkerSnapshot{WorkerID: "w1", HeartbeatAt: ptrTime(now), Status: ptrString(airuntime.WorkerStatusIdle), ObservedMCPJSON: ptrString(`[]`), ObservedSkillJSON: ptrString(`[]`), LastErrorRedacted: &errText}
	item, _ := workerObservationDTO(row, now, time.Minute)
	if strings.Contains(item.LastError, "super-secret-token") || item.LastError == "" {
		t.Fatalf("last_error=%q", item.LastError)
	}
}

func TestWorkerHealthServiceProjectsPersistedMySQLRows(t *testing.T) {
	dsn := os.Getenv("SENTINELOPS_TEST_DSN")
	if dsn == "" {
		t.Skip("SENTINELOPS_TEST_DSN is required for disposable MySQL service-path evidence")
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&mysql.RuntimeWorkerSnapshot{}); err != nil {
		t.Fatal(err)
	}
	validID, malformedID := "health-service-valid", ""
	now := time.Now().UTC().Truncate(time.Millisecond)
	running, stale, idle := airuntime.WorkerStatusRunning, airuntime.WorkerStatusIdle, airuntime.WorkerStatusIdle
	version := "worker-v2"
	gen := uint64(9)
	errText := "authorization: Bearer service-secret"
	rows := []mysql.RuntimeWorkerSnapshot{
		{WorkerID: validID, HeartbeatAt: ptrTime(now), RuntimeVersion: &version, Status: &running, ActiveGeneration: &gen, ObservedMCPJSON: ptrString(`[{"name":"mcp"}]`), ObservedSkillJSON: ptrString(`[]`), LastErrorRedacted: &errText},
		{WorkerID: "health-service-stale", HeartbeatAt: ptrTime(now.Add(-time.Minute)), Status: &stale, ObservedMCPJSON: ptrString(`[]`), ObservedSkillJSON: ptrString(`[]`)},
		{WorkerID: malformedID, HeartbeatAt: ptrTime(now), Status: &idle, ObservedMCPJSON: ptrString(`[]`), ObservedSkillJSON: ptrString(`[]`)},
	}
	defer db.Where("worker_id IN ?", []string{validID, "health-service-stale", malformedID}).Delete(&mysql.RuntimeWorkerSnapshot{})
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	service := &RuntimeService{Store: workflow.NewGORMStore(db)}
	res, err := service.GetWorkerHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 || res.Aggregate == nil || res.Aggregate.Total == nil || *res.Aggregate.Total != 2 || res.Availability != "partial" {
		t.Fatalf("res=%+v", res)
	}
	// Exercise the persisted service path, including a page containing only
	// the malformed record and an out-of-range page.
	for page := 1; page <= 4; page++ {
		paged, err := service.GetWorkerHealth(context.Background(), v1.PageRequest{Page: page, PageSize: 1})
		if err != nil || paged.ResourceMeta != res.ResourceMeta || !reflect.DeepEqual(paged.Aggregate, res.Aggregate) || paged.Page.Total != 3 || paged.Page.HasNext != (page < 3) {
			t.Fatalf("paged=%+v err=%v", paged, err)
		}
		if page <= 3 {
			if len(paged.Items) != 1 || !reflect.DeepEqual(paged.Items[0], res.Items[page-1]) {
				t.Fatalf("page=%+v", paged)
			}
		} else if paged.Items == nil || len(paged.Items) != 0 {
			t.Fatalf("out of range=%+v", paged)
		}
	}
	byID := map[string]v1.WorkerObservationDTO{}
	for _, item := range res.Items {
		byID[item.WorkerID] = item
	}
	if byID[validID].Status != airuntime.WorkerStatusRunning || byID[validID].ActiveGeneration != gen || byID[validID].ObservedMCPCount != 1 || strings.Contains(byID[validID].LastError, "service-secret") {
		t.Fatalf("valid=%+v", byID[validID])
	}
	if byID["health-service-stale"].Status != airuntime.WorkerStatusStale {
		t.Fatalf("stale=%+v", byID["health-service-stale"])
	}
	if byID[malformedID].Status != airuntime.WorkerStatusUnavailable {
		t.Fatalf("malformed=%+v", byID[malformedID])
	}
}

func TestRetentionUsesValidatedPolicy(t *testing.T) {
	if err := (airuntime.RetentionPolicy{PayloadDays: 30, AuditDays: 180}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (airuntime.RetentionPolicy{PayloadDays: 0, AuditDays: 180}).Validate(); err == nil {
		t.Fatal("expected invalid policy")
	}
}

func TestRetentionMissingCleanupIsNotObserved(t *testing.T) {
	res, err := NewRuntimeService(nil).GetRetention(context.Background())
	if err != nil || res.Availability != "unavailable" || res.ReasonCode != "not_observed" || res.Item.LastCleanup != nil || res.Item.Availability != "unavailable" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func ptrTime(v time.Time) *time.Time { return &v }
func ptrString(v string) *string     { return &v }
func ptrU64(v uint64) *uint64        { return &v }

func TestWorkerHealthPaginationKeepsGlobalAggregateAndAbnormalRows(t *testing.T) {
	now := time.Now().UTC()
	rows := []mysql.RuntimeWorkerSnapshot{
		{WorkerID: "", HeartbeatAt: &now, ObservedMCPJSON: ptrString(`[]`), ObservedSkillJSON: ptrString(`[]`)},
		{WorkerID: "z-unobserved"},
	}
	for i := 50; i >= 0; i-- {
		status := airuntime.WorkerStatusIdle
		heartbeat := now
		if i%3 == 0 {
			status = airuntime.WorkerStatusRunning
		}
		if i%3 == 1 {
			heartbeat = now.Add(-time.Hour)
		}
		rows = append(rows, mysql.RuntimeWorkerSnapshot{WorkerID: fmt.Sprintf("worker-%02d", i), HeartbeatAt: &heartbeat, Status: &status, ObservedMCPJSON: ptrString(`[]`), ObservedSkillJSON: ptrString(`[]`)})
	}
	all, err := projectWorkerHealth(rows, time.Minute, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if all.Page.Total != 53 || *all.Aggregate.Total != 51 || *all.Aggregate.Active != 17 || *all.Aggregate.Idle != 17 || *all.Aggregate.Stale != 17 || all.Availability != v1.AvailabilityPartial {
		t.Fatalf("all=%+v aggregate=%+v", all, all.Aggregate)
	}
	var combined []v1.WorkerObservationDTO
	for _, request := range []v1.PageRequest{{1, 50}, {2, 50}, {3, 50}, {int(^uint(0) >> 1), 100}} {
		got, err := projectWorkerHealth(rows, time.Minute, now, []v1.PageRequest{request})
		if err != nil {
			t.Fatal(err)
		}
		if got.ResourceMeta != all.ResourceMeta || !reflect.DeepEqual(got.Aggregate, all.Aggregate) || got.Page.Total != 53 || got.Page.Page != request.Page || got.Page.PageSize != request.PageSize || got.Page.HasNext != (request.Page == 1) {
			t.Fatalf("page=%+v", got)
		}
		if request.Page <= 2 {
			combined = append(combined, got.Items...)
		} else if got.Items == nil || len(got.Items) != 0 {
			t.Fatal("out of range must be []")
		}
	}
	if !reflect.DeepEqual(combined, all.Items) {
		t.Fatal("lost rows or changed observation facts")
	}
	for i := 1; i < len(all.Items); i++ {
		if all.Items[i-1].WorkerID > all.Items[i].WorkerID {
			t.Fatal("unstable worker order")
		}
	}
	// Malformed JSON is retained with its existing partial quality semantics.
	row := mysql.RuntimeWorkerSnapshot{WorkerID: "bad-json", HeartbeatAt: &now, ObservedMCPJSON: ptrString(`{broken`), ObservedSkillJSON: ptrString(`[]`)}
	got, err := projectWorkerHealth([]mysql.RuntimeWorkerSnapshot{row}, time.Minute, now, []v1.PageRequest{{1, 1}})
	if err != nil || len(got.Items) != 1 || got.Items[0].ReasonCode != "malformed_worker_snapshot" || got.Items[0].DataQuality != v1.DataQualityPartial {
		t.Fatalf("malformed=%+v err=%v", got, err)
	}
	for _, request := range []v1.PageRequest{{0, 50}, {1, 0}, {1, 101}} {
		if _, err := (&RuntimeService{}).GetWorkerHealth(context.Background(), request); err == nil {
			t.Fatal("accepted invalid pagination")
		}
	}
	empty, err := projectWorkerHealth(nil, time.Minute, now, []v1.PageRequest{{3, 50}})
	if err != nil || empty.Page != (v1.PageMeta{Page: 3, PageSize: 50}) || empty.Items == nil || empty.Aggregate.Total != nil || empty.ReasonCode != "not_observed" {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}
