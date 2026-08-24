package trace

import (
	"context"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestClientUserIDCannotOverrideJWT(t *testing.T) {
	t.Parallel()
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "server-user",
		Role:   policy.RoleViewer,
		Scope:  policy.Scope{UserID: "server-user"},
	})
	clientTags := map[string]any{"server_user_id": "client-user", "safe": true}
	got := authoritativeTraceTags(ctx, clientTags)
	if got["server_user_id"] != "server-user" {
		t.Fatalf("client trace tag overrode server Identity: %v", got["server_user_id"])
	}
	if clientTags["server_user_id"] != "client-user" {
		t.Fatal("authoritativeTraceTags mutated caller tags")
	}
}
