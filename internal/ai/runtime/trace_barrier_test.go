package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

type fixture35Barrier struct {
	quality string
	flushed bool
}

func (b *fixture35Barrier) Finish(error) {}
func (b *fixture35Barrier) Flush(context.Context) string {
	b.flushed = true
	return b.quality
}

func TestTraceFlushPrecedesRunCompletion(t *testing.T) {
	barrier := &fixture35Barrier{quality: workflow.TraceQualityComplete}
	completed := false
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase35", LeaseDuration: time.Second, MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Second,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			owner := "worker-phase35"
			return &workflow.ClaimedRun{Run: mysql.WorkflowRun{ID: "run-phase35", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 3, LeaseOwner: &owner, LeaseGeneration: 4}, Token: workflow.LeaseToken{RunID: "run-phase35", Owner: owner, Generation: 4}}, true, nil
		},
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{RevisionStateJSON: []byte(`{"schema":"sentinelops/session-state/v1"}`), TraceID: "trace-phase35", TraceBarrier: barrier}, nil
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete: func(_ context.Context, input workflow.CompleteRunInput) error {
			if !barrier.flushed {
				t.Fatal("Run completion happened before Trace Flush")
			}
			if input.TraceQuality != workflow.TraceQualityComplete {
				t.Fatalf("trace quality = %q", input.TraceQuality)
			}
			completed = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := worker.RunOnce(context.Background()); err != nil || !ok || !completed {
		t.Fatalf("RunOnce ok=%v completed=%v err=%v", ok, completed, err)
	}
}

func TestTraceIncompleteQualityReachesTerminalPrimitive(t *testing.T) {
	barrier := &fixture35Barrier{quality: workflow.TraceQualityIncomplete}
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase35-timeout", LeaseDuration: time.Second, MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Second,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			owner := "worker-phase35-timeout"
			return &workflow.ClaimedRun{Run: mysql.WorkflowRun{ID: "run-phase35-timeout", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 1, LeaseOwner: &owner, LeaseGeneration: 2}, Token: workflow.LeaseToken{RunID: "run-phase35-timeout", Owner: owner, Generation: 2}}, true, nil
		},
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{TraceID: "trace-phase35-timeout", TraceBarrier: barrier}, errors.New("model failed")
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete: func(_ context.Context, input workflow.CompleteRunInput) error {
			if input.TraceQuality != workflow.TraceQualityIncomplete {
				t.Fatalf("trace quality = %q", input.TraceQuality)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStaleTraceFlushesButCannotChangeRun(t *testing.T) {
	barrier := &fixture35Barrier{quality: workflow.TraceQualityComplete}
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase35-stale", LeaseDuration: time.Second, MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Second,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			owner := "worker-phase35-stale"
			return &workflow.ClaimedRun{Run: mysql.WorkflowRun{ID: "run-phase35-stale", Status: workflow.RunStatusRunning, Attempt: 2, MaxAttempts: 2, LeaseOwner: &owner, LeaseGeneration: 9}, Token: workflow.LeaseToken{RunID: "run-phase35-stale", Owner: owner, Generation: 9}}, true, nil
		},
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{RevisionStateJSON: []byte(`{"schema":"sentinelops/session-state/v1"}`), TraceID: "trace-phase35-stale", TraceBarrier: barrier}, nil
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete: func(context.Context, workflow.CompleteRunInput) error {
			if !barrier.flushed {
				t.Fatal("stale Attempt was rejected before its diagnostic Trace flushed")
			}
			return workflow.ErrLeaseLost
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := worker.RunOnce(context.Background()); !ok || !errors.Is(err, workflow.ErrLeaseLost) {
		t.Fatalf("RunOnce ok=%v err=%v", ok, err)
	}
}

func TestStaleTraceFlushesAfterHeartbeatLeaseLoss(t *testing.T) {
	barrier := &fixture35Barrier{quality: workflow.TraceQualityComplete}
	heartbeatCalled := make(chan struct{})
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase35-heartbeat", LeaseDuration: 3 * time.Millisecond, MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Second,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			owner := "worker-phase35-heartbeat"
			return &workflow.ClaimedRun{Run: mysql.WorkflowRun{ID: "run-phase35-heartbeat", Status: workflow.RunStatusRunning, Attempt: 2, MaxAttempts: 2, LeaseOwner: &owner, LeaseGeneration: 10}, Token: workflow.LeaseToken{RunID: "run-phase35-heartbeat", Owner: owner, Generation: 10}}, true, nil
		},
		Heartbeat: func(context.Context, workflow.LeaseToken) error {
			close(heartbeatCalled)
			return workflow.ErrLeaseLost
		},
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			<-heartbeatCalled
			return RunExecutionResult{TraceID: "trace-phase35-heartbeat", TraceBarrier: barrier}, nil
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete: func(context.Context, workflow.CompleteRunInput) error {
			t.Fatal("stale heartbeat generation reached Run completion")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := worker.RunOnce(context.Background()); !ok || !errors.Is(err, workflow.ErrLeaseLost) {
		t.Fatalf("RunOnce ok=%v err=%v", ok, err)
	}
	if !barrier.flushed {
		t.Fatal("heartbeat lease loss returned before diagnostic Trace flush")
	}
}
