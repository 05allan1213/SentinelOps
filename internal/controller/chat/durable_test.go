package chat

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	chatsvc "SentinelOps/internal/service/chat"
)

func TestSSEReplaysTerminalEventsWithoutExecution(t *testing.T) {
	service, err := chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      runtime.FrozenRuntimeSnapshot{},
		CreateRun:     func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) {
			return []workflow.StreamEvent{{ID: 9, RunID: "run-sse", Type: workflow.EventRunCompleted}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sent []workflow.StreamEvent
	if err := streamDurableEvents(context.Background(), service, "run-sse", 8, func(event workflow.StreamEvent) {
		sent = append(sent, event)
	}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].ID != 9 {
		t.Fatalf("sent=%+v, want terminal event replay", sent)
	}
}

func TestSSEReconnectAfterTerminalCursorCloses(t *testing.T) {
	var reads atomic.Int32
	service, err := chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      runtime.FrozenRuntimeSnapshot{},
		CreateRun:     func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		ListEvents: func(_ context.Context, _ string, afterSeq int64) ([]workflow.StreamEvent, error) {
			reads.Add(1)
			if afterSeq == 8 {
				return nil, nil
			}
			return []workflow.StreamEvent{{ID: 8, RunID: "run-terminal-cursor", Type: workflow.EventRunCompleted}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := streamDurableEvents(context.Background(), service, "run-terminal-cursor", 8, func(workflow.StreamEvent) {
		t.Fatal("terminal cursor probe must not replay an acknowledged event")
	}); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 2 {
		t.Fatalf("reads=%d, want after_seq and terminal boundary probes", reads.Load())
	}
}

func TestSSERetryableFailureContinuesUntilTerminalEvent(t *testing.T) {
	var reads atomic.Int32
	service, err := chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      runtime.FrozenRuntimeSnapshot{},
		CreateRun:     func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		ListEvents: func(_ context.Context, _ string, afterSeq int64) ([]workflow.StreamEvent, error) {
			reads.Add(1)
			if afterSeq == 0 {
				return []workflow.StreamEvent{{
					ID: 1, RunID: "run-retry-sse", Type: workflow.EventRunFailed,
					Payload: map[string]any{"schema": workflow.EventEnvelopeSchema, "data": map[string]any{"retryable": true}},
				}}, nil
			}
			return []workflow.StreamEvent{{ID: 2, RunID: "run-retry-sse", Type: workflow.EventRunCompleted}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sent []workflow.StreamEvent
	if err := streamDurableEvents(context.Background(), service, "run-retry-sse", 0, func(event workflow.StreamEvent) {
		sent = append(sent, event)
	}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0].ID != 1 || sent[1].ID != 2 || reads.Load() != 2 {
		t.Fatalf("sent=%+v reads=%d, want retry event followed by terminal event", sent, reads.Load())
	}
}

func TestSessionRunConflictMapsToHTTP409(t *testing.T) {
	if got := durableHTTPStatus(fmt.Errorf("create Run: %w", workflow.ErrSessionRunActive)); got != http.StatusConflict {
		t.Fatalf("status=%d, want 409", got)
	}
}

func TestSSEScopeFailureMapsToHTTP403(t *testing.T) {
	if got := durableHTTPStatus(fmt.Errorf("read events: %w", policy.ErrForbidden)); got != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", got)
	}
}
