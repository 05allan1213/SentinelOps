package mysql

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeWorkerSnapshotQueryRequiresStore(t *testing.T) {
	if _, err := (*GORMStore)(nil).ListRuntimeWorkerSnapshots(context.Background()); err == nil {
		t.Fatal("expected missing store error")
	}
}

func TestRuntimeRunStatusQueryRequiresStore(t *testing.T) {
	if _, err := (*GORMStore)(nil).ListRuntimeRunStatuses(context.Background()); err == nil {
		t.Fatal("expected missing store error")
	}
}

func TestRuntimeWorkerSnapshotQueryReturnsPersistedRows(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_health_projection")
	now := time.Now().UTC().Truncate(time.Millisecond)
	version := "worker-v1"
	status := "running"
	if err := db.Create(&RuntimeWorkerSnapshot{WorkerID: "health-worker", HeartbeatAt: &now, RuntimeVersion: &version, Status: &status, ObservedMCPJSON: ptrString(`[{}]`), ObservedSkillJSON: ptrString(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListRuntimeWorkerSnapshots(context.Background())
	if err != nil || len(rows) != 1 || rows[0].WorkerID != "health-worker" || rows[0].RuntimeVersion == nil || *rows[0].RuntimeVersion != version {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestWorkerSnapshotOrderingKeepsUnavailableRecords(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_health_ordering")
	for _, id := range []string{"z-worker", "", "a-worker"} {
		if err := db.Create(&RuntimeWorkerSnapshot{WorkerID: id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.ListRuntimeWorkerSnapshots(context.Background())
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	for i, want := range []string{"", "a-worker", "z-worker"} {
		if rows[i].WorkerID != want {
			t.Fatalf("row %d=%s", i, rows[i].WorkerID)
		}
	}
}
