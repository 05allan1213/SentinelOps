package runtime

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/rageval"
	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestEvalDistinguishesRAGEvalAndAgentEval(t *testing.T) {
	rag := mapRAGEvalMetrics(&rageval.DashboardMetrics{TotalRuns: 4, SuccessRate: 0.75})
	if rag.Suite != EvalSuiteRAG || rag.CaseCount != 4 || rag.Passed != 3 || rag.Failed != 1 {
		t.Fatalf("RAG result=%+v", rag)
	}
	if rag.DeterministicGateResult != "not_applicable" || rag.LLMJudgeResult != "not_applicable" {
		t.Fatalf("RAG result was promoted to Agent Eval evidence: %+v", rag)
	}

	agent, err := (&RuntimeService{}).GetEval(context.Background(), EvalFilter{Suite: "runtime", Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Item.Suite != EvalSuiteAgent || !agent.Item.NotRun || agent.Item.ReasonCode != "eval_not_executed" {
		t.Fatalf("Agent result=%+v", agent.Item)
	}
	if agent.Item.CaseCount != 0 || agent.Item.Passed != 0 || agent.Item.Failed != 0 || agent.Item.Regression != nil {
		t.Fatalf("Agent Eval fabricated aggregate=%+v", agent.Item)
	}
}

func TestEvalMalformedOrAbsentSourceIsNotRun(t *testing.T) {
	for _, suite := range []string{"", "agent_eval", "runtime", "unknown-suite"} {
		t.Run(suite, func(t *testing.T) {
			res, err := (&RuntimeService{}).GetEval(context.Background(), EvalFilter{Suite: suite})
			if err != nil {
				t.Fatal(err)
			}
			if res.Availability != v1.AvailabilityUnavailable || res.DataQuality != v1.DataQualityUnknown || !res.NotRun || res.ReasonCode != "eval_not_executed" {
				t.Fatalf("absent Agent Eval source metadata=%+v", res.ResourceMeta)
			}
			if res.Item.Availability != res.Availability || res.Item.ReasonCode != res.ReasonCode || !res.Item.NotRun {
				t.Fatalf("item metadata=%+v wrapper=%+v", res.Item.ResourceMeta, res.ResourceMeta)
			}
		})
	}

	malformed := mapRAGEvalMetrics(nil)
	if malformed.Suite != EvalSuiteRAG || malformed.Availability != v1.AvailabilityUnavailable || !malformed.NotRun || malformed.ReasonCode != "rag_eval_unavailable" {
		t.Fatalf("malformed optional RAG source=%+v", malformed)
	}
	ragUnavailable, err := (&RuntimeService{}).GetEval(context.Background(), EvalFilter{Suite: EvalSuiteRAG})
	if err != nil {
		t.Fatal(err)
	}
	if ragUnavailable.Item.Suite != EvalSuiteRAG || ragUnavailable.Availability != v1.AvailabilityUnavailable || !ragUnavailable.NotRun || ragUnavailable.ReasonCode != "rag_eval_unavailable" {
		t.Fatalf("missing RAG source metadata=%+v", ragUnavailable)
	}
	for name, metrics := range map[string]*rageval.DashboardMetrics{
		"empty object":  {TotalRuns: 0, SuccessRate: 0},
		"invalid count": {TotalRuns: -1, SuccessRate: 0},
		"invalid rate":  {TotalRuns: 1, SuccessRate: 2},
	} {
		t.Run(name, func(t *testing.T) {
			res := mapRAGEvalMetrics(metrics)
			if res.Availability == v1.AvailabilityAvailable || !res.NotRun || res.CaseCount != 0 || res.Passed != 0 || res.Failed != 0 {
				t.Fatalf("malformed RAG source fabricated evidence=%+v", res)
			}
		})
	}
}

func TestReleaseWithoutRolloutSourceIsUnavailable(t *testing.T) {
	res, err := (&RuntimeService{}).GetRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Item.RuntimeVersion) == "" {
		t.Fatal("release did not expose current runtime version")
	}
	if res.Item.GrayState != nil || res.Item.RollbackState != nil {
		t.Fatalf("release fabricated rollout state=%+v", res.Item)
	}
	if res.Availability != v1.AvailabilityUnavailable || res.DataQuality != v1.DataQualityUnknown || res.ReasonCode != "not_observed" || !res.NotRun {
		t.Fatalf("release metadata=%+v", res.ResourceMeta)
	}
}

func TestReleaseUsesObservedWorkerVersions(t *testing.T) {
	values := make(map[string]bool, len(airuntime.CanonicalGateKeys()))
	for _, key := range airuntime.CanonicalGateKeys() {
		values[key] = true
	}
	static, err := airuntime.NewGateVector(values)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := make(map[string]string, len(values))
	for _, key := range airuntime.CanonicalGateKeys() {
		dynamic[key] = "true"
	}
	dynamic[airuntime.GateAgentRuntimeL1Writes] = "false"
	evaluator, err := airuntime.NewGateEvaluator(static, func(context.Context, []string) (map[string]string, error) {
		return dynamic, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	versionB, versionA := "worker-v2", "worker-v1"
	rows := []mysql.RuntimeWorkerSnapshot{
		{WorkerID: "worker-a", RuntimeVersion: &versionB},
		{WorkerID: "worker-b", RuntimeVersion: &versionA},
		{WorkerID: "worker-c", RuntimeVersion: &versionB},
		{WorkerID: "worker-missing"},
	}

	res, err := (&RuntimeService{Gates: evaluator}).GetRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Item.GateVector[airuntime.GateAgentRuntimeL1Writes] {
		t.Fatalf("release ignored current Gate vector=%v", res.Item.GateVector)
	}
	if len(res.Item.ObservedWorkerVersions) != 0 {
		t.Fatalf("release used non-persisted worker versions=%v", res.Item.ObservedWorkerVersions)
	}
	if got := observedWorkerVersions(rows); !reflect.DeepEqual(got, []string{"worker-v1", "worker-v2"}) {
		t.Fatalf("observed versions=%v", got)
	}
}

func TestReleaseServiceProjectsPersistedWorkerVersions(t *testing.T) {
	dsn := os.Getenv("SENTINELOPS_TEST_DSN")
	if dsn == "" {
		t.Skip("SENTINELOPS_TEST_DSN is required for disposable MySQL release projection evidence")
	}
	baseConfig, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse SENTINELOPS_TEST_DSN: %v", err)
	}
	if baseConfig.DBName != "sentinelops_phase03" {
		t.Fatalf("refuse non-disposable database %q", baseConfig.DBName)
	}
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(baseConfig.DBName) {
		t.Fatalf("unsafe database name %q", baseConfig.DBName)
	}
	databaseName := "sentinelops_phase03_h03_release"
	adminConfig := *baseConfig
	adminConfig.DBName = "mysql"
	adminDB, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	quotedName := "`" + databaseName + "`"
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName); err != nil {
		t.Fatalf("drop stale disposable database: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + quotedName + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatalf("create disposable database: %v", err)
	}
	t.Cleanup(func() { _, _ = adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName) })
	testConfig := *baseConfig
	testConfig.DBName = databaseName
	db, err := gorm.Open(gormmysql.Open(testConfig.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&mysql.RuntimeWorkerSnapshot{}); err != nil {
		t.Fatal(err)
	}
	versionB, versionA := "release-worker-v2", "release-worker-v1"
	now := time.Now().UTC().Truncate(time.Millisecond)
	rows := []mysql.RuntimeWorkerSnapshot{
		{WorkerID: "release-observed-a", RuntimeVersion: &versionB, HeartbeatAt: &now},
		{WorkerID: "release-observed-b", RuntimeVersion: &versionA, HeartbeatAt: &now},
		{WorkerID: "release-observed-c", RuntimeVersion: &versionB, HeartbeatAt: &now},
	}
	defer db.Where("worker_id IN ?", []string{"release-observed-a", "release-observed-b", "release-observed-c"}).Delete(&mysql.RuntimeWorkerSnapshot{})
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	res, err := (&RuntimeService{Store: workflow.NewGORMStore(db)}).GetRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Item.ObservedWorkerVersions; !reflect.DeepEqual(got, []string{"release-worker-v1", "release-worker-v2"}) {
		t.Fatalf("persisted worker versions=%v", got)
	}
}

func TestReleaseUsesCurrentGateVector(t *testing.T) {
	values := make(map[string]bool, len(airuntime.CanonicalGateKeys()))
	for _, key := range airuntime.CanonicalGateKeys() {
		values[key] = true
	}
	static, err := airuntime.NewGateVector(values)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := make(map[string]string, len(values))
	for _, key := range airuntime.CanonicalGateKeys() {
		dynamic[key] = "true"
	}
	dynamic[airuntime.GateAgentRuntimeL2Writes] = "false"
	evaluator, err := airuntime.NewGateEvaluator(static, func(context.Context, []string) (map[string]string, error) {
		return dynamic, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&RuntimeService{Gates: evaluator}).GetRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Item.GateVector[airuntime.GateAgentRuntimeL2Writes] {
		t.Fatalf("release ignored current Gate vector=%v", res.Item.GateVector)
	}
}

func TestIncompleteTraceCannotPassRelease(t *testing.T) {
	res, err := (&RuntimeService{}).GetRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Release has no rollout source and therefore cannot claim a pass even if
	// an incomplete legacy trace exists elsewhere in the database. RAG metric
	// aggregation separately reuses EvidenceTraceQualityPredicate.
	if res.Item.GrayState != nil || res.Item.RollbackState != nil || !res.Item.NotRun || res.Item.ReasonCode != "not_observed" {
		t.Fatalf("incomplete evidence was treated as release success=%+v", res.Item)
	}
	const expectedPredicate = "COALESCE(CASE WHEN JSON_VALID(agent_trace_runs.tags) THEN JSON_UNQUOTE(JSON_EXTRACT(agent_trace_runs.tags, '$.trace_quality')) ELSE NULL END, 'unknown') <> 'incomplete'"
	if mysql.EvidenceTraceQualityPredicate != expectedPredicate {
		t.Fatalf("release evidence changed the shared trace-quality contract: %q", mysql.EvidenceTraceQualityPredicate)
	}
}
