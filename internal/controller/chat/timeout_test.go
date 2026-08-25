package chat

import (
	"context"
	"testing"
	"time"

	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	chatsvc "SentinelOps/internal/service/chat"
)

func TestSSETerminalReplayDoesNotWaitForPollTick(t *testing.T) {
	service, err := chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      runtime.FrozenRuntimeSnapshot{},
		CreateRun:     func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) {
			return []workflow.StreamEvent{{ID: 3, RunID: "run-terminal", Type: workflow.EventRunCompleted}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := streamDurableEvents(context.Background(), service, "run-terminal", 2, func(workflow.StreamEvent) {}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= durableEventPollInterval {
		t.Fatalf("terminal replay took %s, want less than poll interval %s", elapsed, durableEventPollInterval)
	}
}
