package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"gorm.io/gorm"
)

// TestDeleteReportMissingRowReturnsNotFound 保护删除契约：目标不存在时必须返回
// gorm.ErrRecordNotFound（HTTP 404），不能把「没有删除任何记录」当成删除成功。
func TestDeleteReportMissingRowReturnsNotFound(t *testing.T) {
	_, db, dsn := newDisposableDatabase(t, "report_delete_contract")
	requireMigrationsUp(t, dsn)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleAdmin, Scope: policy.Scope{All: true}})
	ctx, err := ContextWithTransaction(ctx, db)
	if err != nil {
		t.Fatalf("bind disposable database: %v", err)
	}

	if err := DeleteReport(ctx, "missing-report"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("delete missing report err=%v want gorm.ErrRecordNotFound", err)
	}

	row := &Report{ID: "report-delete-contract", Title: "int2", Content: "body", Type: "custom", CreatedAt: time.Now().UTC()}
	if err := CreateReport(ctx, row); err != nil {
		t.Fatalf("create report: %v", err)
	}
	if err := DeleteReport(ctx, row.ID); err != nil {
		t.Fatalf("delete existing report: %v", err)
	}
	if err := DeleteReport(ctx, row.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("second delete err=%v want gorm.ErrRecordNotFound", err)
	}

}
