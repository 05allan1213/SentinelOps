package trace

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestServiceLayerRechecksScope(t *testing.T) {
	t.Parallel()
	_, _, err := NewService().ListRuns(context.Background(), "", "", "", 1, 20)
	if !errors.Is(err, policy.ErrUnauthenticated) {
		t.Fatalf("trace service accepted a call without server Identity: %v", err)
	}
}
