// T1 follow-up 机制探针：验证 Claim 事务里 Attempt 投影插入的死锁环是否来自
// 「先对不存在的 (run_id, attempt) 做 SELECT ... FOR UPDATE，再 INSERT」这一顺序。
//
// 该探针只使用目标库里的一张一次性表 `_sentinelops_deadlock_probe`，不读产品表：
//
//	-mode=locking-read 复现生产代码当前形状：
//	  tx: SELECT ... WHERE run_id=? AND attempt=1 FOR UPDATE （miss，取 gap lock）
//	  tx: INSERT (run_id, attempt=1)
//	-mode=plain-read  验证最小修复形状：
//	  tx: SELECT ... WHERE run_id=? AND attempt=1        （无锁读，不取 gap lock）
//	  tx: INSERT (run_id, attempt=1)
//
// 两个事务的新 key 落在同一个唯一索引 gap 里；locking-read 形状应稳定出现
// MySQL 1213（X gap lock 互不冲突，但都挡住对方的 insert intention），
// plain-read 形状应双双成功。
//
// 用法：
//
//	SENTINELOPS_DEADLOCK_PROBE_DSN='root:<password>@tcp(127.0.0.1:3307)/sentinelops_phase03?...' \
//	  go run ./scripts/evidence/deadlock-probe -mode=locking-read
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

const probeTable = "_sentinelops_deadlock_probe"

func main() {
	mode := flag.String("mode", "locking-read", "locking-read (production shape) or plain-read (candidate fix shape)")
	flag.Parse()
	lockingRead := *mode == "locking-read"
	if !lockingRead && *mode != "plain-read" {
		fmt.Fprintf(os.Stderr, "unknown -mode %q\n", *mode)
		os.Exit(2)
	}
	dsn := strings.TrimSpace(os.Getenv("SENTINELOPS_DEADLOCK_PROBE_DSN"))
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "SENTINELOPS_DEADLOCK_PROBE_DSN is required")
		os.Exit(2)
	}
	config, err := driver.ParseDSN(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse DSN: %v\n", err)
		os.Exit(2)
	}
	config.MultiStatements = true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		fmt.Fprintf(os.Stderr, "open MySQL: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := setup(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "setup probe table: %v\n", err)
		os.Exit(2)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+probeTable) }()

	first, err := db.Conn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pin first connection: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = first.Close() }()
	second, err := db.Conn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pin second connection: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = second.Close() }()

	// seed rows 之间留出 gap：probe-0100 / probe-0101 都落在 (probe-0000, probe-0200) 里。
	outcome, err := runPair(ctx, first, second, lockingRead, "probe-0100", "probe-0101")
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe run: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("mode=%s locking_read=%v\n", *mode, lockingRead)
	fmt.Printf("tx1_insert_error=%s\n", outcome.firstErr)
	fmt.Printf("tx2_insert_error=%s\n", outcome.secondErr)
	fmt.Printf("deadlock_1213_observed=%v\n", outcome.deadlockObserved)
}

type probeOutcome struct {
	firstErr         string
	secondErr        string
	deadlockObserved bool
}

func setup(ctx context.Context, db *sql.DB) error {
	statements := []string{
		"DROP TABLE IF EXISTS " + probeTable,
		"CREATE TABLE " + probeTable + ` (
			id VARCHAR(64) NOT NULL,
			run_id VARCHAR(64) NOT NULL,
			attempt INT UNSIGNED NOT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY uidx_probe_run_attempt (run_id, attempt)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		"INSERT INTO " + probeTable + " (id, run_id, attempt) VALUES ('seed-0', 'probe-0000', 1), ('seed-1', 'probe-0200', 1)",
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}
	return nil
}

// runPair 复刻 beginAttemptTx 的语句顺序，并让两个 INSERT 并发进入同一 gap。
func runPair(ctx context.Context, first, second *sql.Conn, lockingRead bool, firstRun, secondRun string) (probeOutcome, error) {
	firstTx, err := first.BeginTx(ctx, nil)
	if err != nil {
		return probeOutcome{}, err
	}
	secondTx, err := second.BeginTx(ctx, nil)
	if err != nil {
		_ = firstTx.Rollback()
		return probeOutcome{}, err
	}
	rollbackBoth := func() {
		_ = firstTx.Rollback()
		_ = secondTx.Rollback()
	}

	lookup := "SELECT id FROM " + probeTable + " WHERE run_id = ? AND attempt = 1"
	if lockingRead {
		lookup += " FOR UPDATE"
	}
	for _, item := range []struct {
		tx  *sql.Tx
		run string
	}{{firstTx, firstRun}, {secondTx, secondRun}} {
		var id string
		err := item.tx.QueryRowContext(ctx, lookup, item.run).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			rollbackBoth()
			return probeOutcome{}, fmt.Errorf("probe lookup %s: %w", item.run, err)
		}
		if err == nil {
			rollbackBoth()
			return probeOutcome{}, fmt.Errorf("probe lookup %s unexpectedly found %s", item.run, id)
		}
	}

	insert := "INSERT INTO " + probeTable + " (id, run_id, attempt) VALUES (?, ?, 1)"
	var (
		wait    sync.WaitGroup
		results [2]error
	)
	wait.Add(2)
	for index, item := range []struct {
		tx  *sql.Tx
		id  string
		run string
	}{{firstTx, "tx1", firstRun}, {secondTx, "tx2", secondRun}} {
		index, item := index, item
		go func() {
			defer wait.Done()
			_, results[index] = item.tx.ExecContext(ctx, insert, item.id, item.run)
		}()
	}
	wait.Wait()

	outcome := probeOutcome{}
	if results[0] != nil {
		outcome.firstErr = results[0].Error()
	}
	if results[1] != nil {
		outcome.secondErr = results[1].Error()
	}
	outcome.deadlockObserved = isDeadlock(results[0]) || isDeadlock(results[1])
	// 复现 1213 时其中一个事务已被 InnoDB 回滚，两个 rollback 都是安全的。
	if results[0] == nil {
		_ = firstTx.Commit()
	} else {
		_ = firstTx.Rollback()
	}
	if results[1] == nil {
		_ = secondTx.Commit()
	} else {
		_ = secondTx.Rollback()
	}
	return outcome, nil
}

func isDeadlock(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *driver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1213
}
