package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"

	driver "github.com/go-sql-driver/mysql"
)

func retryableDeadlockError() error {
	return fmt.Errorf("%w: %w", workflow.ErrClaimRetryableTransaction,
		&driver.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock; try restarting transaction"})
}

// TestWorkerRunSurvivesRetryableClaimTransactions 是 P0 的核心回归：
// Claim 事务 1213 只能触发有界退避后的重新认领，不能结束 Worker 生命周期。
func TestWorkerRunSurvivesRetryableClaimTransactions(t *testing.T) {
	var claims atomic.Int64
	var observations []WorkerObservation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-claim-retry", LeaseDuration: time.Second,
		MinPollBackoff: 10 * time.Millisecond, MaxPollBackoff: 40 * time.Millisecond,
		Observation: WorkerObservation{WorkerID: "worker-claim-retry"},
		PersistSnapshot: func(_ context.Context, observation WorkerObservation) error {
			observations = append(observations, observation)
			return nil
		},
		HeartbeatSnapshot: func(_ context.Context, observation WorkerObservation) error {
			observations = append(observations, observation)
			return nil
		},
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			if claims.Add(1) <= 3 {
				return nil, false, retryableDeadlockError()
			}
			cancel()
			return nil, false, nil
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
	if runErr := worker.Run(ctx); runErr != context.Canceled {
		t.Fatalf("Run() error=%v, want context.Canceled", runErr)
	}
	if got := claims.Load(); got != 4 {
		t.Fatalf("claim attempts=%d, want three retries plus one final poll", got)
	}
	stats := worker.ClaimRetryStats()
	if stats.Count != 3 || stats.LastReason != "mysql_deadlock_1213" {
		t.Fatalf("claim retry stats=%+v", stats)
	}
	if stats.LastBackoff < 5*time.Millisecond || stats.LastBackoff > 40*time.Millisecond {
		t.Fatalf("claim retry backoff out of bounds: %s", stats.LastBackoff)
	}
	found := false
	for _, observation := range observations {
		if strings.Contains(observation.LastError, "claim retryable database error") &&
			strings.Contains(observation.LastError, "worker=worker-claim-retry") &&
			strings.Contains(observation.LastError, "reason=mysql_deadlock_1213") &&
			strings.Contains(observation.LastError, "db_retry=") &&
			strings.Contains(observation.LastError, "backoff=") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Worker Health never observed the retryable claim conflict: %#v", observations)
	}
}

// TestWorkerRunKeepsFatalClaimErrorsFatal 保证 retry 分类不吞掉真正的 fatal error。
func TestWorkerRunKeepsFatalClaimErrorsFatal(t *testing.T) {
	var claims atomic.Int64
	fatal := errors.New("claim Run: table workflow_runs does not exist")
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-claim-fatal", LeaseDuration: time.Second,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Millisecond,
		Observation: WorkerObservation{WorkerID: "worker-claim-fatal"},
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			claims.Add(1)
			return nil, false, fatal
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
	if runErr := worker.Run(context.Background()); !errors.Is(runErr, fatal) {
		t.Fatalf("Run() error=%v, want fatal claim error", runErr)
	}
	if got := claims.Load(); got != 1 {
		t.Fatalf("fatal claim error was retried %d times", got)
	}
	if stats := worker.ClaimRetryStats(); stats.Count != 0 {
		t.Fatalf("fatal error counted as retryable: %+v", stats)
	}
}

// TestWorkerRunClaimRetryBackoffHonorsContext 保证退避期间 graceful shutdown 不被阻塞。
func TestWorkerRunClaimRetryBackoffHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-claim-shutdown", LeaseDuration: time.Second,
		MinPollBackoff: time.Second, MaxPollBackoff: time.Second,
		Observation: WorkerObservation{WorkerID: "worker-claim-shutdown"},
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			go func() {
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()
			return nil, false, retryableDeadlockError()
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
	started := time.Now()
	if runErr := worker.Run(ctx); runErr != context.Canceled {
		t.Fatalf("Run() error=%v, want context.Canceled", runErr)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("context cancellation waited for the full backoff: %s", elapsed)
	}
}

func TestClaimRetryBackoffIsBoundedAndJittered(t *testing.T) {
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-claim-backoff", LeaseDuration: time.Second,
		MinPollBackoff: 20 * time.Millisecond, MaxPollBackoff: 200 * time.Millisecond,
		ClaimNext:  func(context.Context) (*workflow.ClaimedRun, bool, error) { return nil, false, nil },
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete:   func(context.Context, workflow.CompleteRunInput) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for retry := uint64(1); retry <= 6; retry++ {
		base := 20 * time.Millisecond
		for current := uint64(1); current < retry && base < 200*time.Millisecond; current++ {
			base *= 2
		}
		if base > 200*time.Millisecond {
			base = 200 * time.Millisecond
		}
		for draw := 0; draw < 16; draw++ {
			got := worker.claimRetryBackoff(retry)
			if got < base/2 || got > base {
				t.Fatalf("retry %d backoff %s outside [%s, %s]", retry, got, base/2, base)
			}
		}
	}
	distinct := map[time.Duration]struct{}{}
	for draw := 0; draw < 32; draw++ {
		distinct[worker.claimRetryBackoff(1)] = struct{}{}
	}
	if len(distinct) < 2 {
		t.Fatalf("claim retry backoff has no jitter: %v", distinct)
	}
}
