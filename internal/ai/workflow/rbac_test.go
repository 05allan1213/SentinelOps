package workflow

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestServiceLayerRechecksScope(t *testing.T) {
	t.Parallel()
	store := NewGORMStore(nil)
	err := store.AppendEvent(context.Background(), StreamEvent{RunID: "run"})
	if !errors.Is(err, policy.ErrUnauthenticated) {
		t.Fatalf("workflow store touched persistence without server Identity: %v", err)
	}
}
