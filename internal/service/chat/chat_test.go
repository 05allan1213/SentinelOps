package chatsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestDurableEventsUsesCallerCancellation(t *testing.T) {
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      runtime.FrozenRuntimeSnapshot{},
		CreateRun:     func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		ListEvents: func(ctx context.Context, _ string, _ int64) ([]workflow.StreamEvent, error) {
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Events(ctx, "run-canceled", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("Events error=%v, want caller cancellation", err)
	}
}

func TestDurableCreatePreservesCallerDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      fixture20ServiceSnapshot(),
		CreateRun: func(ctx context.Context, _ workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			got, ok := ctx.Deadline()
			if !ok || !got.Equal(deadline) {
				t.Fatalf("CreateRun deadline=%v/%t, want %v", got, ok, deadline)
			}
			return &mysql.WorkflowRun{ID: "run-deadline", Status: workflow.RunStatusPending}, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	if _, err := service.CreateRun(ctx, CreateDurableRunRequest{SessionID: "session-deadline", Query: "deadline"}); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeThinkTimeoutCapsBackgroundThinking(t *testing.T) {
	if got := normalizeThinkTimeout(10 * time.Minute); got != 30*time.Second {
		t.Fatalf("预思考超时应限制在独立小预算内，got=%v want=%v", got, 30*time.Second)
	}
}

func TestNormalizeThinkTimeoutKeepsShorterBudget(t *testing.T) {
	if got := normalizeThinkTimeout(20 * time.Second); got != 20*time.Second {
		t.Fatalf("较短主预算下，预思考不应扩张预算，got=%v want=%v", got, 20*time.Second)
	}
}
