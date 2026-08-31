package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestWorkerSnapshotConfiguredAndObservedAreSeparate(t *testing.T) {
	record, err := workerSnapshotRecord(WorkerObservation{
		WorkerID: "worker-c05", RuntimeVersion: "runtime-v1",
		RuntimeCompatibilityHash: strings.Repeat("a", 64), ConfiguredCatalogHash: strings.Repeat("b", 64),
		ObservedMCP:   []ObservedRuntimeComponent{{Name: "inventory", Status: "not_observed", Validation: "not_run"}},
		ObservedSkill: []ObservedRuntimeComponent{{Name: "triage", Hash: strings.Repeat("c", 64), Status: "loaded", Validation: "valid"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ConfiguredCatalogHash == nil || *record.ConfiguredCatalogHash != strings.Repeat("b", 64) {
		t.Fatalf("configured catalog identity was not stored separately: %#v", record.ConfiguredCatalogHash)
	}
	if record.ObservedMCPJSON == nil || strings.Contains(*record.ObservedMCPJSON, strings.Repeat("b", 64)) {
		t.Fatalf("configured state leaked into observed state: %v", record.ObservedMCPJSON)
	}
}

func TestWorkerSnapshotNeverContainsSecret(t *testing.T) {
	secret := "top-secret-token"
	record, err := workerSnapshotRecord(WorkerObservation{
		WorkerID: "worker-secret", Status: WorkerStatusIdle,
		ObservedMCP:   []ObservedRuntimeComponent{{Name: "inventory", Status: "error", Error: "Authorization: Bearer " + secret}},
		ObservedSkill: []ObservedRuntimeComponent{{Name: "triage", Status: "error", Error: "password=" + secret}},
		LastError:     "api_key=" + secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := *record.ObservedMCPJSON + *record.ObservedSkillJSON
	if record.LastErrorRedacted != nil {
		joined += *record.LastErrorRedacted
	}
	if strings.Contains(joined, secret) {
		t.Fatalf("snapshot leaked secret fixture: %s", joined)
	}
	for _, forbidden := range []string{"url", "header", "secret_ref", "handle"} {
		if strings.Contains(strings.ToLower(joined), forbidden) {
			t.Fatalf("snapshot contains forbidden field %q: %s", forbidden, joined)
		}
	}
}

func TestWorkerSnapshotHeartbeatAndStale(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-29 * time.Second)
	stale := now.Add(-31 * time.Second)
	if got := ClassifyObservedWorker(now, nil, 30*time.Second); got != WorkerStatusUnavailable {
		t.Fatalf("missing snapshot = %q, want unavailable", got)
	}
	if got := ClassifyObservedWorker(now, &fresh, 30*time.Second); got != WorkerStatusIdle {
		t.Fatalf("fresh snapshot = %q, want idle", got)
	}
	if got := ClassifyObservedWorker(now, &stale, 30*time.Second); got != WorkerStatusStale {
		t.Fatalf("expired snapshot = %q, want stale", got)
	}
	if _, err := workerSnapshotRecord(WorkerObservation{WorkerID: "worker-stale", Status: WorkerStatusStale}); err == nil {
		t.Fatal("Worker persisted server-only stale classification")
	}
}

func TestWorkerSnapshotTracksActiveGeneration(t *testing.T) {
	observations := make([]WorkerObservation, 0, 3)
	record := func(_ context.Context, observation WorkerObservation) error {
		observations = append(observations, observation)
		return nil
	}
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-active", LeaseDuration: time.Second, MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Second,
		Observation: WorkerObservation{WorkerID: "worker-active", RuntimeVersion: "v1"}, PersistSnapshot: record, HeartbeatSnapshot: record,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			return &workflow.ClaimedRun{Run: mysql.WorkflowRun{ID: "run-active", Status: workflow.RunStatusRunning}, Token: workflow.LeaseToken{RunID: "run-active", Owner: "worker-active", Generation: 7}}, true, nil
		},
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, nil
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete:   func(context.Context, workflow.CompleteRunInput) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if worked, runErr := worker.RunOnce(context.Background()); runErr != nil || !worked {
		t.Fatalf("RunOnce worked=%t err=%v", worked, runErr)
	}
	if len(observations) < 3 || observations[1].Status != WorkerStatusRunning || observations[1].ActiveRunID != "run-active" || observations[1].ActiveGeneration != 7 {
		t.Fatalf("active generation was not observed: %#v", observations)
	}
	last := observations[len(observations)-1]
	if last.Status != WorkerStatusIdle || last.ActiveRunID != "" || last.ActiveGeneration != 0 {
		t.Fatalf("active generation cleared before/after terminal commit incorrectly: %#v", last)
	}
}

func TestWorkerUsesExistingPollLoop(t *testing.T) {
	var polls int
	ctx, cancel := context.WithCancel(context.Background())
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-loop", LeaseDuration: time.Second, MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Millisecond,
		Observation: WorkerObservation{WorkerID: "worker-loop"}, PersistSnapshot: func(context.Context, WorkerObservation) error { return nil }, HeartbeatSnapshot: func(context.Context, WorkerObservation) error { return nil },
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			polls++
			if polls == 2 {
				cancel()
			}
			return nil, false, nil
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil }, Complete: func(context.Context, workflow.CompleteRunInput) error { return nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runErr := worker.Run(ctx); runErr != context.Canceled {
		t.Fatalf("Run() error=%v", runErr)
	}
	if polls != 2 {
		t.Fatalf("poll loop count=%d, want one existing loop with two iterations", polls)
	}
}

func TestWorkerSnapshotPersistsHeartbeatWithDisposableDSN(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "worker_snapshot_c05")
	first := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	observation := WorkerObservation{
		WorkerID: "worker-persisted", HeartbeatAt: first, RuntimeVersion: "runtime-v1",
		RuntimeCompatibilityHash: strings.Repeat("a", 64), ConfiguredCatalogHash: strings.Repeat("b", 64),
		ObservedMCP: []ObservedRuntimeComponent{{Name: "inventory", Status: "not_observed", Validation: "not_run"}},
		Status:      WorkerStatusIdle,
	}
	if err := PersistWorkerSnapshot(context.Background(), db, observation); err != nil {
		t.Fatal(err)
	}
	observation.HeartbeatAt = first.Add(10 * time.Second)
	observation.Status = WorkerStatusRunning
	observation.ActiveRunID = "run-c05"
	observation.ActiveGeneration = 9
	if err := HeartbeatWorkerSnapshot(context.Background(), db, observation); err != nil {
		t.Fatal(err)
	}
	var stored mysql.RuntimeWorkerSnapshot
	if err := db.Where("worker_id = ?", observation.WorkerID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status == nil || *stored.Status != WorkerStatusRunning || stored.ActiveRunID == nil || *stored.ActiveRunID != "run-c05" || stored.ActiveGeneration == nil || *stored.ActiveGeneration != 9 {
		t.Fatalf("persisted active observation = %#v", stored)
	}
	if stored.HeartbeatAt == nil || !stored.HeartbeatAt.Equal(observation.HeartbeatAt) {
		t.Fatalf("heartbeat_at=%v, want %v", stored.HeartbeatAt, observation.HeartbeatAt)
	}
}
