package runtime

// T1「多 Worker 并发正确性 + Claim SQL EXPLAIN」正式实验 harness。
//
// 真实部分：K 个独立 OS 子进程（复用同一测试二进制，跑的是同一份产品代码）
// 并发拉取 M 个真实 Run；真实 MySQL 8.0；生产 Worker 的 claim / heartbeat /
// transition / complete 原语；真实 run.claimed 事件与 workflow_attempts 投影。
// 受控部分：Agent 执行体替换为确定性短任务（固定 sleep + 固定 OutputPayload），
// 避免把并发调度实验绑在外部模型 Provider 上；每次执行写入进程内执行日志，
// 用于交叉核对「认领事件 / attempt 投影 / 实际执行调用」三方真值。
//
// 本文件不修改任何产品代码。证据落盘到 SENTINELOPS_CONCW_EVIDENCE_DIR。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	concwChildFlag   = "SENTINELOPS_CONCW_CHILD"
	concwMode        = "SENTINELOPS_CONCW_MODE"
	concwDSN         = "SENTINELOPS_CONCW_DSN"
	concwOwner       = "SENTINELOPS_CONCW_OWNER"
	concwHash        = "SENTINELOPS_CONCW_HASH"
	concwLeaseMS     = "SENTINELOPS_CONCW_LEASE_MS"
	concwWorkMS      = "SENTINELOPS_CONCW_WORK_MS"
	concwIdleLimit   = "SENTINELOPS_CONCW_IDLE_LIMIT"
	concwExecLog     = "SENTINELOPS_CONCW_EXEC_LOG"
	concwEvidenceDir = "SENTINELOPS_CONCW_EVIDENCE_DIR"
	concwCaseFilter  = "SENTINELOPS_CONCW_CASES"

	concwModeBurst = "burst"
)

// concwCaseConfig 描述一组 K Worker × M Run 实验。
type concwCaseConfig struct {
	Name         string          `json:"name"`
	DBSuffix     string          `json:"db_suffix"`
	Workers      int             `json:"workers"`
	Runs         int             `json:"runs"`
	WorkMS       int             `json:"work_ms"`
	LeaseMS      int             `json:"lease_ms"`
	IdleLimit    int             `json:"idle_limit"`
	Distribution concwSpreadSpec `json:"distribution"`
}

// concwSpreadSpec 是「非 runnable / 其他状态」行的实验数据分布，只用于让查询计划
// 的选择率接近真实队列，不参与本轮 Run 的断言集合。
type concwSpreadSpec struct {
	PendingFuture     int `json:"pending_future"`
	RetryableNotDue   int `json:"retryable_not_due"`
	RunningValidlease int `json:"running_valid_lease"`
	TerminalSucceeded int `json:"terminal_succeeded"`
	AttemptsExhausted int `json:"attempts_exhausted"`
}

func concwCaseConfigs() []concwCaseConfig {
	return []concwCaseConfig{
		{
			Name: "case-a-4x100", DBSuffix: "concw_case_a", Workers: 4, Runs: 100,
			WorkMS: 60, LeaseMS: 30_000, IdleLimit: 40,
		},
		{
			Name: "case-b-8x300", DBSuffix: "concw_case_b", Workers: 8, Runs: 300,
			WorkMS: 60, LeaseMS: 30_000, IdleLimit: 40,
			Distribution: concwSpreadSpec{
				PendingFuture: 25, RetryableNotDue: 20, RunningValidlease: 15,
				TerminalSucceeded: 25, AttemptsExhausted: 15,
			},
		},
	}
}

type concwExecRecord struct {
	RunID      string `json:"run_id"`
	Owner      string `json:"owner"`
	Generation uint64 `json:"generation"`
	Attempt    uint   `json:"attempt"`
	PID        int    `json:"pid"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Error      string `json:"error,omitempty"`
}

type concwAttemptFact struct {
	Attempt         uint    `json:"attempt"`
	WorkerID        string  `json:"worker_id"`
	Status          string  `json:"status"`
	FailureCode     string  `json:"failure_code"`
	LeaseGeneration *uint64 `json:"lease_generation"`
	StartedAt       *string `json:"started_at"`
	FinishedAt      *string `json:"finished_at"`
	OpenProjection  bool    `json:"open_projection"`
}

type concwClaimEventFact struct {
	Case            string  `json:"case"`
	RunID           string  `json:"run_id"`
	Seq             uint64  `json:"seq"`
	Owner           string  `json:"owner"`
	LeaseGeneration float64 `json:"lease_generation"`
	Attempt         float64 `json:"attempt"`
	RuntimeVersion  string  `json:"runtime_version"`
	Compatibility   string  `json:"runtime_compatibility_hash"`
}

type concwRunFact struct {
	Case                  string             `json:"case"`
	RunID                 string             `json:"run_id"`
	Status                string             `json:"status"`
	Attempt               uint               `json:"attempt"`
	MaxAttempts           uint               `json:"max_attempts"`
	LeaseGeneration       uint64             `json:"lease_generation"`
	LeaseOwner            string             `json:"lease_owner"`
	LeaseUntil            *string            `json:"lease_until"`
	HeartbeatAt           *string            `json:"heartbeat_at"`
	FinishedAt            *string            `json:"finished_at"`
	LastEventSeq          uint64             `json:"last_event_seq"`
	EventCount            int                `json:"event_count"`
	EventSeqContiguous    bool               `json:"event_seq_contiguous"`
	ClaimedEventCount     int                `json:"claimed_event_count"`
	OutputPayload         string             `json:"output_payload"`
	Attempts              []concwAttemptFact `json:"attempts"`
	Executions            []concwExecRecord  `json:"executions"`
	ExecutingWorkers      []string           `json:"executing_workers"`
	OverlappingExecutions int                `json:"overlapping_executions"`
}

type concwAssertion struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type concwExplainTable struct {
	TableName           string   `json:"table_name"`
	AccessType          string   `json:"access_type"`
	Key                 string   `json:"key"`
	PossibleKeys        []string `json:"possible_keys"`
	RowsExaminedPerScan *float64 `json:"rows_examined_per_scan"`
	RowsProducedPerJoin *float64 `json:"rows_produced_per_join"`
	Filtered            *float64 `json:"filtered_percent"`
	UsingFilesort       bool     `json:"using_filesort"`
	UsingTemporaryTable bool     `json:"using_temporary_table"`
	IndexCondition      string   `json:"index_condition"`
	AttachedCondition   string   `json:"attached_condition"`
	QueryBlockPath      string   `json:"query_block_path"`
}

type concwExplainEvidence struct {
	Case             string              `json:"case"`
	Label            string              `json:"label"`
	CapturedAt       string              `json:"captured_at"`
	DatabaseName     string              `json:"database_name"`
	Statement        string              `json:"statement"`
	TableRowCount    int64               `json:"workflow_runs_row_count"`
	DurableRowCount  int64               `json:"durable_v1_row_count"`
	StatusHistogram  map[string]int64    `json:"status_histogram_durable_v1"`
	AccessPathCounts map[string]int      `json:"access_type_counts"`
	ChosenKeys       map[string]string   `json:"chosen_keys_by_access"`
	UsingFilesort    bool                `json:"using_filesort"`
	FilesortPaths    []string            `json:"filesort_paths,omitempty"`
	SortCosts        []string            `json:"sort_cost_info,omitempty"`
	QueryCost        string              `json:"query_cost,omitempty"`
	Indexes          []string            `json:"workflow_runs_indexes"`
	Tables           []concwExplainTable `json:"tables"`
	RawPlanJSON      string              `json:"raw_plan_json"`
	AnalyzePlanText  string              `json:"explain_analyze_text,omitempty"`
	AnalyzeError     string              `json:"explain_analyze_error,omitempty"`
}

type concwSummary struct {
	Case                   string            `json:"case"`
	DatabaseName           string            `json:"database_name"`
	CompatibilityHash      string            `json:"runtime_compatibility_hash"`
	Workers                int               `json:"workers"`
	Runs                   int               `json:"runs"`
	WorkMS                 int               `json:"work_ms_per_run"`
	LeaseMS                int               `json:"lease_ms"`
	IdleLimit              int               `json:"idle_limit_consecutive_empty"`
	StartedAt              string            `json:"started_at"`
	FinishedAt             string            `json:"finished_at"`
	DurationMS             int64             `json:"duration_ms"`
	SeedDurationMS         int64             `json:"seed_duration_ms"`
	RunDurationMS          int64             `json:"worker_run_duration_ms"`
	DistributionSeeded     map[string]int    `json:"distribution_seeded"`
	DistributionStatuses   map[string]int64  `json:"distribution_final_status"`
	ChildExitCodes         map[string]int    `json:"child_exit_codes"`
	ChildStderr            map[string]string `json:"child_stderr_tail,omitempty"`
	ChildFailures          map[string]string `json:"child_failure_first_error,omitempty"`
	DeadlockObservations   int               `json:"deadlock_error_1213_observations"`
	ClaimRetryableErrors   int               `json:"claim_retryable_error_observations"`
	ProductionClaimRetry   int               `json:"production_claim_retry_count"`
	ClaimRetryReasons      map[string]int    `json:"claim_retry_reasons,omitempty"`
	PerWorkerExecutions    map[string]int    `json:"per_worker_executions"`
	WorkersWithExecutions  int               `json:"workers_with_executions"`
	TotalExecutions        int               `json:"total_executions"`
	DuplicateValidClaims   int               `json:"duplicate_valid_claims"`
	DuplicateExecutions    int               `json:"duplicate_executions"`
	ConcurrentSameRunExec  int               `json:"concurrent_same_run_executions"`
	NonTerminalRuns        []string          `json:"non_terminal_runs"`
	RunsNotSucceeded       []string          `json:"runs_not_succeeded"`
	RunsWithMultiClaim     []string          `json:"runs_with_multiple_claimed_events"`
	RunsWithMultiAttempt   []string          `json:"runs_with_multiple_attempts"`
	RunsWithMultiExec      []string          `json:"runs_with_multiple_executions"`
	OpenAttemptsAfterRun   int               `json:"open_attempts_after_run"`
	RunsClaimOwnerMismatch []string          `json:"runs_claim_owner_mismatch"`
	RunsExecOwnerMismatch  []string          `json:"runs_exec_owner_mismatch"`
	Assertions             []concwAssertion  `json:"assertions"`
	AllAssertionsPassed    bool              `json:"all_assertions_passed"`
}

// TestWorkerConcurrentClaimMatrix 是 T1 主实验入口（父进程）。
func TestWorkerConcurrentClaimMatrix(t *testing.T) {
	if os.Getenv(concwChildFlag) != "" {
		t.Skip("T1 concurrency harness runs only in child processes")
	}
	evidenceRoot := strings.TrimSpace(os.Getenv(concwEvidenceDir))
	if evidenceRoot == "" {
		t.Skip("T1 evidence harness: set SENTINELOPS_CONCW_EVIDENCE_DIR to run the concurrency matrix")
	}
	evidenceDir, err := filepath.Abs(evidenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	selected := strings.TrimSpace(os.Getenv(concwCaseFilter))

	var (
		summaries []concwSummary
		runFacts  []concwRunFact
		events    []concwClaimEventFact
	)
	for _, cfg := range concwCaseConfigs() {
		if selected != "" && !concwCaseSelected(selected, cfg) {
			t.Logf("T1 case %s not selected by %s=%q", cfg.Name, concwCaseFilter, selected)
			continue
		}
		summary, facts, claimed := concwRunCase(t, evidenceDir, cfg)
		summaries = append(summaries, summary)
		runFacts = append(runFacts, facts...)
		events = append(events, claimed...)
	}
	if len(summaries) == 0 {
		t.Fatal("no T1 case selected")
	}

	concwWriteJSON(t, filepath.Join(evidenceDir, "summary.json"), summaries)
	concwWriteJSON(t, filepath.Join(evidenceDir, "runs.json"), runFacts)
	concwWriteJSON(t, filepath.Join(evidenceDir, "claim-events.json"), events)
	t.Logf("T1 evidence written to %s", evidenceDir)

	for _, summary := range summaries {
		if !summary.AllAssertionsPassed {
			t.Errorf("T1 case %s: assertions failed: %s", summary.Case, concwFailedAssertionText(summary.Assertions))
		}
	}
}

func concwCaseSelected(selection string, cfg concwCaseConfig) bool {
	for _, token := range strings.Split(selection, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if token == cfg.Name || token == cfg.DBSuffix || strings.HasPrefix(cfg.Name, token+"-") {
			return true
		}
	}
	return false
}

func concwRunCase(t *testing.T, evidenceDir string, cfg concwCaseConfig) (concwSummary, []concwRunFact, []concwClaimEventFact) {
	t.Helper()
	summary := concwSummary{
		Case: cfg.Name, Workers: cfg.Workers, Runs: cfg.Runs, WorkMS: cfg.WorkMS,
		LeaseMS: cfg.LeaseMS, IdleLimit: cfg.IdleLimit,
		StartedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		ChildExitCodes: map[string]int{}, ChildStderr: map[string]string{},
		ChildFailures:       map[string]string{},
		PerWorkerExecutions: map[string]int{}, DistributionSeeded: map[string]int{},
	}
	started := time.Now()
	db := killexNewRuntimeDatabase(t, cfg.DBSuffix)
	if err := db.Raw("SELECT DATABASE()").Scan(&summary.DatabaseName).Error; err != nil {
		t.Fatalf("read experiment database name: %v", err)
	}
	store := workflow.NewGORMStore(db)

	frozen, err := FreezeRuntimeSnapshot(fixture14SnapshotInput(t))
	if err != nil {
		t.Fatal(err)
	}
	hash := frozen.CompatibilityHash()
	summary.CompatibilityHash = hash

	// 阶段 1：在空库上跑一次生产认领路径，从 MySQL general_log 抓取真实 Claim SQL 原文。
	capture := concwCaptureClaimStatement(t, db, store, hash)

	// 阶段 2：写入 M 个可执行 Run（+ 可选的非 runnable 分布行）并采集 EXPLAIN。
	seedStart := time.Now()
	runIDs := concwSeedRuns(t, store, frozen, cfg.Name, cfg.Runs)
	spreadIDs := concwSeedSpread(t, store, frozen, cfg, summary.DistributionSeeded)
	summary.SeedDurationMS = time.Since(seedStart).Milliseconds()
	t.Logf("T1 %s: seeded %d runs (%d spread rows) in %dms", cfg.Name, len(runIDs), len(spreadIDs), summary.SeedDurationMS)

	explain := concwExplainClaimPlan(t, db, cfg.Name, capture.Statement, summary.DatabaseName, true)
	concwWriteJSON(t, filepath.Join(evidenceDir, fmt.Sprintf("explain-%s.json", cfg.Name)), explain)
	if cfg.DBSuffix == "concw_case_b" {
		concwWriteJSON(t, filepath.Join(evidenceDir, "explain-representative.json"), explain)
	}

	// 阶段 3：K 个真实 Worker 子进程并发执行。
	execDir := filepath.Join(evidenceDir, "exec-logs", cfg.Name)
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		t.Fatal(err)
	}
	childDSN := killexChildDSN(t, db)
	runStart := time.Now()
	liveCapture := concwStartLiveClaimCapture(t, db)
	children := make([]*exec.Cmd, 0, cfg.Workers)
	buffers := make([]*concwChildBuffer, 0, cfg.Workers)
	execLogs := make([]string, 0, cfg.Workers)
	for index := 0; index < cfg.Workers; index++ {
		owner := fmt.Sprintf("concw-%s-w%d", cfg.Name, index+1)
		execLog := filepath.Join(execDir, owner+".jsonl")
		execLogs = append(execLogs, execLog)
		buffer := &concwChildBuffer{}
		buffers = append(buffers, buffer)
		child := concwStartChild(t, buffer, map[string]string{
			concwMode:      concwModeBurst,
			concwDSN:       childDSN,
			concwOwner:     owner,
			concwHash:      hash,
			concwLeaseMS:   strconv.Itoa(cfg.LeaseMS),
			concwWorkMS:    strconv.Itoa(cfg.WorkMS),
			concwIdleLimit: strconv.Itoa(cfg.IdleLimit),
			concwExecLog:   execLog,
		})
		children = append(children, child)
	}
	exitCodes := concwWaitChildren(t, children, buffers, 6*time.Minute)
	var childLog strings.Builder
	for index, code := range exitCodes {
		owner := fmt.Sprintf("concw-%s-w%d", cfg.Name, index+1)
		summary.ChildExitCodes[owner] = code
		full := buffers[index].text()
		childLog.WriteString("=== " + owner + " exit=" + strconv.Itoa(code) + " ===\n")
		childLog.WriteString(full)
		childLog.WriteString("\n")
		summary.DeadlockObservations += concwDeadlockObservationCount(full)
		summary.ClaimRetryableErrors += concwClaimRetryableErrorCount(full)
		summary.ProductionClaimRetry += concwChildProductionClaimRetryCount(full)
		if summary.ClaimRetryReasons == nil {
			summary.ClaimRetryReasons = map[string]int{}
		}
		for reason, count := range concwClaimRetryReasonCounts(full) {
			summary.ClaimRetryReasons[reason] += count
		}
		if failure := concwFirstFailureLine(full); failure != "" {
			summary.ChildFailures[owner] = failure
		}
		if code != 0 {
			summary.ChildStderr[owner] = buffers[index].tail()
		}
	}
	if err := os.WriteFile(filepath.Join(evidenceDir, fmt.Sprintf("child-logs-%s.log", cfg.Name)), []byte(childLog.String()), 0o644); err != nil {
		t.Fatalf("write child logs: %v", err)
	}
	summary.RunDurationMS = time.Since(runStart).Milliseconds()
	liveCapture.finish(t, db, capture.Statement)
	concwWriteJSON(t, filepath.Join(evidenceDir, fmt.Sprintf("claim-sql-capture-%s.json", cfg.Name)),
		map[string]any{"probe": capture, "live_run": liveCapture})

	// 阶段 4：数据库真值 + 子进程执行日志汇总。
	execRecords := concwReadExecRecords(t, execLogs)
	facts, events := concwCollectRunFacts(t, db, cfg.Name, runIDs, execRecords)
	summary.DistributionStatuses = concwStatusHistogram(t, db, spreadIDs)
	summary.PerWorkerExecutions, summary.TotalExecutions = concwPerWorkerExecutions(execRecords)
	summary.WorkersWithExecutions = len(summary.PerWorkerExecutions)
	concwApplyAssertions(&summary, cfg, facts, events)
	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	summary.DurationMS = time.Since(started).Milliseconds()

	for _, assertion := range summary.Assertions {
		t.Logf("T1 %s assertion %-32s passed=%v (%s)", cfg.Name, assertion.Name, assertion.Passed, assertion.Detail)
	}
	return summary, facts, events
}

func concwApplyAssertions(summary *concwSummary, cfg concwCaseConfig, facts []concwRunFact, events []concwClaimEventFact) {
	var (
		nonTerminal        []string
		notSucceeded       []string
		multiClaim         []string
		multiAttempts      []string
		multiExec          []string
		ownerClaimMismatch []string
		ownerExecMismatch  []string
		openAttempts       int
		duplicateExecs     int
		overlaps           int
	)
	for _, fact := range facts {
		switch fact.Status {
		case workflow.RunStatusSucceeded, workflow.RunStatusFailed, workflow.RunStatusCanceled:
		default:
			nonTerminal = append(nonTerminal, fact.RunID)
		}
		if fact.Status != workflow.RunStatusSucceeded {
			notSucceeded = append(notSucceeded, fact.RunID)
		}
		if len(fact.Attempts) != 1 {
			multiAttempts = append(multiAttempts, fmt.Sprintf("%s(attempts=%d)", fact.RunID, len(fact.Attempts)))
			if len(fact.Attempts) > 1 {
				multiExec = append(multiExec, fmt.Sprintf("%s(attempts=%d)", fact.RunID, len(fact.Attempts)))
			}
		}
		if fact.Attempt != 1 {
			multiExec = append(multiExec, fmt.Sprintf("%s(attempt=%d)", fact.RunID, fact.Attempt))
		}
		if fact.ClaimedEventCount != 1 {
			multiClaim = append(multiClaim, fmt.Sprintf("%s(claimed_events=%d)", fact.RunID, fact.ClaimedEventCount))
		}
		if len(fact.Executions) != 1 {
			duplicateExecs += 1
		}
		if fact.OverlappingExecutions > 0 {
			overlaps += fact.OverlappingExecutions
		}
		if len(fact.Attempts) == 1 {
			attempt := fact.Attempts[0]
			if attempt.OpenProjection {
				openAttempts++
			}
			claimOwner := ""
			for _, event := range events {
				if event.RunID == fact.RunID {
					claimOwner = event.Owner
				}
			}
			if claimOwner != "" && attempt.WorkerID != "" && attempt.WorkerID != claimOwner {
				ownerClaimMismatch = append(ownerClaimMismatch, fmt.Sprintf("%s(claim=%s attempt=%s)", fact.RunID, claimOwner, attempt.WorkerID))
			}
			if len(fact.Executions) == 1 && claimOwner != "" && fact.Executions[0].Owner != claimOwner {
				ownerExecMismatch = append(ownerExecMismatch, fmt.Sprintf("%s(claim=%s exec=%s)", fact.RunID, claimOwner, fact.Executions[0].Owner))
			}
		}
	}
	sort.Strings(nonTerminal)
	sort.Strings(notSucceeded)
	sort.Strings(multiClaim)
	sort.Strings(multiAttempts)
	sort.Strings(multiExec)
	sort.Strings(ownerClaimMismatch)
	sort.Strings(ownerExecMismatch)
	summary.NonTerminalRuns = nonTerminal
	summary.RunsNotSucceeded = notSucceeded
	summary.RunsWithMultiClaim = multiClaim
	summary.RunsWithMultiAttempt = multiAttempts
	summary.RunsWithMultiExec = multiExec
	summary.DuplicateValidClaims = len(multiClaim)
	summary.DuplicateExecutions = duplicateExecs
	summary.ConcurrentSameRunExec = overlaps
	summary.OpenAttemptsAfterRun = openAttempts
	summary.RunsClaimOwnerMismatch = ownerClaimMismatch
	summary.RunsExecOwnerMismatch = ownerExecMismatch

	add := func(name string, passed bool, detail string) {
		summary.Assertions = append(summary.Assertions, concwAssertion{Name: name, Passed: passed, Detail: detail})
	}
	add("all_runs_terminal", len(nonTerminal) == 0, fmt.Sprintf("non_terminal=%d", len(nonTerminal)))
	add("all_runs_succeeded", len(notSucceeded) == 0, fmt.Sprintf("not_succeeded=%d", len(notSucceeded)))
	add("single_valid_claim_per_run", len(multiClaim) == 0, fmt.Sprintf("runs_with_claim_count_ne_1=%d", len(multiClaim)))
	add("single_attempt_per_run", len(multiAttempts) == 0, fmt.Sprintf("runs_with_attempt_rows_ne_1=%d", len(multiAttempts)))
	add("single_execution_per_run", duplicateExecs == 0, fmt.Sprintf("runs_with_exec_records_ne_1=%d", duplicateExecs))
	add("no_concurrent_same_run_execution", overlaps == 0, fmt.Sprintf("overlaps=%d", overlaps))
	add("no_open_attempt_after_run", openAttempts == 0, fmt.Sprintf("open_attempts=%d", openAttempts))
	add("claim_owner_matches_attempt_worker", len(ownerClaimMismatch) == 0, fmt.Sprintf("mismatch=%d", len(ownerClaimMismatch)))
	add("claim_owner_matches_execution_owner", len(ownerExecMismatch) == 0, fmt.Sprintf("mismatch=%d", len(ownerExecMismatch)))
	add("all_workers_participated", summary.WorkersWithExecutions == cfg.Workers,
		fmt.Sprintf("workers_with_executions=%d/%d", summary.WorkersWithExecutions, cfg.Workers))
	fatalExits := 0
	for _, code := range summary.ChildExitCodes {
		if code != 0 {
			fatalExits++
		}
	}
	add("no_worker_fatal_exit", fatalExits == 0, fmt.Sprintf("fatal_worker_exits=%d", fatalExits))
	add("claim_retry_accounting",
		summary.ProductionClaimRetry == summary.ClaimRetryableErrors,
		fmt.Sprintf("production_claim_retries=%d retryable_claim_errors=%d", summary.ProductionClaimRetry, summary.ClaimRetryableErrors))
	summary.AllAssertionsPassed = true
	for _, assertion := range summary.Assertions {
		if !assertion.Passed {
			summary.AllAssertionsPassed = false
		}
	}
}

func concwFailedAssertionText(assertions []concwAssertion) string {
	var failed []string
	for _, assertion := range assertions {
		if !assertion.Passed {
			failed = append(failed, assertion.Name)
		}
	}
	return strings.Join(failed, ",")
}

// concwFirstFailureLine 提取子进程输出中第一条 Worker.Run 失败原因，便于在
// 汇总证据里直接看到 worker 退出的真实错误分类。
func concwFirstFailureLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "Worker.Run:") {
			continue
		}
		if index := strings.Index(trimmed, "Worker.Run:"); index >= 0 {
			return strings.TrimSpace(trimmed[index:])
		}
	}
	return ""
}

// concwDeadlockObservationCount 统计子进程日志里 MySQL 1213 出现次数。
// 修复后它对应「数据库确实报告了死锁」，不再对应 Worker 退出。
func concwDeadlockObservationCount(text string) int {
	return strings.Count(text, "Error 1213")
}

// concwClaimRetryableErrorCount 统计子进程经生产 Claim 原语返回的可重试事务冲突。
func concwClaimRetryableErrorCount(text string) int {
	return strings.Count(text, "harness observed retryable claim error:")
}

// concwClaimRetryReasonCounts 解析子进程记录的稳定重试原因标签。
func concwClaimRetryReasonCounts(text string) map[string]int {
	counts := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		index := strings.Index(line, "reason=")
		if index < 0 || !strings.Contains(line, "harness observed retryable claim error:") {
			continue
		}
		reason := strings.TrimSpace(line[index+len("reason="):])
		if stop := strings.IndexAny(reason, " \t"); stop >= 0 {
			reason = reason[:stop]
		}
		if reason != "" {
			counts[reason]++
		}
	}
	return counts
}

// concwChildProductionClaimRetryCount 解析子进程退出行里的生产侧累计 Claim 重试次数
// （来自 Worker.ClaimRetryStats，与业务 workflow attempt 无关）。
func concwChildProductionClaimRetryCount(text string) int {
	return concwChildIntField(text, "production_claim_retries=")
}

func concwChildIntField(text, field string) int {
	for _, line := range strings.Split(text, "\n") {
		index := strings.Index(line, field)
		if index < 0 {
			continue
		}
		value := strings.TrimSpace(line[index+len(field):])
		if stop := strings.IndexAny(value, " \t"); stop >= 0 {
			value = value[:stop]
		}
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return 0
}

// concwCollectRunFacts 从 workflow_runs / workflow_attempts / workflow_events 读取真值，
// 并与子进程执行日志交叉核对。
func concwCollectRunFacts(t *testing.T, db *gorm.DB, caseName string, runIDs []string, execRecords map[string][]concwExecRecord) ([]concwRunFact, []concwClaimEventFact) {
	t.Helper()
	facts := make([]concwRunFact, 0, len(runIDs))
	claimFacts := make([]concwClaimEventFact, 0, len(runIDs))
	for _, runID := range runIDs {
		var run mysql.WorkflowRun
		if err := db.First(&run, "id = ?", runID).Error; err != nil {
			t.Fatalf("read run %s: %v", runID, err)
		}
		fact := concwRunFact{
			Case: caseName, RunID: runID, Status: run.Status, Attempt: run.Attempt,
			MaxAttempts: run.MaxAttempts, LeaseGeneration: run.LeaseGeneration,
			LeaseOwner: killexValue(run.LeaseOwner), LeaseUntil: concwTimePtr(run.LeaseUntil),
			HeartbeatAt: concwTimePtr(run.HeartbeatAt), FinishedAt: concwTimePtr(run.FinishedAt),
			LastEventSeq: run.LastEventSeq, OutputPayload: run.OutputPayload,
		}
		var attempts []mysql.WorkflowAttempt
		if err := db.Where("run_id = ?", runID).Order("attempt ASC").Find(&attempts).Error; err != nil {
			t.Fatalf("read attempts for %s: %v", runID, err)
		}
		for _, attempt := range attempts {
			row := concwAttemptFact{
				Attempt: attempt.Attempt, WorkerID: killexValue(attempt.WorkerID), Status: killexValue(attempt.Status),
				FailureCode: killexValue(attempt.FailureCode), LeaseGeneration: attempt.LeaseGeneration,
				StartedAt: concwTimePtr(attempt.StartedAt), FinishedAt: concwTimePtr(attempt.FinishedAt),
				OpenProjection: attempt.FinishedAt == nil,
			}
			fact.Attempts = append(fact.Attempts, row)
			if row.WorkerID != "" {
				fact.ExecutingWorkers = append(fact.ExecutingWorkers, row.WorkerID)
			}
		}
		var events []mysql.WorkflowEvent
		if err := db.Where("run_id = ?", runID).Order("seq ASC").Find(&events).Error; err != nil {
			t.Fatalf("read events for %s: %v", runID, err)
		}
		fact.EventCount = len(events)
		fact.EventSeqContiguous = true
		for index, event := range events {
			if event.Seq != uint64(index+1) {
				fact.EventSeqContiguous = false
			}
			if event.EventType != workflow.EventRunClaimed {
				continue
			}
			fact.ClaimedEventCount++
			claimFacts = append(claimFacts, concwClaimEventFact{
				Case: caseName, RunID: runID, Seq: event.Seq,
				Owner:           concwEventAttribute(event.Payload, "owner"),
				LeaseGeneration: concwEventNumber(event.Payload, "lease_generation"),
				Attempt:         concwEventNumber(event.Payload, "attempt"),
				RuntimeVersion:  concwEventAttribute(event.Payload, "runtime_version"),
				Compatibility:   concwEventAttribute(event.Payload, "runtime_compatibility_hash"),
			})
		}
		fact.Executions = execRecords[runID]
		fact.OverlappingExecutions = concwOverlaps(fact.Executions)
		facts = append(facts, fact)
	}
	return facts, claimFacts
}

func concwEventAttribute(payload, key string) string {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return ""
	}
	value, ok := envelope.Data[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func concwEventNumber(payload, key string) float64 {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return 0
	}
	value, ok := envelope.Data[key]
	if !ok {
		return 0
	}
	number, ok := value.(float64)
	if !ok {
		return 0
	}
	return number
}

func concwOverlaps(records []concwExecRecord) int {
	overlaps := 0
	for first := 0; first < len(records); first++ {
		for second := first + 1; second < len(records); second++ {
			firstStart, firstErr := time.Parse(time.RFC3339Nano, records[first].StartedAt)
			firstEnd, secondErr := time.Parse(time.RFC3339Nano, records[first].FinishedAt)
			secondStart, thirdErr := time.Parse(time.RFC3339Nano, records[second].StartedAt)
			secondEnd, fourthErr := time.Parse(time.RFC3339Nano, records[second].FinishedAt)
			if firstErr != nil || secondErr != nil || thirdErr != nil || fourthErr != nil {
				continue
			}
			if firstStart.Before(secondEnd) && secondStart.Before(firstEnd) {
				overlaps++
			}
		}
	}
	return overlaps
}

func concwReadExecRecords(t *testing.T, execLogs []string) map[string][]concwExecRecord {
	t.Helper()
	records := map[string][]concwExecRecord{}
	for _, path := range execLogs {
		payload, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read exec log %s: %v", path, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(payload)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var record concwExecRecord
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("decode exec record %s: %v (%s)", path, err, line)
			}
			if record.RunID == "" {
				continue
			}
			records[record.RunID] = append(records[record.RunID], record)
		}
	}
	for runID := range records {
		sort.Slice(records[runID], func(first, second int) bool {
			return records[runID][first].StartedAt < records[runID][second].StartedAt
		})
	}
	return records
}

func concwPerWorkerExecutions(records map[string][]concwExecRecord) (map[string]int, int) {
	perWorker := map[string]int{}
	total := 0
	for _, list := range records {
		for _, record := range list {
			perWorker[record.Owner]++
			total++
		}
	}
	return perWorker, total
}

func concwStatusHistogram(t *testing.T, db *gorm.DB, runIDs []string) map[string]int64 {
	t.Helper()
	histogram := map[string]int64{}
	if len(runIDs) == 0 {
		return histogram
	}
	var rows []struct {
		Status string
		Count  int64
	}
	if err := db.Raw("SELECT status, COUNT(*) AS count FROM workflow_runs WHERE id IN ? GROUP BY status", runIDs).
		Scan(&rows).Error; err != nil {
		t.Fatalf("status histogram: %v", err)
	}
	for _, row := range rows {
		histogram[row.Status] = row.Count
	}
	return histogram
}

// --- 子进程 ---

func concwStartChild(t *testing.T, buffer *concwChildBuffer, env map[string]string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestWorkerConcurrencyChildHelper$", "-test.v")
	command.Env = append(os.Environ(), concwChildFlag+"=1")
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdout = buffer
	command.Stderr = buffer
	if err := command.Start(); err != nil {
		t.Fatalf("start concurrency child: %v", err)
	}
	return command
}

func concwWaitChildren(t *testing.T, children []*exec.Cmd, buffers []*concwChildBuffer, timeout time.Duration) []int {
	t.Helper()
	type result struct {
		index int
		code  int
		err   error
	}
	done := make(chan result, len(children))
	for index, child := range children {
		go func(index int, child *exec.Cmd) {
			err := child.Wait()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if ok := concwAsExitError(err, &exitErr); ok {
					code = exitErr.ExitCode()
				} else {
					code = -1
				}
			}
			done <- result{index: index, code: code, err: err}
		}(index, child)
	}
	codes := make([]int, len(children))
	deadline := time.After(timeout)
	for received := 0; received < len(children); received++ {
		select {
		case item := <-done:
			codes[item.index] = item.code
			if item.err != nil {
				t.Logf("T1 child %d exit: %v\n%s", item.index, item.err, buffers[item.index].tail())
			}
		case <-deadline:
			for _, child := range children {
				if child.Process != nil {
					_ = child.Process.Kill()
				}
			}
			t.Fatalf("T1 children did not finish within %s", timeout)
		}
	}
	return codes
}

func concwAsExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

type concwChildBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *concwChildBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, payload...)
	return len(payload), nil
}

func (b *concwChildBuffer) tail() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := string(b.data)
	if len(text) > 4000 {
		text = text[len(text)-4000:]
	}
	return text
}

func (b *concwChildBuffer) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// TestWorkerConcurrencyChildHelper 是 K 个 Worker 子进程入口：循环执行真实
// Worker.RunOnce，直到连续 N 次空转后退出（队列已无可认领 Run）。
func TestWorkerConcurrencyChildHelper(t *testing.T) {
	if os.Getenv(concwChildFlag) == "" {
		t.Skip("T1 concurrency helper runs only in child processes")
	}
	if mode := os.Getenv(concwMode); mode != concwModeBurst {
		t.Fatalf("unknown T1 child mode %q", mode)
	}
	db, err := gorm.Open(gormmysql.Open(os.Getenv(concwDSN)), &gorm.Config{})
	if err != nil {
		t.Fatalf("child open MySQL: %v", err)
	}
	store := workflow.NewGORMStore(db)
	owner := os.Getenv(concwOwner)
	leaseMS, err := strconv.Atoi(os.Getenv(concwLeaseMS))
	if err != nil {
		t.Fatalf("child lease ms: %v", err)
	}
	workMS, err := strconv.Atoi(os.Getenv(concwWorkMS))
	if err != nil || workMS <= 0 {
		t.Fatalf("child work ms: %v", err)
	}
	idleLimit, err := strconv.Atoi(os.Getenv(concwIdleLimit))
	if err != nil || idleLimit <= 0 {
		t.Fatalf("child idle limit: %v", err)
	}
	execLog := os.Getenv(concwExecLog)
	if execLog == "" {
		t.Fatal("child exec log path is required")
	}
	// 每次实验轮次必须从空文件开始，否则上一轮的追加记录会污染执行次数断言。
	if err := os.WriteFile(execLog, nil, 0o600); err != nil {
		t.Fatalf("truncate exec log: %v", err)
	}
	// 认领原语仍然是生产 GORMStore.ClaimNextRun，只在现有 WorkerConfig 注入点上
	// 记录 idle / 可重试事务冲突观测，并让子进程沿用原来的 idle 退出语义；
	// Worker 生命周期跑的仍是生产 Worker.Run（含 retryable claim 重试与退避）。
	leaseDuration := time.Duration(leaseMS) * time.Millisecond
	fingerprint := os.Getenv(concwHash)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	empty := 0
	var claims atomic.Int64
	var retryableClaimErrors atomic.Int64
	worker, err := NewWorker(store, WorkerConfig{
		Owner: owner, LeaseDuration: leaseDuration,
		MinPollBackoff: 20 * time.Millisecond, MaxPollBackoff: 200 * time.Millisecond,
		Observation: WorkerObservation{WorkerID: owner, RuntimeCompatibilityHash: fingerprint},
		ClaimNext: func(ctx context.Context) (*workflow.ClaimedRun, bool, error) {
			claimed, ok, claimErr := store.ClaimNextRun(ctx, workflow.ClaimInput{
				Owner: owner, LeaseDuration: leaseDuration, ExecutingWorkerFingerprint: fingerprint,
			})
			switch {
			case claimErr != nil && errors.Is(claimErr, workflow.ErrClaimRetryableTransaction):
				retryableClaimErrors.Add(1)
				// 生产侧会把它写成 Worker Health last_error 并做有界退避重试；
				// 这里额外打印一行，便于父进程在不连库的情况下统计原始冲突。
				t.Logf("child %s harness observed retryable claim error: reason=%s", owner, workflow.ClaimRetryReason(claimErr))
				return nil, false, claimErr
			case claimErr != nil:
				return nil, false, claimErr
			case ok:
				claims.Add(1)
				empty = 0
			default:
				empty++
				if empty >= idleLimit {
					cancel()
				}
			}
			return claimed, ok, nil
		},
		Execute: func(ctx context.Context, claimed *workflow.ClaimedRun) (RunExecutionResult, error) {
			started := time.Now().UTC()
			time.Sleep(time.Duration(workMS) * time.Millisecond)
			finished := time.Now().UTC()
			record := concwExecRecord{
				RunID: claimed.Run.ID, Owner: owner, Generation: claimed.Token.Generation,
				Attempt: claimed.Run.Attempt, PID: os.Getpid(),
				StartedAt: started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
			}
			if err := concwAppendJSONLine(execLog, record); err != nil {
				return RunExecutionResult{}, err
			}
			return RunExecutionResult{
				OutputPayload:     fmt.Sprintf(`{"harness":"t1-worker-concurrency","worker":%q,"run_id":%q}`, owner, claimed.Run.ID),
				RevisionStateJSON: []byte(fmt.Sprintf(`{"session_revision":1,"harness":"t1","worker":%q}`, owner)),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("child build Worker: %v", err)
	}
	runErr := worker.Run(ctx)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		_ = concwAppendJSONLine(execLog, concwExecRecord{Owner: owner, PID: os.Getpid(), Error: runErr.Error()})
		t.Fatalf("child %s Worker.Run: %v", owner, runErr)
	}
	stats := worker.ClaimRetryStats()
	t.Logf("child %s exiting: claims=%d consecutive_empty=%d retryable_claim_errors=%d production_claim_retries=%d last_reason=%s last_backoff=%s",
		owner, claims.Load(), empty, retryableClaimErrors.Load(), stats.Count, stats.LastReason, stats.LastBackoff)
}

func concwAppendJSONLine(path string, record concwExecRecord) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

// --- 数据构造 ---

func concwSeedRuns(t *testing.T, store *workflow.GORMStore, frozen FrozenRuntimeSnapshot, caseName string, count int) []string {
	t.Helper()
	runIDs := make([]string, 0, count)
	for index := 0; index < count; index++ {
		runID := fmt.Sprintf("run-%s-%04d", caseName, index)
		concwCreateRun(t, store, frozen, caseName, runID)
		runIDs = append(runIDs, runID)
	}
	return runIDs
}

func concwSeedSpread(t *testing.T, store *workflow.GORMStore, frozen FrozenRuntimeSnapshot, cfg concwCaseConfig, seeded map[string]int) []string {
	t.Helper()
	spread := cfg.Distribution
	type bucket struct {
		name  string
		count int
		apply string
	}
	buckets := []bucket{
		{"pending_future", spread.PendingFuture, "UPDATE workflow_runs SET available_at = DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 2 HOUR) WHERE id = ?"},
		{"retryable_not_due", spread.RetryableNotDue, "UPDATE workflow_runs SET status = 'retryable_failed', available_at = DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 2 HOUR) WHERE id = ?"},
		{"running_valid_lease", spread.RunningValidlease, "UPDATE workflow_runs SET status = 'running', lease_owner = 'fixture-lease-holder', lease_until = DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 1 HOUR), heartbeat_at = CURRENT_TIMESTAMP(3), lease_generation = 1, attempt = 1 WHERE id = ?"},
		{"terminal_succeeded", spread.TerminalSucceeded, "UPDATE workflow_runs SET status = 'succeeded', finished_at = CURRENT_TIMESTAMP(3), attempt = 1, lease_generation = 1 WHERE id = ?"},
		{"attempts_exhausted", spread.AttemptsExhausted, "UPDATE workflow_runs SET attempt = max_attempts WHERE id = ?"},
	}
	var spreadIDs []string
	for _, item := range buckets {
		if item.count == 0 {
			continue
		}
		seeded[item.name] = item.count
		for index := 0; index < item.count; index++ {
			runID := fmt.Sprintf("spread-%s-%s-%03d", cfg.Name, item.name, index)
			concwCreateRun(t, store, frozen, cfg.Name, runID)
			if err := store.DB().Exec(item.apply, runID).Error; err != nil {
				t.Fatalf("apply spread bucket %s: %v", item.name, err)
			}
			spreadIDs = append(spreadIDs, runID)
		}
	}
	return spreadIDs
}

func concwCreateRun(t *testing.T, store *workflow.GORMStore, frozen FrozenRuntimeSnapshot, caseName, runID string) {
	t.Helper()
	userID := "operator-" + caseName
	identity := policy.Identity{UserID: userID, Role: policy.RoleOperator, Scope: policy.Scope{UserID: userID}}
	if _, err := store.CreateRunWithSessionLock(policy.WithIdentity(context.Background(), identity), workflow.CreateRunInput{
		ID: runID, WorkflowKey: "t1-worker-concurrency", SessionID: "session-" + runID,
		QueryText: "T1 worker concurrency harness", ImmutableInputJSON: json.RawMessage(`{"agent":"test","query":"t1-worker-concurrency"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: json.RawMessage(`{}`),
		DeadlineAt: time.Now().Add(2 * time.Hour), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	}); err != nil {
		t.Fatalf("create run %s: %v", runID, err)
	}
}

// --- Claim SQL 抓取 + EXPLAIN ---

type concwClaimCapture struct {
	CapturedAt        string   `json:"captured_at"`
	GeneralLogEnabled bool     `json:"general_log_enabled"`
	LogOutputPrevious string   `json:"log_output_previous"`
	Statements        []string `json:"captured_statements"`
	Statement         string   `json:"production_claim_sql"`
	DSNInterpolate    bool     `json:"dsn_interpolate_params"`
	Note              string   `json:"note"`
}

type concwLiveCapture struct {
	Enabled          bool   `json:"general_log_enabled"`
	ClaimedStatement string `json:"live_run_claim_statement"`
	MatchesProbe     bool   `json:"live_run_statement_matches_probe"`
	Note             string `json:"note"`
}

func concwCaptureClaimStatement(t *testing.T, db *gorm.DB, store *workflow.GORMStore, hash string) concwClaimCapture {
	t.Helper()
	capture := concwClaimCapture{
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Note:       "在空库上通过生产 Worker.ClaimNext 触发一次真实 Claim SQL，从 MySQL general_log 抓取服务端实际收到的语句原文",
	}
	var previous struct {
		GeneralLog string `gorm:"column:general_log"`
		LogOutput  string `gorm:"column:log_output"`
	}
	if err := db.Raw("SELECT @@global.general_log AS general_log, @@global.log_output AS log_output").Scan(&previous).Error; err != nil {
		t.Fatalf("read general log settings: %v", err)
	}
	capture.LogOutputPrevious = previous.LogOutput
	capture.DSNInterpolate = strings.Contains(os.Getenv("SENTINELOPS_TEST_DSN"), "interpolateParams=true")
	restore := func() {
		_ = db.Exec("SET GLOBAL general_log = 'OFF'").Error
		_ = db.Exec("SET GLOBAL log_output = '" + previous.LogOutput + "'").Error
	}
	if err := db.Exec("SET GLOBAL log_output = 'TABLE'").Error; err != nil {
		t.Fatalf("enable general log table: %v", err)
	}
	if err := db.Exec("SET GLOBAL general_log = 'ON'").Error; err != nil {
		t.Fatalf("enable general log: %v", err)
	}
	capture.GeneralLogEnabled = true
	t.Cleanup(restore)
	if err := db.Exec("TRUNCATE TABLE mysql.general_log").Error; err != nil {
		t.Fatalf("truncate general log: %v", err)
	}
	probe, err := NewWorker(store, WorkerConfig{
		Owner: "concw-sql-capture", LeaseDuration: 30 * time.Second,
		MinPollBackoff: 20 * time.Millisecond, MaxPollBackoff: 200 * time.Millisecond,
		Observation: WorkerObservation{WorkerID: "concw-sql-capture", RuntimeCompatibilityHash: hash},
	})
	if err != nil {
		t.Fatalf("build claim SQL probe worker: %v", err)
	}
	claimed, ok, err := probe.ClaimNext(context.Background())
	if err != nil {
		t.Fatalf("claim SQL probe: %v", err)
	}
	if claimed != nil || ok {
		t.Fatalf("claim SQL probe unexpectedly claimed a run on the empty experiment database: %#v", claimed)
	}
	var statements []string
	if err := db.Raw(`SELECT argument FROM mysql.general_log
		WHERE command_type = 'Query'
		  AND argument LIKE '%workflow_runs%'
		  AND argument LIKE '%SKIP LOCKED%'
		  AND argument NOT LIKE '%general_log%'
		ORDER BY event_time ASC`).Scan(&statements).Error; err != nil {
		t.Fatalf("read captured claim SQL: %v", err)
	}
	capture.Statements = statements
	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") && strings.Contains(strings.ToUpper(trimmed), "FOR UPDATE") {
			capture.Statement = trimmed
		}
	}
	if capture.Statement == "" {
		t.Fatalf("no production claim SELECT captured; statements=%v", statements)
	}
	restore()
	_ = db.Exec("TRUNCATE TABLE mysql.general_log").Error
	return capture
}

func concwStartLiveClaimCapture(t *testing.T, db *gorm.DB) *concwLiveCapture {
	t.Helper()
	live := &concwLiveCapture{Enabled: true, Note: "正式 K×M 运行期间再次打开 general_log，校验子进程实际发出的 Claim SQL 与探针抓取一致"}
	if err := db.Exec("SET GLOBAL log_output = 'TABLE'").Error; err != nil {
		live.Enabled = false
		live.Note = "unable to enable general log for live capture: " + err.Error()
		return live
	}
	if err := db.Exec("SET GLOBAL general_log = 'ON'").Error; err != nil {
		live.Enabled = false
		live.Note = "unable to enable general log for live capture: " + err.Error()
		return live
	}
	if err := db.Exec("TRUNCATE TABLE mysql.general_log").Error; err != nil {
		live.Enabled = false
		live.Note = "unable to truncate general log for live capture: " + err.Error()
		return live
	}
	t.Cleanup(func() {
		_ = db.Exec("SET GLOBAL general_log = 'OFF'").Error
	})
	return live
}

func (live *concwLiveCapture) finish(t *testing.T, db *gorm.DB, probeStatement string) {
	t.Helper()
	if live == nil || !live.Enabled {
		return
	}
	var claimedCount int64
	if err := db.Model(&mysql.WorkflowEvent{}).Where("event_type = ?", workflow.EventRunClaimed).Count(&claimedCount).Error; err != nil {
		live.Note = "unable to count live claim events: " + err.Error()
	}
	var statements []string
	if err := db.Raw(`SELECT argument FROM mysql.general_log
		WHERE command_type = 'Query'
		  AND argument LIKE '%workflow_runs%'
		  AND argument LIKE '%SKIP LOCKED%'
		  AND argument NOT LIKE '%general_log%'
		ORDER BY event_time ASC`).Scan(&statements).Error; err != nil {
		live.Note = "unable to read live general log: " + err.Error()
	} else {
		for _, statement := range statements {
			trimmed := strings.TrimSpace(statement)
			if strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") && strings.Contains(strings.ToUpper(trimmed), "FOR UPDATE") {
				live.ClaimedStatement = trimmed
			}
		}
	}
	live.MatchesProbe = live.ClaimedStatement != "" && concwNormalizeSQL(live.ClaimedStatement) == concwNormalizeSQL(probeStatement)
	live.Note = fmt.Sprintf("%s; live_claim_events=%d; captured_statements=%d", live.Note, claimedCount, len(statements))
	if err := db.Exec("SET GLOBAL general_log = 'OFF'").Error; err != nil {
		live.Note += "; disable general log failed: " + err.Error()
	}
	_ = db.Exec("TRUNCATE TABLE mysql.general_log").Error
}

func concwNormalizeSQL(statement string) string {
	return strings.Join(strings.Fields(statement), " ")
}

func concwExplainClaimPlan(t *testing.T, db *gorm.DB, caseName, statement, databaseName string, runAnalyze bool) concwExplainEvidence {
	t.Helper()
	evidence := concwExplainEvidence{
		Case: caseName, Label: caseName, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		DatabaseName: databaseName, Statement: statement,
		StatusHistogram: map[string]int64{}, AccessPathCounts: map[string]int{}, ChosenKeys: map[string]string{},
	}
	if err := db.Raw("SELECT COUNT(*) FROM workflow_runs").Scan(&evidence.TableRowCount).Error; err != nil {
		t.Fatalf("count workflow_runs: %v", err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM workflow_runs WHERE runtime_mode = 'durable_v1'").Scan(&evidence.DurableRowCount).Error; err != nil {
		t.Fatalf("count durable runs: %v", err)
	}
	var histogram []struct {
		Status string
		Count  int64
	}
	if err := db.Raw("SELECT status, COUNT(*) AS count FROM workflow_runs WHERE runtime_mode = 'durable_v1' GROUP BY status").Scan(&histogram).Error; err != nil {
		t.Fatalf("durable status histogram: %v", err)
	}
	for _, row := range histogram {
		evidence.StatusHistogram[row.Status] = row.Count
	}
	var indexRows []struct {
		KeyName string `gorm:"column:Key_name"`
	}
	if err := db.Raw("SHOW INDEX FROM workflow_runs").Scan(&indexRows).Error; err != nil {
		t.Fatalf("show workflow_runs indexes: %v", err)
	}
	seen := map[string]bool{}
	for _, row := range indexRows {
		if !seen[row.KeyName] {
			seen[row.KeyName] = true
			evidence.Indexes = append(evidence.Indexes, row.KeyName)
		}
	}
	sort.Strings(evidence.Indexes)

	var plan string
	if err := db.Raw("EXPLAIN FORMAT=JSON " + statement).Scan(&plan).Error; err != nil {
		t.Fatalf("EXPLAIN FORMAT=JSON production claim SQL: %v", err)
	}
	evidence.RawPlanJSON = plan
	var decoded map[string]any
	if err := json.Unmarshal([]byte(plan), &decoded); err != nil {
		t.Fatalf("decode EXPLAIN JSON: %v", err)
	}
	concwCollectPlanFlags(decoded, "", &evidence)
	var tableNodes []map[string]any
	concwCollectTableNodes(decoded, &tableNodes)
	for _, node := range tableNodes {
		row := concwExplainTable{
			TableName:           concwStringField(node, "table_name"),
			AccessType:          concwStringField(node, "access_type"),
			Key:                 concwStringField(node, "key"),
			PossibleKeys:        concwStringList(node, "possible_keys"),
			RowsExaminedPerScan: concwFloatField(node, "rows_examined_per_scan"),
			RowsProducedPerJoin: concwFloatField(node, "rows_produced_per_join"),
			Filtered:            concwFloatField(node, "filtered"),
			UsingFilesort:       concwBoolField(node, "using_filesort"),
			UsingTemporaryTable: concwBoolField(node, "using_temporary_table"),
			IndexCondition:      concwNestedStringField(node, "index_condition"),
			AttachedCondition:   concwNestedStringField(node, "attached_condition"),
		}
		evidence.Tables = append(evidence.Tables, row)
		evidence.AccessPathCounts[row.AccessType]++
		if row.Key != "" {
			evidence.ChosenKeys[row.TableName] = row.Key
		}
		if row.UsingFilesort {
			evidence.UsingFilesort = true
		}
	}
	if len(tableNodes) == 0 && strings.Contains(plan, "\"using_filesort\": true") {
		evidence.UsingFilesort = true
	}
	if runAnalyze {
		var analyze string
		if err := db.Raw("EXPLAIN ANALYZE " + statement).Scan(&analyze).Error; err != nil {
			evidence.AnalyzeError = err.Error()
		} else {
			evidence.AnalyzePlanText = analyze
		}
	}
	return evidence
}

// TestWorkerConcurrencyClaimExplainDevSmall 在既有 dev 库（preflight 同规模的
// durable_v1 小表）上复用同一份生产 Claim SQL 采集小数据规模对照，仅执行
// EXPLAIN FORMAT=JSON（不执行 EXPLAIN ANALYZE，不写任何数据）。
func TestWorkerConcurrencyClaimExplainDevSmall(t *testing.T) {
	if os.Getenv(concwChildFlag) != "" {
		t.Skip("T1 concurrency harness runs only in child processes")
	}
	evidenceRoot := strings.TrimSpace(os.Getenv(concwEvidenceDir))
	if evidenceRoot == "" {
		t.Skip("T1 evidence harness: set SENTINELOPS_CONCW_EVIDENCE_DIR to run the small-scale EXPLAIN")
	}
	evidenceDir, err := filepath.Abs(evidenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Fatal("SENTINELOPS_TEST_DSN is required")
	}
	statement := concwReadCapturedStatement(t, evidenceDir)
	config, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	if config.DBName != "sentinelops_phase03" {
		t.Fatalf("refuse non-disposable base database %q", config.DBName)
	}
	// 只读对照：直接复用本机既有 dev 库（preflight 审计使用的同一张表）。
	config.DBName = "sentinelops"
	db, err := gorm.Open(gormmysql.Open(config.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open dev database for small-scale EXPLAIN: %v", err)
	}
	var databaseName string
	if err := db.Raw("SELECT DATABASE()").Scan(&databaseName).Error; err != nil {
		t.Fatal(err)
	}
	evidence := concwExplainClaimPlan(t, db, "small-dev-db", statement, databaseName, false)
	concwWriteJSON(t, filepath.Join(evidenceDir, "explain-small.json"), evidence)
	t.Logf("T1 small-scale EXPLAIN: rows=%d durable=%d access=%v filesort=%v indexes=%v",
		evidence.TableRowCount, evidence.DurableRowCount, evidence.AccessPathCounts, evidence.UsingFilesort, evidence.Indexes)
}

func concwReadCapturedStatement(t *testing.T, evidenceDir string) string {
	t.Helper()
	candidates, err := filepath.Glob(filepath.Join(evidenceDir, "claim-sql-capture-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(candidates)
	for _, candidate := range candidates {
		payload, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatal(err)
		}
		var wrapper struct {
			Probe concwClaimCapture `json:"probe"`
		}
		if err := json.Unmarshal(payload, &wrapper); err != nil {
			t.Fatalf("decode %s: %v", candidate, err)
		}
		if strings.TrimSpace(wrapper.Probe.Statement) != "" {
			return wrapper.Probe.Statement
		}
	}
	t.Fatalf("no captured production claim SQL under %s", evidenceDir)
	return ""
}

// concwCollectPlanFlags 在完整计划树上定位 filesort 与 sort_cost，而不是只看 table 节点。
func concwCollectPlanFlags(node any, path string, evidence *concwExplainEvidence) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			switch key {
			case "using_filesort":
				if flag, ok := child.(bool); ok && flag {
					evidence.UsingFilesort = true
					evidence.FilesortPaths = append(evidence.FilesortPaths, path)
				}
			case "sort_cost":
				if _, ok := child.(string); ok {
					evidence.SortCosts = append(evidence.SortCosts, fmt.Sprintf("%s=%v", path, child))
				}
			case "query_cost":
				if text, ok := child.(string); ok && evidence.QueryCost == "" {
					evidence.QueryCost = text
				}
			}
			concwCollectPlanFlags(child, path+"/"+key, evidence)
		}
	case []any:
		for index, child := range value {
			concwCollectPlanFlags(child, fmt.Sprintf("%s[%d]", path, index), evidence)
		}
	}
}

func concwCollectTableNodes(node any, sink *[]map[string]any) {
	switch value := node.(type) {
	case map[string]any:
		if _, ok := value["table_name"]; ok {
			*sink = append(*sink, value)
		}
		for _, child := range value {
			concwCollectTableNodes(child, sink)
		}
	case []any:
		for _, child := range value {
			concwCollectTableNodes(child, sink)
		}
	}
}

func concwStringField(node map[string]any, key string) string {
	if value, ok := node[key].(string); ok {
		return value
	}
	return ""
}

func concwNestedStringField(node map[string]any, key string) string {
	if value, ok := node[key].(string); ok {
		return value
	}
	if nested, ok := node[key].(map[string]any); ok {
		if value, ok := nested["attached_condition"].(string); ok {
			return value
		}
		for _, value := range nested {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
}

func concwStringList(node map[string]any, key string) []string {
	raw, ok := node[key].([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func concwFloatField(node map[string]any, key string) *float64 {
	switch value := node[key].(type) {
	case float64:
		return &value
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil
		}
		return &parsed
	}
	return nil
}

func concwBoolField(node map[string]any, key string) bool {
	value, ok := node[key].(bool)
	return ok && value
}

func concwTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	text := value.UTC().Format(time.RFC3339Nano)
	return &text
}

func concwWriteJSON(t *testing.T, path string, payload any) {
	t.Helper()
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("prepare evidence dir: %v", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
