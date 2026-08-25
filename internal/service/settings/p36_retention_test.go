package settingssvc

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestRetentionAuditRejectsNonAdminRoles(t *testing.T) {
	for _, role := range []policy.Role{policy.RoleViewer, policy.RoleOperator, policy.RoleApprover} {
		ctx := policy.WithIdentity(context.Background(), policy.Identity{
			UserID: "user-" + string(role), Role: role, Scope: policy.Scope{UserID: "user-" + string(role)},
		})
		if err := SaveRetention(ctx, RetentionSettings{PayloadDays: 30, AuditDays: 180}, "reason"); !errors.Is(err, policy.ErrForbidden) {
			t.Fatalf("role %s error=%v, want forbidden", role, err)
		}
	}
}
