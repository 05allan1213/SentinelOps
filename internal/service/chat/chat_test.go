package chatsvc

import (
	"context"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

type stubWorkflowStore struct {
	appendCtxErr error
}

func (s *stubWorkflowStore) CreateRun(ctx context.Context, input workflow.WorkflowRunInput) (*mysql.WorkflowRun, error) {
	return nil, nil
}

func (s *stubWorkflowStore) AppendEvent(ctx context.Context, event workflow.StreamEvent) error {
	s.appendCtxErr = ctx.Err()
	return nil
}

func (s *stubWorkflowStore) ListEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]workflow.StreamEvent, error) {
	return nil, nil
}

func (s *stubWorkflowStore) SaveCheckpoint(ctx context.Context, snapshot workflow.CheckpointSnapshot) error {
	return nil
}

func (s *stubWorkflowStore) LatestCheckpoint(ctx context.Context, runID, checkpointKey string) (workflow.CheckpointSnapshot, error) {
	return workflow.CheckpointSnapshot{}, nil
}

func (s *stubWorkflowStore) FinishRun(ctx context.Context, runID, status, outputPayload, errorMessage string) error {
	return nil
}

func TestChatWorkflowRunAppendEventUsesContextWithoutCancel(t *testing.T) {
	store := &stubWorkflowStore{}
	run := &chatWorkflowRun{
		store:   store,
		runID:   "run-1",
		enabled: true,
	}

	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	run.appendEvent(parentCtx, "plan_step", map[string]any{"content": "test"})

	if store.appendCtxErr != nil {
		t.Fatalf("AppendEvent 不应继承已取消的 context，got=%v", store.appendCtxErr)
	}
	if run.seq != 1 {
		t.Fatalf("事件序号应自增到 1，got=%d", run.seq)
	}
}

func TestChatWorkflowRunAppendEventPreservesDeadline(t *testing.T) {
	store := &stubWorkflowStore{}
	run := &chatWorkflowRun{
		store:   store,
		runID:   "run-1",
		enabled: true,
	}

	parentCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	run.appendEvent(parentCtx, "plan_step", "test")

	if store.appendCtxErr != nil {
		t.Fatalf("AppendEvent 在 deadline 未到期时不应报错，got=%v", store.appendCtxErr)
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
