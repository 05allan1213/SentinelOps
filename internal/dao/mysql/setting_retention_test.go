package mysql

import (
	"context"
	"strings"
	"testing"
)

func TestRetentionAuditIsAtomicQueryableAndRedacted(t *testing.T) {
	_, db, dsn := newDisposableDatabase(t, "phase36_retention_audit")
	requireMigrationsUp(t, dsn)
	previousDB, previousErr := globalDB, initErr
	globalDB, initErr = db, nil
	t.Cleanup(func() {
		globalDB, initErr = previousDB, previousErr
	})
	ctx := context.Background()
	if err := SetSetting(ctx, RetentionPayloadDaysKey, "30"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(ctx, RetentionAuditDaysKey, "180"); err != nil {
		t.Fatal(err)
	}
	const secret = "sk-ABCDEFGHIJKLMNOPQRSTUV"
	if err := UpdateRetentionPolicyWithAudit(ctx, RetentionPolicyChange{
		ActorID: "admin-phase36", OldPayloadDays: 30, OldAuditDays: 180,
		NewPayloadDays: 45, NewAuditDays: 240, Reason: "ticket contains api_key=" + secret,
	}); err != nil {
		t.Fatal(err)
	}
	settings, err := GetSettings(ctx, []string{RetentionPayloadDaysKey, RetentionAuditDaysKey})
	if err != nil {
		t.Fatal(err)
	}
	if settings[RetentionPayloadDaysKey] != "45" || settings[RetentionAuditDaysKey] != "240" {
		t.Fatalf("retention settings=%v", settings)
	}
	records, err := ListRetentionPolicyAudit(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ActorID != "admin-phase36" || records[0].OldPayloadDays != 30 || records[0].NewAuditDays != 240 {
		t.Fatalf("retention audit=%+v", records)
	}
	if strings.Contains(records[0].Reason, secret) || !strings.Contains(records[0].Reason, "[REDACTED]") || records[0].ChangedAt.IsZero() {
		t.Fatalf("retention audit reason/time=%q %v", records[0].Reason, records[0].ChangedAt)
	}
}
