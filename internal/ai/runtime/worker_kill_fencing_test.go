package runtime

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// 本文件是 S1「真实 Worker 崩溃 → 租约过期 → 第二 Worker 接管 → fencing 拒旧写」故障注入。
//
// 真实部分：两个独立 OS 进程、真实 MySQL、真实 runtime.Worker 的
// claim / heartbeat / transition / complete 原语、真实租约与 generation 条件更新。
// 受控部分：Agent 执行体被替换为确定性长任务（hold）与确定性短任务（work），
// 以避免把故障实验绑在外部模型 Provider 上；Checkpoint 由单独的 phase10/phase12 测试覆盖。
//
// 子进程复用同一个测试二进制（-test.run=^TestWorkerKillChildHelper$），与现有
// phase10 跨进程用例采用同一模式；因此子进程与父进程运行的是同一份产品代码。

const (
	killexChildFlag   = "SENTINELOPS_KILLEX_CHILD"
	killexMode        = "SENTINELOPS_KILLEX_MODE"
	killexDSN         = "SENTINELOPS_KILLEX_DSN"
	killexRunID       = "SENTINELOPS_KILLEX_RUN_ID"
	killexOwner       = "SENTINELOPS_KILLEX_OWNER"
	killexHash        = "SENTINELOPS_KILLEX_HASH"
	killexLeaseMS     = "SENTINELOPS_KILLEX_LEASE_MS"
	killexWorkMS      = "SENTINELOPS_KILLEX_WORK_MS"
	killexTokenFile   = "SENTINELOPS_KILLEX_TOKEN_FILE"
	killexResultFile  = "SENTINELOPS_KILLEX_RESULT_FILE"
	killexStaleGen    = "SENTINELOPS_KILLEX_STALE_GENERATION"
	killexKeepDB      = "SENTINELOPS_KILLEX_KEEP_DB"
	killexEvidenceEnv = "SENTINELOPS_KILLEX_EVIDENCE"

	killexModeHold  = "hold"
	killexModeWork  = "work"
	killexModeStale = "stale"
)

type killexLeaseIdentity struct {
	RunID      string `json:"run_id"`
	Owner      string `json:"owner"`
	Generation uint64 `json:"generation"`
}

type killexStaleResult struct {
	RunID           string `json:"run_id"`
	Owner           string `json:"owner"`
	Generation      uint64 `json:"generation"`
	Heartbeat       string `json:"heartbeat"`
	Transition      string `json:"transition"`
	Complete        string `json:"complete"`
	AllRejected     bool   `json:"all_rejected"`
	AllLeaseLost    bool   `json:"all_lease_lost"`
	ObservedRunJSON string `json:"observed_run"`
}

type killexAttemptFact struct {
	Attempt        uint   `json:"attempt"`
	Worker         string `json:"worker"`
	Status         string `json:"status"`
	Phase          string `json:"phase"`
	Generation     uint64 `json:"generation"`
	FailureCode    string `json:"failure_code"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at"`
	OpenProjection bool   `json:"open_projection"`
}

type killexEventFact struct {
	Seq       uint64 `json:"seq"`
	EventType string `json:"event_type"`
}

type killexEvidence struct {
	RunID                 string              `json:"run_id"`
	CompatibilityHash     string              `json:"compatibility_hash"`
	LeaseDurationMS       int64               `json:"lease_duration_ms"`
	WorkerAPID            int                 `json:"worker_a_pid"`
	WorkerBPID            int                 `json:"worker_b_pid"`
	ClaimAGeneration      uint64              `json:"claim_a_generation"`
	ClaimALeaseUntil      string              `json:"claim_a_lease_until"`
	ClaimAAttempt         uint                `json:"claim_a_attempt"`
	ClaimAEventCount      int64               `json:"claim_a_event_count"`
	AfterKillOwner        string              `json:"after_kill_lease_owner"`
	AfterKillStatus       string              `json:"after_kill_run_status"`
	TakeoverOwner         string              `json:"takeover_owner"`
	TakeoverGeneration    uint64              `json:"takeover_generation"`
	TakeoverAttempt       uint                `json:"takeover_attempt"`
	TakeoverObservedDBNow string              `json:"takeover_observed_db_now"`
	TakeoverAfterExpiry   bool                `json:"takeover_after_lease_expiry"`
	ReapGeneration        uint64              `json:"reap_generation,omitempty"`
	ReapClosedAttemptOne  bool                `json:"reap_closed_attempt_one,omitempty"`
	FinalStatus           string              `json:"final_run_status"`
	FinalAttempt          uint                `json:"final_attempt"`
	FinalGeneration       uint64              `json:"final_lease_generation"`
	FinalLastEventSeq     uint64              `json:"final_last_event_seq"`
	FinalOutputPayload    string              `json:"final_output_payload"`
	Attempts              []killexAttemptFact `json:"attempts"`
	Events                []killexEventFact   `json:"events"`
	StaleWriteAtTakeover  *killexStaleResult  `json:"stale_write_during_takeover,omitempty"`
	StaleWriteAfterEnd    *killexStaleResult  `json:"stale_write_after_completion,omitempty"`
	BoundaryHeartbeatPre  string              `json:"boundary_heartbeat_before_expiry,omitempty"`
	BoundaryHeartbeatPost string              `json:"boundary_heartbeat_after_reap,omitempty"`
}

// TestWorkerProcessKillLeaseTakeoverFencing 是 S1 主实验：claim-only 接管路径。
func TestWorkerProcessKillLeaseTakeoverFencing(t *testing.T) {
	if os.Getenv(killexChildFlag) != "" {
		t.Skip("fault-injection helper runs only in child processes")
	}
	db := killexNewRuntimeDatabase(t, "claim_only")
	_, runID, hash := killexCreatePendingRun(t, db, "kill-claim-only")
	evidencePath := killexEvidencePath(t, "claim_only")
	lease := 5 * time.Second
	tokenAPath := filepath.Join(t.TempDir(), "worker-a-token.json")
	var bufferA bytes.Buffer

	childA := killexStartChild(t, &bufferA, map[string]string{
		killexMode:      killexModeHold,
		killexDSN:       killexChildDSN(t, db),
		killexRunID:     runID,
		killexOwner:     "worker-kill-a",
		killexHash:      hash,
		killexLeaseMS:   strconv.FormatInt(lease.Milliseconds(), 10),
		killexTokenFile: tokenAPath,
	})
	evidence := killexEvidence{RunID: runID, CompatibilityHash: hash, LeaseDurationMS: lease.Milliseconds(), WorkerAPID: childA.Process.Pid}

	tokenA := killexWaitForToken(t, tokenAPath, childA, 60*time.Second)
	if tokenA.Owner != "worker-kill-a" || tokenA.RunID != runID || tokenA.Generation == 0 {
		t.Fatalf("worker A token = %#v", tokenA)
	}
	claimA := killexReadRun(t, db, runID)
	if claimA.LeaseOwner == nil || *claimA.LeaseOwner != tokenA.Owner || claimA.LeaseGeneration != tokenA.Generation ||
		claimA.Attempt != 1 || claimA.Status != workflow.RunStatusRunning || claimA.LeaseUntil == nil {
		t.Fatalf("run truth after A claim = %s", killexDescribeRun(claimA))
	}
	evidence.ClaimAGeneration = claimA.LeaseGeneration
	evidence.ClaimAAttempt = claimA.Attempt
	evidence.ClaimALeaseUntil = claimA.LeaseUntil.UTC().Format(time.RFC3339Nano)
	evidence.ClaimAEventCount = killexCountEvents(t, db, runID)

	// 真实 SIGKILL：Worker A 在持有租约、心跳仍在续期时被杀。
	if err := childA.Process.Kill(); err != nil {
		t.Fatalf("kill worker A: %v", err)
	}
	waitErr := childA.Wait()
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || !exitErr.ProcessState.Sys().(syscall.WaitStatus).Signaled() {
		t.Fatalf("worker A wait = %v, want signaled termination", waitErr)
	}
	killedAt := killexReadRun(t, db, runID)
	if killedAt.LeaseOwner == nil || *killedAt.LeaseOwner != "worker-kill-a" || killedAt.Status != workflow.RunStatusRunning {
		t.Fatalf("run truth after kill = %s", killexDescribeRun(killedAt))
	}
	evidence.AfterKillOwner = *killedAt.LeaseOwner
	evidence.AfterKillStatus = killedAt.Status

	// Worker B 在 A 的租约仍然有效时启动：它必须拿不到这个 Run。
	tokenBPath := filepath.Join(t.TempDir(), "worker-b-token.json")
	var bufferB bytes.Buffer
	childB := killexStartChild(t, &bufferB, map[string]string{
		killexMode:      killexModeWork,
		killexDSN:       killexChildDSN(t, db),
		killexRunID:     runID,
		killexOwner:     "worker-kill-b",
		killexHash:      hash,
		killexLeaseMS:   strconv.FormatInt(lease.Milliseconds(), 10),
		killexWorkMS:    "4000",
		killexTokenFile: tokenBPath,
	})
	evidence.WorkerBPID = childB.Process.Pid

	takeover := killexWaitForTakeover(t, db, childB, runID, "worker-kill-a", claimA.LeaseUntil)
	evidence.TakeoverOwner = takeover.owner
	evidence.TakeoverGeneration = takeover.generation
	evidence.TakeoverAttempt = takeover.attempt
	evidence.TakeoverObservedDBNow = takeover.dbNow
	evidence.TakeoverAfterExpiry = true

	// 旧执行者「复活」：在 B 正持有租约执行时，用被杀进程留下的真实 token 写库。
	// 这是 fencing 的判定窗口：租约有效者已换人，旧 generation 必须全线被拒。
	staleAtTakeover := killexRunStaleWriter(t, db, tokenA)
	if !staleAtTakeover.AllLeaseLost {
		t.Fatalf("stale writes during takeover were not rejected with lease lost: %#v", staleAtTakeover)
	}
	evidence.StaleWriteAtTakeover = &staleAtTakeover
	duringTakeover := killexReadRun(t, db, runID)
	if duringTakeover.Status != workflow.RunStatusRunning || duringTakeover.LeaseOwner == nil || *duringTakeover.LeaseOwner != "worker-kill-b" {
		t.Fatalf("stale writer disturbed the takeover owner: %s", killexDescribeRun(duringTakeover))
	}

	if err := childB.Wait(); err != nil {
		t.Fatalf("worker B exit: %v\n%s", err, bufferB.String())
	}
	finished := killexReadRun(t, db, runID)
	if finished.Status != workflow.RunStatusSucceeded || finished.Attempt != 2 || finished.OutputPayload == "" {
		t.Fatalf("run truth after B completed = %s", killexDescribeRun(finished))
	}
	evidence.FinalStatus = finished.Status
	evidence.FinalAttempt = finished.Attempt
	evidence.FinalGeneration = finished.LeaseGeneration
	evidence.FinalLastEventSeq = finished.LastEventSeq
	evidence.FinalOutputPayload = finished.OutputPayload
	evidence.Attempts = killexAttemptFacts(t, db, runID)
	evidence.Events = killexEventFacts(t, db, runID)

	// claim 接管现在会关闭上一轮 Attempt 投影，与显式回收保持同一查询口径。
	oldAttempt := killexReadAttempt(t, db, runID, 1)
	if oldAttempt.FinishedAt == nil || killexValue(oldAttempt.Status) != workflow.RunStatusFailed ||
		killexValue(oldAttempt.FailureCode) != "lease_expired" {
		t.Fatalf("claim takeover did not close attempt 1: status=%s failure=%s finished=%v",
			killexValue(oldAttempt.Status), killexValue(oldAttempt.FailureCode), oldAttempt.FinishedAt)
	}

	// Run 已终态后旧 token 仍然写不动任何真值。
	staleAfterCompletion := killexRunStaleWriter(t, db, tokenA)
	if !staleAfterCompletion.AllRejected {
		t.Fatalf("stale writes after completion were accepted: %#v", staleAfterCompletion)
	}
	evidence.StaleWriteAfterEnd = &staleAfterCompletion
	afterStale := killexReadRun(t, db, runID)
	if afterStale.Status != finished.Status || afterStale.LastEventSeq != finished.LastEventSeq ||
		killexCountEvents(t, db, runID) != int64(len(evidence.Events)) || afterStale.OutputPayload != finished.OutputPayload {
		t.Fatalf("stale writer changed truth: before=%s after=%s", killexDescribeRun(finished), killexDescribeRun(afterStale))
	}
	killexWriteEvidence(t, evidencePath, evidence)
}

// TestWorkerProcessKillReapClosesStaleAttempt 覆盖显式 reap primitive 路径：
// 清空过期执行权、generation+1、旧 Attempt 记为 lease_expired，然后第二 Worker 接管。
func TestWorkerProcessKillReapClosesStaleAttempt(t *testing.T) {
	if os.Getenv(killexChildFlag) != "" {
		t.Skip("fault-injection helper runs only in child processes")
	}
	db := killexNewRuntimeDatabase(t, "reap_path")
	store, runID, hash := killexCreatePendingRun(t, db, "kill-reap-path")
	evidencePath := killexEvidencePath(t, "reap_path")
	lease := 5 * time.Second
	tokenAPath := filepath.Join(t.TempDir(), "worker-reap-a-token.json")
	var bufferReapA bytes.Buffer

	childA := killexStartChild(t, &bufferReapA, map[string]string{
		killexMode:      killexModeHold,
		killexDSN:       killexChildDSN(t, db),
		killexRunID:     runID,
		killexOwner:     "worker-reap-a",
		killexHash:      hash,
		killexLeaseMS:   strconv.FormatInt(lease.Milliseconds(), 10),
		killexTokenFile: tokenAPath,
	})
	evidence := killexEvidence{RunID: runID, CompatibilityHash: hash, LeaseDurationMS: lease.Milliseconds(), WorkerAPID: childA.Process.Pid}
	tokenA := killexWaitForToken(t, tokenAPath, childA, 60*time.Second)
	claimA := killexReadRun(t, db, runID)
	evidence.ClaimAGeneration = claimA.LeaseGeneration
	evidence.ClaimAAttempt = claimA.Attempt
	evidence.ClaimALeaseUntil = claimA.LeaseUntil.UTC().Format(time.RFC3339Nano)
	evidence.ClaimAEventCount = killexCountEvents(t, db, runID)

	if err := childA.Process.Kill(); err != nil {
		t.Fatalf("kill worker A: %v", err)
	}
	_ = childA.Wait()
	killedAt := killexReadRun(t, db, runID)
	evidence.AfterKillOwner = killexValue(killedAt.LeaseOwner)
	evidence.AfterKillStatus = killedAt.Status

	// 等待租约过期后，用真实 Worker 对象调用显式 reap primitive。
	killexWaitForLeaseExpiry(t, db, runID, claimA.LeaseUntil, 30*time.Second)
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-reap-observer", LeaseDuration: lease,
		MinPollBackoff: 100 * time.Millisecond, MaxPollBackoff: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	reaped, err := worker.ReapExpired(context.Background(), 10)
	if err != nil || reaped != 1 {
		t.Fatalf("reap expired lease: reaped=%d err=%v", reaped, err)
	}
	reapedRun := killexReadRun(t, db, runID)
	evidence.ReapGeneration = reapedRun.LeaseGeneration
	oldAttempt := killexReadAttempt(t, db, runID, 1)
	if oldAttempt.FinishedAt == nil || killexValue(oldAttempt.FailureCode) != "lease_expired" || killexValue(oldAttempt.Status) != workflow.RunStatusFailed {
		t.Fatalf("reaped attempt projection = %+v", oldAttempt)
	}
	evidence.ReapClosedAttemptOne = true

	var bufferReapB bytes.Buffer
	childB := killexStartChild(t, &bufferReapB, map[string]string{
		killexMode:      killexModeWork,
		killexDSN:       killexChildDSN(t, db),
		killexRunID:     runID,
		killexOwner:     "worker-reap-b",
		killexHash:      hash,
		killexLeaseMS:   strconv.FormatInt(lease.Milliseconds(), 10),
		killexTokenFile: filepath.Join(t.TempDir(), "worker-reap-b-token.json"),
	})
	evidence.WorkerBPID = childB.Process.Pid
	if err := childB.Wait(); err != nil {
		t.Fatalf("worker B exit: %v\n%s", err, bufferReapB.String())
	}
	finished := killexReadRun(t, db, runID)
	if finished.Status != workflow.RunStatusSucceeded || finished.Attempt != 2 {
		t.Fatalf("run truth after reap + takeover = %s", killexDescribeRun(finished))
	}
	evidence.TakeoverOwner = killexValue(finished.LeaseOwner)
	if attemptTwo := killexReadAttempt(t, db, runID, 2); attemptTwo.WorkerID != nil {
		evidence.TakeoverOwner = *attemptTwo.WorkerID
	}
	evidence.TakeoverGeneration = finished.LeaseGeneration
	evidence.TakeoverAttempt = finished.Attempt
	evidence.TakeoverAfterExpiry = finished.LeaseGeneration > tokenA.Generation
	evidence.FinalStatus = finished.Status
	evidence.FinalAttempt = finished.Attempt
	evidence.FinalGeneration = finished.LeaseGeneration
	evidence.FinalLastEventSeq = finished.LastEventSeq
	evidence.FinalOutputPayload = finished.OutputPayload
	evidence.Attempts = killexAttemptFacts(t, db, runID)
	evidence.Events = killexEventFacts(t, db, runID)
	var openAttempts int64
	if err := db.Model(&mysql.WorkflowAttempt{}).Where("run_id = ? AND finished_at IS NULL", runID).Count(&openAttempts).Error; err != nil {
		t.Fatal(err)
	}
	if openAttempts != 0 {
		t.Fatalf("open attempts after reap path = %d, want 0", openAttempts)
	}
	killexWriteEvidence(t, evidencePath, evidence)
}

// TestWorkerProcessKillFencingBoundaryIsGeneration 记录 fencing 的真实边界：
// 进程死亡本身不是拦截条件——只要旧租约仍在有效期内，旧 token 的续约依然被数据库接受；
// 接管（generation 递增）之后，同一 token 才会被全线拒绝。
func TestWorkerProcessKillFencingBoundaryIsGeneration(t *testing.T) {
	if os.Getenv(killexChildFlag) != "" {
		t.Skip("fault-injection helper runs only in child processes")
	}
	db := killexNewRuntimeDatabase(t, "boundary")
	store, runID, hash := killexCreatePendingRun(t, db, "kill-boundary")
	evidencePath := killexEvidencePath(t, "boundary")
	lease := 6 * time.Second
	tokenAPath := filepath.Join(t.TempDir(), "worker-boundary-a-token.json")
	var bufferA bytes.Buffer
	childA := killexStartChild(t, &bufferA, map[string]string{
		killexMode:      killexModeHold,
		killexDSN:       killexChildDSN(t, db),
		killexRunID:     runID,
		killexOwner:     "worker-boundary-a",
		killexHash:      hash,
		killexLeaseMS:   strconv.FormatInt(lease.Milliseconds(), 10),
		killexTokenFile: tokenAPath,
	})
	evidence := killexEvidence{RunID: runID, CompatibilityHash: hash, LeaseDurationMS: lease.Milliseconds(), WorkerAPID: childA.Process.Pid}
	tokenA := killexWaitForToken(t, tokenAPath, childA, 60*time.Second)
	claimA := killexReadRun(t, db, runID)
	evidence.ClaimAGeneration = claimA.LeaseGeneration
	evidence.ClaimAAttempt = claimA.Attempt
	evidence.ClaimALeaseUntil = claimA.LeaseUntil.UTC().Format(time.RFC3339Nano)
	if err := childA.Process.Kill(); err != nil {
		t.Fatalf("kill worker A: %v", err)
	}
	_ = childA.Wait()

	stale := workflow.LeaseToken{RunID: tokenA.RunID, Owner: tokenA.Owner, Generation: tokenA.Generation}
	evidence.BoundaryHeartbeatPre = killexErrorClass(store.HeartbeatLease(context.Background(), stale, 500*time.Millisecond))
	if evidence.BoundaryHeartbeatPre != "accepted" {
		t.Fatalf("heartbeat before expiry = %s, want accepted (lease still owns the run)", evidence.BoundaryHeartbeatPre)
	}
	time.Sleep(600 * time.Millisecond)
	reaped, err := store.ReapExpiredLeases(context.Background(), workflow.ReapInput{Limit: 10})
	if err != nil || reaped != 1 {
		t.Fatalf("reap revived lease: reaped=%d err=%v", reaped, err)
	}
	evidence.ReapGeneration = killexReadRun(t, db, runID).LeaseGeneration
	evidence.BoundaryHeartbeatPost = killexErrorClass(store.HeartbeatLease(context.Background(), stale, time.Minute))
	if evidence.BoundaryHeartbeatPost != "lease_lost" {
		t.Fatalf("heartbeat after reap = %s, want lease_lost", evidence.BoundaryHeartbeatPost)
	}
	evidence.ReapClosedAttemptOne = killexReadAttempt(t, db, runID, 1).FinishedAt != nil
	evidence.Attempts = killexAttemptFacts(t, db, runID)
	evidence.Events = killexEventFacts(t, db, runID)
	killexWriteEvidence(t, evidencePath, evidence)
}

// TestWorkerKillChildHelper 是上面两个实验使用的子进程入口。
func TestWorkerKillChildHelper(t *testing.T) {
	if os.Getenv(killexChildFlag) == "" {
		t.Skip("helper runs only in child processes")
	}
	mode := os.Getenv(killexMode)
	db, err := gorm.Open(gormmysql.Open(os.Getenv(killexDSN)), &gorm.Config{})
	if err != nil {
		t.Fatalf("child open MySQL: %v", err)
	}
	store := workflow.NewGORMStore(db)
	switch mode {
	case killexModeHold, killexModeWork:
		killexRunWorkerChild(t, store, mode)
	case killexModeStale:
		killexRunStaleChild(t, store)
	default:
		t.Fatalf("unknown child mode %q", mode)
	}
}

func killexRunWorkerChild(t *testing.T, store *workflow.GORMStore, mode string) {
	owner := os.Getenv(killexOwner)
	runID := os.Getenv(killexRunID)
	leaseMS, err := strconv.ParseInt(os.Getenv(killexLeaseMS), 10, 64)
	if err != nil {
		t.Fatalf("child lease duration: %v", err)
	}
	var claimed atomic.Bool
	worker, err := NewWorker(store, WorkerConfig{
		Owner: owner, LeaseDuration: time.Duration(leaseMS) * time.Millisecond,
		MinPollBackoff: 100 * time.Millisecond, MaxPollBackoff: time.Second,
		Observation: WorkerObservation{WorkerID: owner, RuntimeCompatibilityHash: os.Getenv(killexHash)},
		Execute: func(ctx context.Context, claimedRun *workflow.ClaimedRun) (RunExecutionResult, error) {
			claimed.Store(true)
			if err := killexWriteJSON(os.Getenv(killexTokenFile), killexLeaseIdentity{
				RunID: claimedRun.Run.ID, Owner: claimedRun.Token.Owner, Generation: claimedRun.Token.Generation,
			}); err != nil {
				return RunExecutionResult{}, err
			}
			if mode == killexModeHold {
				// 保持执行中，等待父进程 SIGKILL；心跳 goroutine 继续按 lease/3 续约。
				time.Sleep(10 * time.Minute)
			}
			workMS := int64(300)
			if configured := os.Getenv(killexWorkMS); configured != "" {
				parsed, parseErr := strconv.ParseInt(configured, 10, 64)
				if parseErr != nil || parsed <= 0 {
					return RunExecutionResult{}, fmt.Errorf("invalid child work duration %q", configured)
				}
				workMS = parsed
			}
			time.Sleep(time.Duration(workMS) * time.Millisecond)
			return RunExecutionResult{
				OutputPayload:     `{"worker":"` + owner + `","result":"continued-after-takeover"}`,
				RevisionStateJSON: []byte(`{"session_revision":1,"owner":"` + owner + `"}`),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("child build Worker: %v", err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("child RunOnce: %v", err)
		}
		if mode == killexModeHold && claimed.Load() {
			// 保持进程存活直到父进程 kill。
			time.Sleep(10 * time.Minute)
		}
		var run mysql.WorkflowRun
		if err := store.DB().First(&run, "id = ?", runID).Error; err != nil {
			t.Fatalf("child read run: %v", err)
		}
		switch run.Status {
		case workflow.RunStatusSucceeded, workflow.RunStatusFailed, workflow.RunStatusCanceled:
			if err := killexWriteJSON(os.Getenv(killexResultFile), map[string]any{
				"run_id": runID, "owner": owner, "status": run.Status, "attempt": run.Attempt,
				"generation": run.LeaseGeneration,
			}); err != nil {
				t.Fatalf("child write result: %v", err)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child %s did not reach terminal run state before deadline", owner)
}

// killexRunStaleChild 模拟"旧 Worker 复活"：使用被杀进程真实持有的 owner/generation
// 调用生产的写 primitive，验证数据库层 fencing 全部拒绝。
func killexRunStaleChild(t *testing.T, store *workflow.GORMStore) {
	generation, err := strconv.ParseUint(os.Getenv(killexStaleGen), 10, 64)
	if err != nil {
		t.Fatalf("stale child generation: %v", err)
	}
	token := workflow.LeaseToken{RunID: os.Getenv(killexRunID), Owner: os.Getenv(killexOwner), Generation: generation}
	var owner mysql.WorkflowRun
	if err := store.DB().Select("user_id").Where("id = ?", token.RunID).First(&owner).Error; err != nil {
		t.Fatalf("stale child read run owner: %v", err)
	}
	// 复活进程持有的是原 Run 的调用身份；fencing 必须在授权之后仍然拒绝旧 generation。
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: owner.UserID, Role: policy.RoleOperator, Scope: policy.Scope{UserID: owner.UserID},
	})
	result := killexStaleResult{RunID: token.RunID, Owner: token.Owner, Generation: token.Generation}
	result.Heartbeat = killexErrorClass(store.HeartbeatLease(ctx, token, time.Minute))
	result.Transition = killexErrorClass(store.TransitionRunWithEvent(ctx, workflow.RunTransition{
		RunID: token.RunID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: workflow.RunStatusWaitingApproval,
		Lease: token, Event: workflow.WorkflowEventInput{Type: workflow.EventApprovalRequested},
	}))
	result.Complete = killexErrorClass(store.CompleteRunAndCommitSession(ctx, workflow.CompleteRunInput{
		RunID: token.RunID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: workflow.RunStatusFailed, Lease: token,
	}))
	result.AllRejected = result.Heartbeat != "accepted" && result.Transition != "accepted" && result.Complete != "accepted"
	result.AllLeaseLost = result.Heartbeat == "lease_lost" && result.Transition == "lease_lost" && result.Complete == "lease_lost"
	var run mysql.WorkflowRun
	if err := store.DB().First(&run, "id = ?", token.RunID).Error; err != nil {
		t.Fatalf("stale child read run: %v", err)
	}
	result.ObservedRunJSON = killexDescribeRun(&run)
	if err := killexWriteJSON(os.Getenv(killexResultFile), result); err != nil {
		t.Fatalf("stale child write result: %v", err)
	}
	if !result.AllRejected {
		t.Fatalf("stale writes escaped fencing: %#v", result)
	}
}

func killexRunStaleWriter(t *testing.T, db *gorm.DB, token killexLeaseIdentity) killexStaleResult {
	t.Helper()
	resultPath := filepath.Join(t.TempDir(), "stale-result.json")
	command := killexNewChildCommand(t, map[string]string{
		killexMode:       killexModeStale,
		killexDSN:        killexChildDSN(t, db),
		killexRunID:      token.RunID,
		killexOwner:      token.Owner,
		killexStaleGen:   strconv.FormatUint(token.Generation, 10),
		killexResultFile: resultPath,
	})
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("stale writer child: %v\n%s", err, output)
	}
	payload, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read stale writer result: %v", err)
	}
	var result killexStaleResult
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("decode stale writer result: %v", err)
	}
	return result
}

func killexNewChildCommand(t *testing.T, env map[string]string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestWorkerKillChildHelper$", "-test.v")
	command.Env = append(os.Environ(), killexChildFlag+"=1")
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	return command
}

func killexStartChild(t *testing.T, out *bytes.Buffer, env map[string]string) *exec.Cmd {
	t.Helper()
	command := killexNewChildCommand(t, env)
	if out != nil {
		command.Stdout = out
		command.Stderr = out
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})
	return command
}

func killexWaitForToken(t *testing.T, path string, command *exec.Cmd, timeout time.Duration) killexLeaseIdentity {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		payload, err := os.ReadFile(path)
		if err == nil && len(payload) > 0 {
			var identity killexLeaseIdentity
			if err := json.Unmarshal(payload, &identity); err != nil {
				t.Fatalf("decode worker token: %v", err)
			}
			return identity
		}
		if command.ProcessState != nil && command.ProcessState.Exited() {
			t.Fatalf("child process exited before claiming: %v", command.ProcessState)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("child did not claim run within %s", timeout)
	return killexLeaseIdentity{}
}

type killexTakeoverFact struct {
	owner      string
	generation uint64
	attempt    uint
	dbNow      string
}

func killexWaitForTakeover(t *testing.T, db *gorm.DB, command *exec.Cmd, runID, previousOwner string, leaseUntilA *time.Time) killexTakeoverFact {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		var run mysql.WorkflowRun
		if err := db.First(&run, "id = ?", runID).Error; err != nil {
			t.Fatalf("read run during takeover: %v", err)
		}
		var dbNow time.Time
		if err := db.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&dbNow).Error; err != nil {
			t.Fatalf("read MySQL clock: %v", err)
		}
		if run.LeaseOwner != nil && *run.LeaseOwner != previousOwner {
			if leaseUntilA != nil && dbNow.Before(*leaseUntilA) {
				t.Fatalf("second worker claimed at %s before lease expiry %s", dbNow, leaseUntilA)
			}
			return killexTakeoverFact{owner: *run.LeaseOwner, generation: run.LeaseGeneration, attempt: run.Attempt, dbNow: dbNow.Format(time.RFC3339Nano)}
		}
		if command.ProcessState != nil && command.ProcessState.Exited() {
			t.Fatalf("worker B exited before takeover: %v", command.ProcessState)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no takeover observed within 45s")
	return killexTakeoverFact{}
}

func killexWaitForLeaseExpiry(t *testing.T, db *gorm.DB, runID string, leaseUntil *time.Time, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var dbNow time.Time
		if err := db.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&dbNow).Error; err != nil {
			t.Fatalf("read MySQL clock: %v", err)
		}
		if leaseUntil != nil && dbNow.After(*leaseUntil) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("lease for %s did not expire within %s", runID, timeout)
}

func killexCreatePendingRun(t *testing.T, db *gorm.DB, suffix string) (*workflow.GORMStore, string, string) {
	t.Helper()
	input := fixture14SnapshotInput(t)
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	store := workflow.NewGORMStore(db)
	userID := "operator-" + suffix
	identity := policy.Identity{UserID: userID, Role: policy.RoleOperator, Scope: policy.Scope{UserID: userID}}
	runID := "run-" + suffix
	if _, err := store.CreateRunWithSessionLock(policy.WithIdentity(context.Background(), identity), workflow.CreateRunInput{
		ID: runID, WorkflowKey: "kill-fencing-experiment", SessionID: "session-" + suffix,
		QueryText: "kill fencing experiment", ImmutableInputJSON: json.RawMessage(`{"agent":"test","query":"kill fencing experiment"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: json.RawMessage(`{}`),
		DeadlineAt: time.Now().Add(time.Hour), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	}); err != nil {
		t.Fatalf("create pending run: %v", err)
	}
	return store, runID, frozen.CompatibilityHash()
}

func killexNewRuntimeDatabase(t *testing.T, suffix string) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Fatal("SENTINELOPS_TEST_DSN is required for the kill/fencing experiment")
	}
	config, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse base test DSN: %v", err)
	}
	if config.DBName != "sentinelops_phase03" {
		t.Fatalf("refuse non-disposable database %q", config.DBName)
	}
	databaseName := "sentinelops_phase03_killex_" + suffix
	adminConfig := *config
	adminConfig.DBName = "mysql"
	adminDB, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	quotedName := "`" + databaseName + "`"
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName); err != nil {
		t.Fatalf("drop stale kill/fencing database: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + quotedName + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatalf("create kill/fencing database: %v", err)
	}
	keep := os.Getenv(killexKeepDB) == "1"
	t.Cleanup(func() {
		if keep {
			t.Logf("keeping experiment database %s on 127.0.0.1:3307 for manual inspection", databaseName)
			return
		}
		_, _ = adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName)
	})
	testConfig := *config
	testConfig.DBName = databaseName
	testDSN := testConfig.FormatDSN()
	gooseBinary := os.Getenv("SENTINELOPS_GOOSE_BIN")
	if gooseBinary == "" {
		gooseBinary, err = exec.LookPath("goose")
		if err != nil {
			t.Fatal("goose v3.27.3 is required for the kill/fencing experiment")
		}
	}
	versionOutput, err := exec.Command(gooseBinary, "-version").CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "v3.27.3") {
		t.Fatalf("goose version = %q err=%v, want v3.27.3", strings.TrimSpace(string(versionOutput)), err)
	}
	migrationDirectory, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gooseBinary, "-dir", migrationDirectory, "mysql", testDSN, "up").CombinedOutput(); err != nil {
		t.Fatalf("goose up: %v\n%s", err, strings.ReplaceAll(string(output), testDSN, "<redacted-dsn>"))
	}
	sqlDB, err := sql.Open("mysql", testDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	database, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func killexChildDSN(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var databaseName string
	if err := db.Raw("SELECT DATABASE()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read experiment database name: %v", err)
	}
	config, err := driver.ParseDSN(os.Getenv("SENTINELOPS_TEST_DSN"))
	if err != nil {
		t.Fatalf("parse base test DSN: %v", err)
	}
	config.DBName = databaseName
	return config.FormatDSN()
}

func killexReadRun(t *testing.T, db *gorm.DB, runID string) *mysql.WorkflowRun {
	t.Helper()
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatalf("read run %s: %v", runID, err)
	}
	return &run
}

func killexReadAttempt(t *testing.T, db *gorm.DB, runID string, attempt uint) mysql.WorkflowAttempt {
	t.Helper()
	var row mysql.WorkflowAttempt
	if err := db.First(&row, "run_id = ? AND attempt = ?", runID, attempt).Error; err != nil {
		t.Fatalf("read attempt %d of %s: %v", attempt, runID, err)
	}
	return row
}

func killexCountEvents(t *testing.T, db *gorm.DB, runID string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("run_id = ?", runID).Count(&count).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

func killexAttemptFacts(t *testing.T, db *gorm.DB, runID string) []killexAttemptFact {
	t.Helper()
	var rows []mysql.WorkflowAttempt
	if err := db.Where("run_id = ?", runID).Order("attempt ASC").Find(&rows).Error; err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	facts := make([]killexAttemptFact, 0, len(rows))
	for _, row := range rows {
		fact := killexAttemptFact{
			Attempt: row.Attempt, Worker: killexValue(row.WorkerID), Status: killexValue(row.Status),
			Phase: killexValue(row.CurrentPhase), FailureCode: killexValue(row.FailureCode),
			OpenProjection: row.FinishedAt == nil,
		}
		if row.LeaseGeneration != nil {
			fact.Generation = *row.LeaseGeneration
		}
		if row.StartedAt != nil {
			fact.StartedAt = row.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		if row.FinishedAt != nil {
			fact.FinishedAt = row.FinishedAt.UTC().Format(time.RFC3339Nano)
		}
		facts = append(facts, fact)
	}
	return facts
}

func killexEventFacts(t *testing.T, db *gorm.DB, runID string) []killexEventFact {
	t.Helper()
	var rows []mysql.WorkflowEvent
	if err := db.Where("run_id = ?", runID).Order("seq ASC").Find(&rows).Error; err != nil {
		t.Fatalf("read events: %v", err)
	}
	facts := make([]killexEventFact, 0, len(rows))
	for _, row := range rows {
		facts = append(facts, killexEventFact{Seq: row.Seq, EventType: row.EventType})
	}
	return facts
}

func killexDescribeRun(run *mysql.WorkflowRun) string {
	if run == nil {
		return "<nil>"
	}
	return fmt.Sprintf("status=%s attempt=%d generation=%d lease_owner=%s lease_until=%s last_event_seq=%d output=%q",
		run.Status, run.Attempt, run.LeaseGeneration, killexValue(run.LeaseOwner),
		killexTime(run.LeaseUntil), run.LastEventSeq, run.OutputPayload)
}

func killexTime(value *time.Time) string {
	if value == nil {
		return "<nil>"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func killexValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func killexErrorClass(err error) string {
	switch {
	case err == nil:
		return "accepted"
	case errors.Is(err, workflow.ErrLeaseLost):
		return "lease_lost"
	case errors.Is(err, workflow.ErrRunCASConflict):
		return "cas_conflict"
	default:
		return "error:" + err.Error()
	}
}

func killexWriteJSON(path string, payload any) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

func killexEvidencePath(t *testing.T, suffix string) string {
	t.Helper()
	if configured := os.Getenv(killexEvidenceEnv); configured != "" {
		if err := os.MkdirAll(filepath.Dir(configured), 0o755); err != nil {
			t.Fatalf("create evidence directory: %v", err)
		}
		return configured
	}
	return filepath.Join(t.TempDir(), "killex-"+suffix+".json")
}

func killexWriteEvidence(t *testing.T, path string, evidence killexEvidence) {
	t.Helper()
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatalf("encode evidence: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	t.Logf("kill/fencing evidence written to %s", path)
}
