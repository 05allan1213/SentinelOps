package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	v1 "SentinelOps/api/chat/v1"
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

func TestChatV1ReconnectDoesNotCreateRun(t *testing.T) {
	var creates atomic.Int32
	service, err := chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return nil, nil
		},
		GetRun: func(context.Context, string) (*mysql.WorkflowRun, error) {
			return &mysql.WorkflowRun{ID: "run-c07", UserID: "owner-c07", SessionID: "session-c07", Status: workflow.RunStatusRunning, RuntimeMode: workflow.RuntimeModeDurableV1, LastEventSeq: 12}, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	controller := &ControllerV1{durable: service}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "owner-c07", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "owner-c07"}})
	run, cursor, err := controller.resolveRun(ctx, &v1.ChatReq{RunID: "run-c07", SessionId: "session-c07", LastSeq: 12})
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "run-c07" || cursor != 12 || creates.Load() != 0 {
		t.Fatalf("run=%+v cursor=%d creates=%d", run, cursor, creates.Load())
	}
}

func TestChatV1LastSeqRequiresRunID(t *testing.T) {
	controller := &ControllerV1{}
	if _, _, err := controller.resolveRun(context.Background(), &v1.ChatReq{SessionId: "session-c07", Query: "new", LastSeq: 1}); !errors.Is(err, chatsvc.ErrDurableReconnectInvalid) {
		t.Fatalf("resolveRun error=%v, want invalid reconnect", err)
	}
	if got := durableHTTPStatus(chatsvc.ErrDurableReconnectInvalid); got != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", got)
	}
}

func TestChatV1ReconnectStableStatusMapping(t *testing.T) {
	if got := durableHTTPStatus(chatsvc.ErrDurableRunForbidden); got != http.StatusForbidden {
		t.Fatalf("forbidden status=%d, want 403", got)
	}
	if got := durableHTTPStatus(chatsvc.ErrDurableRunNotFound); got != http.StatusNotFound {
		t.Fatalf("not found status=%d, want 404", got)
	}
}

func TestChatV1IdentityEventIDDoesNotAdvanceReconnectCursor(t *testing.T) {
	if got := durableIdentityEventID(false); got != 1 {
		t.Fatalf("new-run identity event id=%d, want 1", got)
	}
	if got := durableIdentityEventID(true); got != 0 {
		t.Fatalf("reconnect identity event id=%d, want 0 non-cursor id", got)
	}
}
