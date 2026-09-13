package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
)

// 运行期修改动态 Gate 后，常驻 Worker 必须重建 observation，否则 claim 使用的
// runtime compatibility hash 会一直停留在启动时的旧向量上，新 Run 永远无法认领。
func TestWorkerRefreshesObservationWhenGatesChange(t *testing.T) {
	refreshCalls := 0
	worker, err := NewWorker(workflow.NewGORMStore(nil), WorkerConfig{
		Owner:          "worker-gate-refresh",
		LeaseDuration:  time.Minute,
		MinPollBackoff: 25 * time.Millisecond,
		MaxPollBackoff: 200 * time.Millisecond,
		Observation: WorkerObservation{
			WorkerID: "worker-gate-refresh", Status: WorkerStatusIdle,
			RuntimeCompatibilityHash: "hash-at-startup",
		},
		ObservationRefresh: func(context.Context) (WorkerObservation, error) {
			refreshCalls++
			return WorkerObservation{
				WorkerID: "worker-gate-refresh", Status: WorkerStatusIdle,
				RuntimeCompatibilityHash: "hash-after-gate-change",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("new Worker: %v", err)
	}

	closed := mustGateVector(t, map[string]bool{})
	open := mustGateVector(t, map[string]bool{GateAgentRuntimeEnabled: true, GateAgentRuntimeAcceptNewRuns: true})
	ctx := context.Background()

	if err := worker.syncObservationWithGates(ctx, closed); err != nil {
		t.Fatalf("sync observation: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("initial observation refresh calls = %d, want 1", refreshCalls)
	}
	if got := worker.observation.RuntimeCompatibilityHash; got != "hash-after-gate-change" {
		t.Fatalf("compatibility hash = %q, want refreshed hash", got)
	}

	// 同一 Gate 向量继续轮询时不得重复重建观测。
	if err := worker.syncObservationWithGates(ctx, closed); err != nil {
		t.Fatalf("sync unchanged gates: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("unchanged Gate vector triggered refresh, calls = %d", refreshCalls)
	}

	if err := worker.syncObservationWithGates(ctx, open); err != nil {
		t.Fatalf("sync changed gates: %v", err)
	}
	if refreshCalls != 2 {
		t.Fatalf("changed Gate vector refresh calls = %d, want 2", refreshCalls)
	}
}

// 未注入 ObservationRefresh 的既有调用方保持原行为。
func TestWorkerGateSyncWithoutRefreshLoaderIsNoop(t *testing.T) {
	worker, err := NewWorker(workflow.NewGORMStore(nil), WorkerConfig{
		Owner:          "worker-gate-noop",
		LeaseDuration:  time.Minute,
		MinPollBackoff: 25 * time.Millisecond,
		MaxPollBackoff: 200 * time.Millisecond,
		Observation:    WorkerObservation{WorkerID: "worker-gate-noop", RuntimeCompatibilityHash: "hash-fixed"},
	})
	if err != nil {
		t.Fatalf("new Worker: %v", err)
	}
	if err := worker.syncObservationWithGates(context.Background(), mustGateVector(t, map[string]bool{})); err != nil {
		t.Fatalf("sync without loader: %v", err)
	}
	if got := worker.observation.RuntimeCompatibilityHash; got != "hash-fixed" {
		t.Fatalf("compatibility hash = %q, want unchanged", got)
	}
}

func mustGateVector(t *testing.T, overrides map[string]bool) GateVector {
	t.Helper()
	values := make(map[string]bool, len(CanonicalGateKeys()))
	for _, key := range CanonicalGateKeys() {
		values[key] = overrides[key]
	}
	vector, err := NewGateVector(values)
	if err != nil {
		t.Fatalf("new Gate vector: %v", err)
	}
	return vector
}

// Attempt 内的 park/approval 发布先完成状态转移时，完成路径的 CAS 冲突是
// “状态已由其它原语拥有”，不能伪装成 Worker 自身失败写进 Worker Health。
func TestWorkerSettleRunCompletionAbsorbsCASConflict(t *testing.T) {
	worker, err := NewWorker(workflow.NewGORMStore(nil), WorkerConfig{
		Owner:          "worker-completion-cas",
		LeaseDuration:  time.Minute,
		MinPollBackoff: 25 * time.Millisecond,
		MaxPollBackoff: 200 * time.Millisecond,
		Observation:    WorkerObservation{WorkerID: "worker-completion-cas", Status: WorkerStatusRunning},
	})
	if err != nil {
		t.Fatalf("new Worker: %v", err)
	}
	conflict := fmt.Errorf("%w: expected=running actual=parked", workflow.ErrRunCASConflict)
	didWork, err := worker.settleRunCompletion(context.Background(), conflict, "execution failed")
	if err != nil || !didWork {
		t.Fatalf("CAS conflict settlement: didWork=%v err=%v", didWork, err)
	}
	if worker.observation.LastError != "" {
		t.Fatalf("CAS conflict left last_error=%q", worker.observation.LastError)
	}
	// 真实失败仍必须上抛。
	realErr := errors.New("complete run failed")
	didWork, err = worker.settleRunCompletion(context.Background(), realErr, "execution failed")
	if !didWork || !errors.Is(err, realErr) {
		t.Fatalf("real failure settlement: didWork=%v err=%v", didWork, err)
	}
}
