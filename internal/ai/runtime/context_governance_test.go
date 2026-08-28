package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestImmutableInputKeepsQueryOnly(t *testing.T) {
	run := mysql.WorkflowRun{ImmutableInputJSON: func() *string { value := `{"agent":"plan_agent","query":"current task"}`; return &value }()}
	input, err := BuildImmutableRunInput(run)
	if err != nil {
		t.Fatalf("decode immutable input: %v", err)
	}
	if input.Query != "current task" || input.Agent != "plan_agent" {
		t.Fatalf("input = %+v", input)
	}
}

func TestImmutableInputRejectsHistoryInQuery(t *testing.T) {
	value := `{"agent":"plan_agent","query":"current task","history":[{"role":"user","content":"old"}]}`
	_, err := BuildImmutableRunInput(mysql.WorkflowRun{ImmutableInputJSON: &value})
	if err == nil {
		t.Fatal("history-bearing query was accepted")
	}
}

func TestHistoryOnceFromSnapshotIsDurableRevisionOnly(t *testing.T) {
	snapshot := workflow.DurableContextSnapshot{Schema: workflow.DurableContextSnapshotSchema, History: json.RawMessage(`{"schema":"fo/session-state/v1","revision":2,"summary":"durable","history":[{"role":"user","content":"old"}]}`)}
	history, err := HistoryMessagesFromRevision(snapshot.History)
	if err != nil {
		t.Fatalf("decode durable history: %v", err)
	}
	if len(history) != 2 || history[0].Content != "【历史对话摘要】\n"+"durable" || history[1].Content != "old" {
		t.Fatalf("history = %+v", history)
	}
}

func TestRedisProjectionFailureDoesNotChangeMySQLCompletion(t *testing.T) {
	var completed bool
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "phase29-worker", LeaseDuration: time.Minute, MinPollBackoff: time.Millisecond, MaxPollBackoff: time.Second,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			return &workflow.ClaimedRun{Run: mysql.WorkflowRun{ID: "run-phase29", Status: workflow.RunStatusRunning}, Token: workflow.LeaseToken{RunID: "run-phase29", Owner: "phase29-worker", Generation: 1}}, true, nil
		},
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{OutputPayload: "ok", RevisionStateJSON: json.RawMessage(`{"schema":"fo/session-state/v1","revision":1}`)}, nil
		},
		Transition:      func(context.Context, workflow.RunTransition) error { return nil },
		Complete:        func(context.Context, workflow.CompleteRunInput) error { completed = true; return nil },
		ProjectRevision: func(context.Context, string, []byte) error { return context.DeadlineExceeded },
	})
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("worker completion failed after projection error: %v", err)
	}
	if !completed {
		t.Fatal("MySQL completion callback was not committed")
	}
}
