package workflow

import (
	"context"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

// TestClaimTakeoverClosesExpiredAttemptProjection 覆盖 claim 接管过期租约时，
// 上一轮 Attempt 投影会被关闭，保证“第几次执行失败”的查询口径与真值一致。
func TestClaimTakeoverClosesExpiredAttemptProjection(t *testing.T) {
	db := newP07Database(t, "phase09_claim_takeover_attempt")
	store := NewGORMStore(db)
	ctx := fixture08UserContext("user-phase09-takeover")
	runID := "run-phase09-takeover"
	if _, err := store.CreateRunWithSessionLock(ctx, fixture08CreateInput(runID, "session-phase09-takeover")); err != nil {
		t.Fatalf("create takeover fixture: %v", err)
	}

	claimA, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-takeover-a", LeaseDuration: time.Minute,
	})
	if err != nil || !ok || claimA == nil {
		t.Fatalf("claim A = %#v ok=%v err=%v", claimA, ok, err)
	}
	// 模拟进程被杀后的接管窗口：租约仍在 A 名下，但已过期。
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", runID).
		Update("lease_until", gorm.Expr("DATE_SUB(CURRENT_TIMESTAMP(3), INTERVAL 1 SECOND)")).Error; err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	claimB, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{
		Owner: "worker-takeover-b", LeaseDuration: time.Minute,
	})
	if err != nil || !ok || claimB == nil {
		t.Fatalf("claim B = %#v ok=%v err=%v", claimB, ok, err)
	}
	if claimB.Token.Generation != claimA.Token.Generation+1 || claimB.Run.Attempt != claimA.Run.Attempt+1 {
		t.Fatalf("takeover identity = generation %d attempt %d, want generation %d attempt %d",
			claimB.Token.Generation, claimB.Run.Attempt, claimA.Token.Generation+1, claimA.Run.Attempt+1)
	}

	var stale mysql.WorkflowAttempt
	if err := db.Where("run_id = ? AND attempt = ?", runID, claimA.Run.Attempt).First(&stale).Error; err != nil {
		t.Fatalf("load stale attempt: %v", err)
	}
	if stale.FinishedAt == nil || stale.Status == nil || *stale.Status != RunStatusFailed ||
		stale.FailureCode == nil || *stale.FailureCode != "lease_expired" {
		t.Fatalf("stale attempt projection = status=%v failure=%v finished=%v", stale.Status, stale.FailureCode, stale.FinishedAt)
	}
}
