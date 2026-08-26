package system

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestEffectiveGateAdminQueryClosesBeforeDatabase(t *testing.T) {
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "admin-p42-query", Role: policy.RoleAdmin, Scope: policy.Scope{All: true},
	})
	checks := 0
	query := NewQueryDatabaseTool(func(context.Context) (bool, error) {
		checks++
		return false, nil
	})
	_, err := query.InvokableRun(ctx, `{"sql":"SELECT * FROM events"}`)
	if !errors.Is(err, policy.ErrForbidden) || checks != 1 {
		t.Fatalf("closed admin query err=%v gate_checks=%d", err, checks)
	}
}

func TestEffectiveGateAdminQueryRejectsNonAdminBeforeGateRead(t *testing.T) {
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "operator-p42-query", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "operator-p42-query"},
	})
	checks := 0
	query := NewQueryDatabaseTool(func(context.Context) (bool, error) {
		checks++
		return true, nil
	})
	_, err := query.InvokableRun(ctx, `{"sql":"SELECT * FROM events"}`)
	if !errors.Is(err, policy.ErrForbidden) || checks != 0 {
		t.Fatalf("non-admin query err=%v gate_checks=%d", err, checks)
	}
}
