package chat

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	chatsvc "SentinelOps/internal/service/chat"
)

func TestSSEReadUsesRequestCancellation(t *testing.T) {
	var observed error
	service, err := chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      runtime.FrozenRuntimeSnapshot{},
		CreateRun:     func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		ListEvents: func(ctx context.Context, _ string, _ int64) ([]workflow.StreamEvent, error) {
			observed = ctx.Err()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = streamDurableEvents(ctx, service, "run-canceled-sse", 0, func(workflow.StreamEvent) {})
	if !errors.Is(err, context.Canceled) || !errors.Is(observed, context.Canceled) {
		t.Fatalf("stream error=%v observed read context=%v, want request cancellation", err, observed)
	}
}
