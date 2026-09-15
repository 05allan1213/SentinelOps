package workflow

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// claimRetryFixtureRun 建一个真实 durable Run 并走生产 Claim 路径，
// 供 Attempt 投影并发/幂等回归使用。
func claimRetryFixtureRun(t *testing.T, suffix, owner string) (*gorm.DB, *GORMStore, *ClaimedRun) {
	t.Helper()
	db := newP07Database(t, suffix)
	store := NewGORMStore(db)
	if _, err := store.CreateRunWithSessionLock(fixture08UserContext("user-"+suffix), fixture08CreateInput("run-"+suffix, "session-"+suffix)); err != nil {
		t.Fatalf("create Run: %v", err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), ClaimInput{Owner: owner, LeaseDuration: time.Minute})
	if err != nil || !ok || claimed == nil {
		t.Fatalf("claim Run: claimed=%#v ok=%v err=%v", claimed, ok, err)
	}
	return db, store, claimed
}

func TestClaimRetryReasonUsesStructuredMySQLCodes(t *testing.T) {
	deadlock := &driver.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock"}
	timeout := &driver.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded"}
	duplicate := &driver.MySQLError{Number: 1062, Message: "Duplicate entry"}
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"deadlock", fmt.Errorf("begin workflow attempt: %w", deadlock), "mysql_deadlock_1213"},
		{"lock wait timeout", timeout, "mysql_lock_wait_timeout_1205"},
		{"duplicate key is not retryable", duplicate, ""},
		{"plain error", errors.New("Error 1213 deadlock text only"), ""},
		{"nil", nil, ""},
	} {
		if got := ClaimRetryReason(test.err); got != test.want {
			t.Errorf("%s: ClaimRetryReason = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestClassifyClaimTransactionErrorOnlyWrapsRetryableConflicts(t *testing.T) {
	deadlock := &driver.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock"}
	wrapped := classifyClaimTransactionError(fmt.Errorf("begin workflow attempt: %w", deadlock))
	if !errors.Is(wrapped, ErrClaimRetryableTransaction) {
		t.Fatalf("retryable deadlock was not classified: %v", wrapped)
	}
	var mysqlErr *driver.MySQLError
	if !errors.As(wrapped, &mysqlErr) || mysqlErr.Number != 1213 {
		t.Fatalf("classified error lost the structured MySQL code: %v", wrapped)
	}

	fatal := fmt.Errorf("lock workflow run: %w", &driver.MySQLError{Number: 1146, Message: "Table doesn't exist"})
	if got := classifyClaimTransactionError(fatal); got != fatal || errors.Is(got, ErrClaimRetryableTransaction) {
		t.Fatalf("fatal error was reclassified: %v", got)
	}
	if got := classifyClaimTransactionError(nil); got != nil {
		t.Fatalf("nil error = %v", got)
	}
}

func TestIsDuplicateWorkflowAttemptErrorOnlyMatchesAttemptUniqueIndex(t *testing.T) {
	rightIndex := &driver.MySQLError{Number: 1062, Message: "Duplicate entry 'run-x-1' for key 'workflow_attempts.uidx_workflow_attempts_run_attempt'"}
	if !isDuplicateWorkflowAttemptError(rightIndex) {
		t.Fatal("attempt unique index duplicate was not recognized")
	}
	for _, test := range []struct {
		name string
		err  error
	}{
		{"primary key collision", &driver.MySQLError{Number: 1062, Message: "Duplicate entry 'id' for key 'workflow_attempts.PRIMARY'"}},
		{"other unique key", &driver.MySQLError{Number: 1062, Message: "Duplicate entry 'x' for key 'workflow_attempts.idx_workflow_attempts_run_status'"}},
		{"other code", &driver.MySQLError{Number: 1213, Message: "Deadlock found"}},
		{"plain error", errors.New("Duplicate entry for key 'uidx_workflow_attempts_run_attempt'")},
		{"nil", nil},
	} {
		if isDuplicateWorkflowAttemptError(test.err) {
			t.Errorf("%s: duplicate key accepted: %v", test.name, test.err)
		}
	}
}

// TestBeginAttemptTxAcceptsConcurrentDuplicateProjection 覆盖唯一索引兜底路径：
// 预检时另一事务的 Attempt 投影还不可见，INSERT 命中 1062 后必须用当前读
// 复核 fenced identity，而不是把并发重复插入当成 fatal error 或覆盖旧事实。
func TestBeginAttemptTxAcceptsConcurrentDuplicateProjection(t *testing.T) {
	db, store, claimed := claimRetryFixtureRun(t, "c02_attempt_duplicate", "worker-duplicate")
	if err := db.Exec("DELETE FROM workflow_attempts WHERE run_id = ? AND attempt = ?", claimed.Run.ID, claimed.Run.Attempt).Error; err != nil {
		t.Fatalf("clear attempt projection: %v", err)
	}

	concurrent, err := db.DB()
	if err != nil {
		t.Fatalf("raw DB: %v", err)
	}
	tx, err := concurrent.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin concurrent insert: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	inserted := make(chan error, 1)
	go func() {
		_, insertErr := tx.Exec(
			"INSERT INTO workflow_attempts (id, run_id, attempt, mode, status, lease_generation) VALUES (?, ?, ?, 'fresh', 'running', ?)",
			"concurrent-attempt", claimed.Run.ID, claimed.Run.Attempt, claimed.Token.Generation)
		inserted <- insertErr
	}()
	if insertErr := <-inserted; insertErr != nil {
		_ = tx.Rollback()
		t.Fatalf("concurrent attempt insert: %v", insertErr)
	}

	// 产品路径的预检看不到未提交投影，INSERT 会等待；提交后命中 1062 并走兜底复核。
	done := make(chan error, 1)
	go func() {
		done <- store.BeginAttemptTx(context.Background(), claimed.Token, claimed.Token.Owner)
	}()
	select {
	case early := <-done:
		t.Fatalf("BeginAttemptTx returned before the concurrent projection committed: %v", early)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit concurrent projection: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("BeginAttemptTx on duplicate projection returned error: %v", err)
	}

	var rows []mysql.WorkflowAttempt
	if err := db.Where("run_id = ? AND attempt = ?", claimed.Run.ID, claimed.Run.Attempt).Find(&rows).Error; err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if len(rows) != 1 || rows[0].LeaseGeneration == nil || *rows[0].LeaseGeneration != claimed.Token.Generation {
		t.Fatalf("attempt projection after duplicate fallback = %+v", rows)
	}
}

// TestBeginAttemptTxRejectsDuplicateProjectionFromNewerGeneration 保证幂等复核
// 不会把更新 generation 的投影当成当前 generation 的成功重放。
func TestBeginAttemptTxRejectsDuplicateProjectionFromNewerGeneration(t *testing.T) {
	db, store, claimed := claimRetryFixtureRun(t, "c02_attempt_duplicate_stale", "worker-duplicate-stale")
	if err := db.Exec("UPDATE workflow_attempts SET lease_generation = ? WHERE run_id = ? AND attempt = ?",
		claimed.Token.Generation+1, claimed.Run.ID, claimed.Run.Attempt).Error; err != nil {
		t.Fatalf("age attempt projection: %v", err)
	}
	if err := store.BeginAttemptTx(context.Background(), claimed.Token, claimed.Token.Owner); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale generation duplicate returned %v, want ErrLeaseLost", err)
	}
}
