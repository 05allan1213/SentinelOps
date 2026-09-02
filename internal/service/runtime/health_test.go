package runtime

import (
	"context"
	"os"
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
