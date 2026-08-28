package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

type fixture36RetentionBackend struct {
	mu       sync.Mutex
	claimed  bool
	cleanups int
}

func (b *fixture36RetentionBackend) ClaimRetentionLease(_ context.Context, _ workflow.RetentionClaimInput) (*workflow.RetentionLease, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.claimed {
		return nil, false, nil
	}
	b.claimed = true
	return &workflow.RetentionLease{Token: workflow.LeaseToken{RunID: workflow.RetentionLeaseRunID, Owner: "worker-phase36", Generation: 1}}, true, nil
}

func (b *fixture36RetentionBackend) ReleaseRetentionLease(context.Context, workflow.RetentionReleaseInput) error {
	return nil
}

func (b *fixture36RetentionBackend) PhysicalDeleteSensitivePayloads(context.Context, time.Time, int) (workflow.RetentionCleanupCounts, error) {
	b.mu.Lock()
	b.cleanups++
	b.mu.Unlock()
	return workflow.RetentionCleanupCounts{RunPayloads: 1}, nil
}

func (b *fixture36RetentionBackend) PhysicalDeleteAuditMetadata(context.Context, time.Time, int) (workflow.RetentionCleanupCounts, error) {
	return workflow.RetentionCleanupCounts{}, nil
}

type fixture36TraceRetention struct{ payloadCalls, auditCalls int }

func (d *fixture36TraceRetention) PhysicalDeleteTracePayloads(context.Context, time.Time, int) (int64, error) {
	d.payloadCalls++
	return 1, nil
}

func (d *fixture36TraceRetention) PhysicalDeleteTraceMetadata(context.Context, time.Time, int) (int64, error) {
	d.auditCalls++
	return 1, nil
}

func TestCleanupLeaseAllowsOnlyOneWorker(t *testing.T) {
	backend := &fixture36RetentionBackend{}
	traceDAO := &fixture36TraceRetention{}
	coordinator, err := NewRetentionCoordinator(backend, traceDAO, RetentionConfig{
		Owner: "worker-phase36", LeaseDuration: time.Minute, Interval: time.Hour, BatchSize: 100,
		Policy: DefaultRetentionPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, runErr := coordinator.RunOnce(context.Background()); runErr != nil {
				t.Error(runErr)
			}
		}()
	}
	wg.Wait()
	if backend.cleanups != 1 || traceDAO.payloadCalls != 1 || traceDAO.auditCalls != 1 {
		t.Fatalf("cleanup calls workflow=%d trace_payload=%d trace_audit=%d, want one lease", backend.cleanups, traceDAO.payloadCalls, traceDAO.auditCalls)
	}
}

func TestRetentionProtectsNonTerminalCheckpoint(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	for _, status := range []string{
		workflow.RunStatusPending, workflow.RunStatusRunning, workflow.RunStatusWaitingApproval,
		workflow.RunStatusRetryableFailed, workflow.RunStatusParked, workflow.RunStatusReconciling,
	} {
		run := mysql.WorkflowRun{Status: status, FinishedAt: &old}
		if _, ok := terminalRetentionAnchor(run); ok {
			t.Fatalf("status %q unexpectedly eligible for checkpoint cleanup", status)
		}
	}
}

func TestPhysicalDeleteUsesTerminalAnchor(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	finished := now.Add(-31 * 24 * time.Hour)
	run := mysql.WorkflowRun{Status: workflow.RunStatusSucceeded, FinishedAt: &finished}
	anchor, ok := terminalRetentionAnchor(run)
	if !ok || !anchor.Equal(finished) {
		t.Fatalf("terminal anchor=%v ok=%t, want finished_at", anchor, ok)
	}
	policy := DefaultRetentionPolicy()
	payloadCutoff, auditCutoff := policy.Cutoffs(now)
	if !finished.Before(payloadCutoff) || finished.Before(auditCutoff) {
		t.Fatalf("cutoffs payload=%v audit=%v finished=%v", payloadCutoff, auditCutoff, finished)
	}
}

func TestRetentionOnlyWorkerDoesNotClaimRuns(t *testing.T) {
	backend := &fixture36RetentionBackend{}
	traceDAO := &fixture36TraceRetention{}
	coordinator, err := NewRetentionCoordinator(backend, traceDAO, RetentionConfig{
		Owner: "worker-phase36", LeaseDuration: time.Minute, Interval: time.Hour, BatchSize: 100,
		Policy: DefaultRetentionPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	claimCalls := 0
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase36", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		Retention:  coordinator,
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete:   func(context.Context, workflow.CompleteRunInput) error { return nil },
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			claimCalls++
			return nil, false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked || claimCalls != 0 {
		t.Fatalf("retention-only RunOnce worked=%t claim_calls=%d err=%v", worked, claimCalls, err)
	}
}
