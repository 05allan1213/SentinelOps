package runtime

import (
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
)

func TestLeaseBackoffIsBounded(t *testing.T) {
	worker, err := NewWorker(workflow.NewGORMStore(nil), WorkerConfig{
		Owner:          "worker-p09",
		LeaseDuration:  time.Minute,
		MinPollBackoff: 25 * time.Millisecond,
		MaxPollBackoff: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new Worker: %v", err)
	}
	want := []time.Duration{25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond}
	for failures, expected := range want {
		if got := worker.NextPollBackoff(failures); got != expected {
			t.Errorf("backoff(%d) = %s, want %s", failures, got, expected)
		}
	}
}
