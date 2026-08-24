package rageval

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestServiceLayerRechecksScope(t *testing.T) {
	t.Parallel()
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "server-user",
		Role:   policy.RoleViewer,
		Scope:  policy.Scope{UserID: "server-user"},
	})
	err := SubmitFeedback(ctx, "session", "client-user", 1, 1, nil)
	if !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("service accepted out-of-scope user_id: %v", err)
	}
}
