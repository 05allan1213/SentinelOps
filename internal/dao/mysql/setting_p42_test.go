package mysql

import (
	"context"
	"strings"
	"testing"
)

func TestEffectiveGateAuditIsAtomicQueryableAndRedacted(t *testing.T) {
	_, db, dsn := newDisposableDatabase(t, "p42_runtime_gate_audit")
	requireMigrationsUp(t, dsn)
	previousDB, previousErr := globalDB, initErr
	globalDB, initErr = db, nil
	t.Cleanup(func() {
		globalDB, initErr = previousDB, previousErr
	})
	keys := []string{
		"agent_runtime.enabled", "agent_runtime.accept_new_runs", "agent_runtime.shadow_mode",
		"agent_runtime.l1_writes", "agent_runtime.l2_writes", "agent_runtime.admin_query_database_debug",
		"mcp.enabled", "skill.enabled", "langfuse.enabled",
	}
	ctx := context.Background()
	SeedRuntimeGateSettings(ctx, keys)
	values := make(map[string]bool, len(keys))
	for _, key := range keys {
		values[key] = true
	}
	const secret = "sk-ABCDEFGHIJKLMNOPQRSTUV"
	change, err := UpdateRuntimeGatesWithAudit(ctx, keys, values, "admin-p42", "ticket api_key="+secret)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := GetSettings(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if stored[key] != "true" || change.OldValues[key] || !change.NewValues[key] {
			t.Fatalf("runtime Gate %s stored=%q change=%+v", key, stored[key], change)
		}
	}
	records, err := ListRuntimeGateAudit(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ActorID != "admin-p42" || records[0].ChangedAt.IsZero() {
		t.Fatalf("runtime Gate audit=%+v", records)
	}
	if strings.Contains(records[0].Reason, secret) || !strings.Contains(records[0].Reason, "[REDACTED]") {
		t.Fatalf("runtime Gate audit reason=%q", records[0].Reason)
	}
}

func TestEffectiveGateAuditFailureRollsBackWholeVector(t *testing.T) {
	_, db, dsn := newDisposableDatabase(t, "p42_runtime_gate_rollback")
	requireMigrationsUp(t, dsn)
	previousDB, previousErr := globalDB, initErr
	globalDB, initErr = db, nil
	t.Cleanup(func() {
		globalDB, initErr = previousDB, previousErr
	})
	keys := []string{
		"agent_runtime.enabled", "agent_runtime.accept_new_runs", "agent_runtime.shadow_mode",
		"agent_runtime.l1_writes", "agent_runtime.l2_writes", "agent_runtime.admin_query_database_debug",
		"mcp.enabled", "skill.enabled", "langfuse.enabled",
	}
	ctx := context.Background()
	SeedRuntimeGateSettings(ctx, keys)
	if err := db.Exec(`CREATE TRIGGER p42_reject_gate_audit BEFORE INSERT ON settings FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject p42 audit'`).Error; err != nil {
		t.Fatal(err)
	}
	values := make(map[string]bool, len(keys))
	for _, key := range keys {
		values[key] = true
	}
	if _, err := UpdateRuntimeGatesWithAudit(ctx, keys, values, "admin-p42", "atomic rollback"); err == nil {
		t.Fatal("audit insert failure did not abort runtime Gate update")
	}
	stored, err := GetSettings(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if stored[key] != "false" {
			t.Fatalf("runtime Gate %s escaped rolled-back transaction: %q", key, stored[key])
		}
	}
}
