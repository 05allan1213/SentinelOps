package chat

import (
	"context"
	"testing"
	"time"
)

func TestWorkflowPersistContextIgnoresCancellation(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	persistCtx := workflowPersistContext(parentCtx)
	if persistCtx.Err() != nil {
		t.Fatalf("workflowPersistContext 不应继承已取消状态，got=%v", persistCtx.Err())
	}
}

func TestWorkflowPersistContextPreservesDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	parentCtx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	persistCtx := workflowPersistContext(parentCtx)
	gotDeadline, ok := persistCtx.Deadline()
	if !ok {
		t.Fatal("workflowPersistContext 应保留 deadline")
	}
	if !gotDeadline.Equal(deadline) {
		t.Fatalf("deadline 不一致，got=%v want=%v", gotDeadline, deadline)
	}
	if persistCtx.Err() != nil {
		t.Fatalf("deadline 未到期时不应报错，got=%v", persistCtx.Err())
	}
}
