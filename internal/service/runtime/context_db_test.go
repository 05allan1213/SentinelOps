package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestGetContextRealRunHistoryCountIsNonZero(t *testing.T) {
	db := newB1ContextDatabase(t, "b1_context")
	identity := policy.Identity{
		UserID: "user-b1-context", Role: policy.RoleOperator,
		Scope: policy.Scope{UserID: "user-b1-context"},
	}
	ctx := policy.WithIdentity(context.Background(), identity)
	sessionID := "session-b1-context"
	state := `{"schema":"fo/session-state/v1","revision":4,"summary":"prior durable run","history":[{"role":"user","content":"prior query"},{"role":"assistant","content":"prior answer"}],"provenance":{"history":"durable"}}`
	if err := db.Create(&mysql.SessionStateRevision{SessionID: sessionID, Revision: 4, StateJSON: state}).Error; err != nil {
		t.Fatalf("seed durable session revision: %v", err)
	}
	store := workflow.NewGORMStore(db)
	run, err := store.CreateRunWithSessionLock(ctx, b1ContextCreateInput("run-b1-context", sessionID))
	if err != nil {
		t.Fatalf("create real durable Run: %v", err)
	}
	service := &RuntimeService{Store: store}
	item, err := service.GetContext(ctx, run.ID, true)
	if err != nil {
		t.Fatalf("read real Run context: %v", err)
	}
	if item.HistoryCount != 2 || len(item.History) != 2 || item.HistoryTruncated || item.RedactionApplied {
		t.Fatalf("real Run context=%+v", item)
	}
	if item.History[0].Content != "prior query" || item.History[1].Content != "prior answer" {
		t.Fatalf("real Run history=%+v", item.History)
	}
	metaOnly, err := service.GetContext(ctx, run.ID, false)
	if err != nil {
		t.Fatalf("read real Run context metadata: %v", err)
	}
	if metaOnly.HistoryCount != 2 || len(metaOnly.History) != 0 {
		t.Fatalf("metadata-only real Run context=%+v", metaOnly)
	}
}

func b1ContextCreateInput(runID, sessionID string) workflow.CreateRunInput {
	return workflow.CreateRunInput{
		ID: runID, WorkflowKey: "b1-context-test", SessionID: sessionID,
		QueryText: "durable run", ImmutableInputJSON: json.RawMessage(`{"agent":"test","query":"durable run"}`),
		RuntimeSnapshot: workflow.RuntimeSnapshotFields{
			RuntimeVersion:           "b1-context-v1",
			RuntimeCompatibilityHash: strings.Repeat("a", 64),
			AgentRevision:            "agent-b1-v1",
			ModelSnapshotJSON:        json.RawMessage(`[]`),
			ToolSnapshotJSON:         json.RawMessage(`[]`),
			MCPCatalogHash:           strings.Repeat("b", 64),
			SkillSnapshotJSON:        json.RawMessage(`[]`),
			PromptHash:               strings.Repeat("c", 64),
			PolicyHash:               strings.Repeat("d", 64),
			ConfigHash:               strings.Repeat("e", 64),
			FeatureSnapshotJSON:      json.RawMessage(`{}`),
		},
		BudgetLimitsJSON: json.RawMessage(`{}`),
		DeadlineAt:       time.Now().Add(time.Hour),
		CreatedEvent:     workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	}
}

func newB1ContextDatabase(t *testing.T, suffix string) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Skip("SENTINELOPS_TEST_DSN is required for disposable MySQL context evidence")
	}
	config, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse SENTINELOPS_TEST_DSN: %v", err)
	}
	if config.DBName != "sentinelops_phase03" {
		t.Fatalf("refuse non-disposable database %q", config.DBName)
	}
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(suffix) {
		t.Fatalf("unsafe disposable database suffix %q", suffix)
	}
	databaseName := "sentinelops_phase03_" + suffix
	adminConfig := *config
	adminConfig.DBName = "mysql"
	adminDB := openB1ContextSQL(t, adminConfig.FormatDSN())
	quotedName := "`" + databaseName + "`"
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName); err != nil {
		t.Fatalf("drop stale disposable database: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + quotedName + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatalf("create disposable database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName)
		_ = adminDB.Close()
	})
	testConfig := *config
	testConfig.DBName = databaseName
	testDSN := testConfig.FormatDSN()
	runB1ContextGoose(t, testDSN)
	sqlDB := openB1ContextSQL(t, testDSN)
	t.Cleanup(func() { _ = sqlDB.Close() })
	gormDB, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable GORM database: %v", err)
	}
	return gormDB
}

func openB1ContextSQL(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		t.Fatalf("ping MySQL: %v", err)
	}
	return db
}

func runB1ContextGoose(t *testing.T, dsn string) {
	t.Helper()
	binary := os.Getenv("SENTINELOPS_GOOSE_BIN")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("goose")
		if err != nil {
			t.Fatal("goose v3.27.3 is required")
		}
	}
	versionOutput, err := exec.Command(binary, "-version").CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "v3.27.3") {
		t.Fatalf("goose version = %q err=%v, want v3.27.3", strings.TrimSpace(string(versionOutput)), err)
	}
	migrationDir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations directory: %v", err)
	}
	output, err := exec.Command(binary, "-dir", migrationDir, "mysql", dsn, "up").CombinedOutput()
	if err != nil {
		t.Fatalf("goose up: %v\n%s", err, strings.ReplaceAll(string(output), dsn, "<redacted-dsn>"))
	}
}
