package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const disposableDatabasePrefix = "sentinelops_phase03"

func TestMigrationsUpFromEmptyDatabase(t *testing.T) {
	_, gormDB, dsn := newDisposableDatabase(t, "empty")
	requireMigrationsUp(t, dsn)
	requireSchemaVersion(t, gormDB)
}

func TestMigrationsUpFromCurrentSchemaSnapshot(t *testing.T) {
	_, gormDB, dsn := newDisposableDatabase(t, "current")
	requireCurrentSchemaSnapshot(t, gormDB)

	legacy := fixture03WorkflowRun{
		ID: "legacy-run", WorkflowKey: "legacy", SessionID: "legacy-session",
		Status: "running", StartedAt: time.Now(),
	}
	if err := gormDB.Create(&legacy).Error; err != nil {
		t.Fatalf("insert current-schema fixture: %v", err)
	}

	requireMigrationsUp(t, dsn)
	requireSchemaVersion(t, gormDB)
	assertRuntimeColumns(t, gormDB)
	assertRuntimeIndexes(t, gormDB)
	assertNoRuntimeForeignKeys(t, gormDB)

	var runtimeMode string
	if err := gormDB.Raw("SELECT runtime_mode FROM workflow_runs WHERE id = ?", legacy.ID).Scan(&runtimeMode).Error; err != nil {
		t.Fatalf("read migrated legacy run: %v", err)
	}
	if runtimeMode != "legacy" {
		t.Fatalf("runtime_mode = %q, want legacy", runtimeMode)
	}
}

func TestApplicationStartupDoesNotRunDDL(t *testing.T) {
	assertProductionStartupHasNoDDL(t)

	_, _, dsn := newDisposableDatabaseWithDSN(t, "startup_no_ddl")
	_, err := openAndCheckSchema(context.Background(), dsn)
	if !errors.Is(err, ErrSchemaVersionMismatch) {
		t.Fatalf("openAndCheckSchema error = %v, want ErrSchemaVersionMismatch", err)
	}

	db := openSQLDatabase(t, dsn)
	var tables int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables); err != nil {
		t.Fatalf("count startup-created tables: %v", err)
	}
	if tables != 0 {
		t.Fatalf("application startup created %d tables, want 0", tables)
	}
}

func assertProductionStartupHasNoDDL(t *testing.T) {
	t.Helper()
	databaseSource, err := os.ReadFile("database.go")
	if err != nil {
		t.Fatalf("read database startup source: %v", err)
	}
	for _, forbidden := range []string{"AutoMigrate(", "Migrator().", "CREATE TABLE", "CREATE INDEX", "ALTER TABLE", "DROP TABLE"} {
		if strings.Contains(string(databaseSource), forbidden) {
			t.Errorf("database startup contains forbidden DDL path %q", forbidden)
		}
	}
	mainSource, err := os.ReadFile(filepath.Join("..", "..", "..", "main.go"))
	if err != nil {
		t.Fatalf("read main startup source: %v", err)
	}
	if strings.Contains(string(mainSource), "CreateIndexes(") {
		t.Error("main startup still calls the legacy DDL index helper")
	}
}

func TestSchemaVersionMismatchFailsFast(t *testing.T) {
	_, _, dsn := newDisposableDatabaseWithDSN(t, "version_mismatch")
	runGoose(t, dsn, "up-to", fmt.Sprint(RequiredSchemaVersion-1))

	_, err := openAndCheckSchema(context.Background(), dsn)
	if !errors.Is(err, ErrSchemaVersionMismatch) {
		t.Fatalf("openAndCheckSchema error = %v, want ErrSchemaVersionMismatch", err)
	}
}

func TestLegacyApplicationModelsReadExpandSchema(t *testing.T) {
	_, gormDB, dsn := newDisposableDatabase(t, "legacy_models")
	requireMigrationsUp(t, dsn)

	now := time.Now().Truncate(time.Millisecond)
	fixtures := []any{
		&WorkflowRun{ID: "old-run", WorkflowKey: "old", SessionID: "old-session", Status: "running", StartedAt: now},
		&WorkflowEvent{RunID: "old-run", Seq: 1, EventType: "legacy.event", Payload: `{"legacy":true}`},
		&WorkflowCheckpoint{ID: "old-checkpoint", RunID: "old-run", CheckpointKey: "legacy", SnapshotJSON: `{"legacy":true}`},
		&KnowledgeBase{ID: "old-base", Name: "Old Base"},
		&TraceRun{TraceID: "00000000-0000-0000-0000-000000000003", CachedInputTokens: 7, ReasoningTokens: 11, StartTime: now},
		&TraceNode{TraceID: "00000000-0000-0000-0000-000000000003", NodeID: "00000000-0000-0000-0000-000000000004", CachedInputTokens: 13, ReasoningTokens: 17, StartTime: now},
	}
	for _, fixture := range fixtures {
		if err := gormDB.Create(fixture).Error; err != nil {
			t.Fatalf("old model %T cannot write expanded schema: %v", fixture, err)
		}
	}

	var run WorkflowRun
	if err := gormDB.First(&run, "id = ?", "old-run").Error; err != nil {
		t.Fatalf("old WorkflowRun cannot read expanded schema: %v", err)
	}
	var trace TraceRun
	if err := gormDB.First(&trace, "trace_id = ?", "00000000-0000-0000-0000-000000000003").Error; err != nil {
		t.Fatalf("old TraceRun cannot read expanded schema: %v", err)
	}
	if trace.CachedInputTokens != 7 || trace.ReasoningTokens != 11 {
		t.Fatalf("trace token split changed: cached=%d reasoning=%d", trace.CachedInputTokens, trace.ReasoningTokens)
	}
}

func TestRuntimeSchemaContract(t *testing.T) {
	_, gormDB, dsn := newDisposableDatabase(t, "contract")
	requireMigrationsUp(t, dsn)
	assertRuntimeColumns(t, gormDB)
	assertRuntimeIndexes(t, gormDB)
	assertNoRuntimeForeignKeys(t, gormDB)
}

func TestMigrationsDownOnDisposableDatabase(t *testing.T) {
	db, _, dsn := newDisposableDatabase(t, "down")
	requireMigrationsUp(t, dsn)
	runGoose(t, dsn, "down-to", "0")
	var tables int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version'").Scan(&tables); err != nil {
		t.Fatalf("count tables after down: %v", err)
	}
	if tables != 0 {
		t.Fatalf("tables after disposable down = %d, want 0", tables)
	}
}

func requireMigrationsUp(t *testing.T, dsn string) {
	t.Helper()
	runGoose(t, dsn, "up")
}

func runGoose(t *testing.T, dsn string, command ...string) string {
	t.Helper()
	binary := os.Getenv("SENTINELOPS_GOOSE_BIN")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("goose")
		if err != nil {
			t.Fatal("goose v3.27.3 is required; set SENTINELOPS_GOOSE_BIN to the pinned binary")
		}
	}
	versionOutput, err := exec.Command(binary, "-version").CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "v3.27.3") {
		t.Fatalf("goose version = %q err=%v, want v3.27.3", strings.TrimSpace(string(versionOutput)), err)
	}
	args := []string{"-dir", migrationsDirectory(t), "mysql", dsn}
	args = append(args, command...)
	output, err := exec.Command(binary, args...).CombinedOutput()
	redacted := strings.ReplaceAll(string(output), dsn, "<redacted-dsn>")
	if err != nil {
		t.Fatalf("goose %s: %v\n%s", strings.Join(command, " "), err, redacted)
	}
	return redacted
}

func requireSchemaVersion(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := CheckSchemaVersion(context.Background(), db); err != nil {
		t.Fatalf("schema version check: %v", err)
	}
}

func requireCurrentSchemaSnapshot(t *testing.T, db *gorm.DB) {
	t.Helper()
	models := []any{
		&Event{}, &Subscription{}, &Report{}, &User{}, &Setting{}, &QueryTermMapping{},
		&TraceRun{}, &TraceNode{}, &fixture03KnowledgeBase{}, &fixture03KnowledgeDocument{}, &fixture03KnowledgeChunk{},
		&MessageFeedback{}, &UserPreference{}, &OpsPlaybook{}, &OpsRun{}, &OpsRunStep{},
		&OpsProtectedAsset{}, &fixture03WorkflowRun{}, &fixture03WorkflowEvent{}, &fixture03WorkflowCheckpoint{},
		&fixture03SessionStateRevision{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("create current schema snapshot: %v", err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_protected_asset_type_value ON ops_protected_assets(asset_type, value)").Error; err != nil {
		t.Fatalf("create current snapshot protected-asset index: %v", err)
	}
	for _, statement := range []string{
		"CREATE INDEX idx_events_created_at ON events(created_at DESC)",
		"CREATE INDEX idx_trace_runs_created ON agent_trace_runs(created_at DESC)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create current snapshot manual index: %v", err)
		}
	}
}

// 下列冻结模型只用于重建 phase03 开始前的旧 Schema。生产 GORM 模型会随
// 后续单元映射 Expand 列，不能反向改变“从旧 Schema Up”的 contract test。
type fixture03KnowledgeBase struct {
	ID          string         `gorm:"column:id;primaryKey;size:64"`
	Name        string         `gorm:"column:name;size:128;not null"`
	Description string         `gorm:"column:description;type:text"`
	DocCount    int            `gorm:"column:doc_count;default:0"`
	ChunkCount  int            `gorm:"column:chunk_count;default:0"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (fixture03KnowledgeBase) TableName() string { return "knowledge_bases" }

type fixture03KnowledgeDocument struct {
	ID              string         `gorm:"column:id;primaryKey;size:64"`
	BaseID          string         `gorm:"column:base_id;size:64;not null;index"`
	Name            string         `gorm:"column:name;size:256;not null"`
	FilePath        string         `gorm:"column:file_path;size:512;not null"`
	FileSize        int64          `gorm:"column:file_size;not null"`
	FileType        string         `gorm:"column:file_type;size:32;not null;index"`
	FileHash        string         `gorm:"column:file_hash;size:64;index"`
	ChunkStrategy   string         `gorm:"column:chunk_strategy;size:32;not null"`
	ChunkConfig     string         `gorm:"column:chunk_config;type:json"`
	ChunkCount      int            `gorm:"column:chunk_count;default:0"`
	IndexedChunks   int            `gorm:"column:indexed_chunks;default:0"`
	IndexedAt       *time.Time     `gorm:"column:indexed_at;type:datetime"`
	IndexDurationMs int64          `gorm:"column:index_duration_ms;default:0"`
	IndexStatus     string         `gorm:"column:index_status;size:32;default:pending;index"`
	IndexError      string         `gorm:"column:index_error;type:text"`
	Enabled         bool           `gorm:"column:enabled;default:true"`
	CreatedAt       time.Time      `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
	DeletedAt       gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (fixture03KnowledgeDocument) TableName() string { return "knowledge_documents" }

type fixture03KnowledgeChunk struct {
	ID             string    `gorm:"column:id;primaryKey;size:64"`
	DocID          string    `gorm:"column:doc_id;size:64;not null;index"`
	ChunkIndex     int       `gorm:"column:chunk_index;not null"`
	ContentPreview string    `gorm:"column:content_preview;type:text"`
	SectionTitle   string    `gorm:"column:section_title;size:256"`
	CharCount      int       `gorm:"column:char_count;not null"`
	Enabled        bool      `gorm:"column:enabled;default:true"`
	CreatedAt      time.Time `gorm:"column:created_at;type:datetime;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;type:datetime;autoUpdateTime"`
}

func (fixture03KnowledgeChunk) TableName() string { return "knowledge_chunks" }

type fixture03WorkflowRun struct {
	ID            string         `gorm:"column:id;primaryKey;size:64"`
	WorkflowKey   string         `gorm:"column:workflow_key;size:128;not null;index"`
	SessionID     string         `gorm:"column:session_id;size:64;index"`
	Status        string         `gorm:"column:status;size:32;default:running;index"`
	InputPayload  string         `gorm:"column:input_payload;type:text"`
	OutputPayload string         `gorm:"column:output_payload;type:text"`
	ErrorMessage  string         `gorm:"column:error_message;type:text"`
	StartedAt     time.Time      `gorm:"column:started_at;type:datetime(3);not null"`
	FinishedAt    *time.Time     `gorm:"column:finished_at;type:datetime(3)"`
	DurationMs    int64          `gorm:"column:duration_ms;default:0"`
	CreatedAt     time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (fixture03WorkflowRun) TableName() string { return "workflow_runs" }

type fixture03WorkflowEvent struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	RunID     string    `gorm:"column:run_id;size:64;not null;uniqueIndex:idx_workflow_events_run_seq,priority:1"`
	Seq       int       `gorm:"column:seq;not null;uniqueIndex:idx_workflow_events_run_seq,priority:2"`
	EventType string    `gorm:"column:event_type;size:64;not null;index"`
	Payload   string    `gorm:"column:payload;type:text"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (fixture03WorkflowEvent) TableName() string { return "workflow_events" }

type fixture03WorkflowCheckpoint struct {
	ID            string         `gorm:"column:id;primaryKey;size:64"`
	RunID         string         `gorm:"column:run_id;size:64;not null;index"`
	CheckpointKey string         `gorm:"column:checkpoint_key;size:128;not null;index"`
	SnapshotJSON  string         `gorm:"column:snapshot_json;type:json;not null"`
	CreatedAt     time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (fixture03WorkflowCheckpoint) TableName() string { return "workflow_checkpoints" }

type fixture03SessionStateRevision struct {
	ID        uint           `gorm:"primaryKey;autoIncrement"`
	SessionID string         `gorm:"column:session_id;size:64;not null;uniqueIndex:idx_session_state_revisions_session_revision,priority:1"`
	Revision  int            `gorm:"column:revision;not null;uniqueIndex:idx_session_state_revisions_session_revision,priority:2"`
	StateJSON string         `gorm:"column:state_json;type:json;not null"`
	CreatedAt time.Time      `gorm:"column:created_at;autoCreateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (fixture03SessionStateRevision) TableName() string { return "session_state_revisions" }

func newDisposableDatabase(t *testing.T, suffix string) (*sql.DB, *gorm.DB, string) {
	t.Helper()
	return newDisposableDatabaseWithDSN(t, suffix)
}

func newDisposableDatabaseWithDSN(t *testing.T, suffix string) (*sql.DB, *gorm.DB, string) {
	t.Helper()
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Fatal("SENTINELOPS_TEST_DSN is required and must target the phase03 throwaway MySQL")
	}
	cfg, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse SENTINELOPS_TEST_DSN: %v", err)
	}
	if cfg.DBName != disposableDatabasePrefix {
		t.Fatalf("refuse non-disposable database %q; want %q", cfg.DBName, disposableDatabasePrefix)
	}
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(suffix) {
		t.Fatalf("unsafe disposable database suffix %q", suffix)
	}
	databaseName := disposableDatabasePrefix + "_" + suffix

	adminCfg := *cfg
	adminCfg.DBName = "mysql"
	adminDB := openSQLDatabase(t, adminCfg.FormatDSN())
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

	testCfg := *cfg
	testCfg.DBName = databaseName
	testDSN := testCfg.FormatDSN()
	db := openSQLDatabase(t, testDSN)
	t.Cleanup(func() { _ = db.Close() })
	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable GORM database: %v", err)
	}
	return db, gormDB, testDSN
}

func openSQLDatabase(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping MySQL: %v", err)
	}
	return db
}

func migrationsDirectory(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations directory: %v", err)
	}
	return dir
}

type columnContract struct {
	table, name, columnType, nullable string
	defaultValue                      *string
}

func ptr(value string) *string { return &value }

func assertRuntimeColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	contracts := runtimeColumnContracts()
	for _, contract := range contracts {
		var got struct {
			ColumnType    string  `gorm:"column:COLUMN_TYPE"`
			IsNullable    string  `gorm:"column:IS_NULLABLE"`
			ColumnDefault *string `gorm:"column:COLUMN_DEFAULT"`
		}
		err := db.Raw(`SELECT COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, contract.table, contract.name).Scan(&got).Error
		if err != nil {
			t.Errorf("query %s.%s: %v", contract.table, contract.name, err)
			continue
		}
		if got.ColumnType == "" {
			t.Errorf("missing column %s.%s", contract.table, contract.name)
			continue
		}
		if !strings.EqualFold(got.ColumnType, contract.columnType) || got.IsNullable != contract.nullable || !equalNullableString(got.ColumnDefault, contract.defaultValue) {
			t.Errorf("%s.%s = type %q nullable %q default %v; want type %q nullable %q default %v",
				contract.table, contract.name, got.ColumnType, got.IsNullable, printable(got.ColumnDefault), contract.columnType, contract.nullable, printable(contract.defaultValue))
		}
	}
}

func runtimeColumnContracts() []columnContract {
	noDefault := (*string)(nil)
	return []columnContract{
		// Spec 7.3: workflow_runs，包括全部 legacy 列与 durable Expand 列。
		{"workflow_runs", "id", "varchar(64)", "NO", noDefault},
		{"workflow_runs", "workflow_key", "varchar(128)", "NO", noDefault},
		{"workflow_runs", "session_id", "varchar(64)", "YES", noDefault},
		{"workflow_runs", "status", "varchar(32)", "YES", ptr("running")},
		{"workflow_runs", "input_payload", "text", "YES", noDefault},
		{"workflow_runs", "output_payload", "text", "YES", noDefault},
		{"workflow_runs", "error_message", "text", "YES", noDefault},
		{"workflow_runs", "started_at", "datetime(3)", "NO", noDefault},
		{"workflow_runs", "finished_at", "datetime(3)", "YES", noDefault},
		{"workflow_runs", "duration_ms", "bigint", "YES", ptr("0")},
		{"workflow_runs", "created_at", "datetime(3)", "YES", noDefault},
		{"workflow_runs", "updated_at", "datetime(3)", "YES", noDefault},
		{"workflow_runs", "deleted_at", "datetime(3)", "YES", noDefault},
		{"workflow_runs", "user_id", "varchar(64)", "YES", noDefault},
		{"workflow_runs", "parent_run_id", "varchar(64)", "YES", noDefault},
		{"workflow_runs", "active_session_key", "varchar(64)", "YES", noDefault},
		{"workflow_runs", "available_at", "datetime(3)", "NO", ptr("CURRENT_TIMESTAMP(3)")},
		{"workflow_runs", "priority", "int", "NO", ptr("0")},
		{"workflow_runs", "runtime_mode", "varchar(32)", "NO", ptr("legacy")},
		{"workflow_runs", "attempt", "int unsigned", "NO", ptr("0")},
		{"workflow_runs", "max_attempts", "int unsigned", "NO", ptr("3")},
		{"workflow_runs", "lease_owner", "varchar(128)", "YES", noDefault},
		{"workflow_runs", "lease_until", "datetime(3)", "YES", noDefault},
		{"workflow_runs", "lease_generation", "bigint unsigned", "NO", ptr("0")},
		{"workflow_runs", "heartbeat_at", "datetime(3)", "YES", noDefault},
		{"workflow_runs", "immutable_input_json", "json", "YES", noDefault},
		{"workflow_runs", "query_text", "longtext", "YES", noDefault},
		{"workflow_runs", "context_snapshot_json", "json", "YES", noDefault},
		{"workflow_runs", "session_revision", "bigint unsigned", "YES", noDefault},
		{"workflow_runs", "checkpoint_id", "varchar(128)", "YES", noDefault},
		{"workflow_runs", "interrupt_address", "varchar(512)", "YES", noDefault},
		{"workflow_runs", "recovery_mode", "varchar(32)", "YES", noDefault},
		{"workflow_runs", "runtime_version", "varchar(128)", "YES", noDefault},
		{"workflow_runs", "runtime_compatibility_hash", "char(64)", "YES", noDefault},
		{"workflow_runs", "agent_revision", "varchar(128)", "YES", noDefault},
		{"workflow_runs", "model_snapshot", "json", "YES", noDefault},
		{"workflow_runs", "tool_snapshot", "json", "YES", noDefault},
		{"workflow_runs", "mcp_catalog_hash", "char(64)", "YES", noDefault},
		{"workflow_runs", "skill_snapshot", "json", "YES", noDefault},
		{"workflow_runs", "prompt_hash", "char(64)", "YES", noDefault},
		{"workflow_runs", "policy_hash", "char(64)", "YES", noDefault},
		{"workflow_runs", "config_hash", "char(64)", "YES", noDefault},
		{"workflow_runs", "feature_snapshot", "json", "YES", noDefault},
		{"workflow_runs", "budget_limits_json", "json", "YES", noDefault},
		{"workflow_runs", "budget_usage_json", "json", "YES", noDefault},
		{"workflow_runs", "budget_reservations_json", "json", "YES", noDefault},
		{"workflow_runs", "usage_quality", "varchar(32)", "NO", ptr("unknown")},
		{"workflow_runs", "trace_quality", "varchar(32)", "NO", ptr("unknown")},
		{"workflow_runs", "last_event_seq", "bigint unsigned", "NO", ptr("0")},
		{"workflow_runs", "cancel_requested_at", "datetime(3)", "YES", noDefault},
		{"workflow_runs", "park_reason", "varchar(128)", "YES", noDefault},

		// Spec 7.4: workflow_events 的版本化 payload 与 durable seq。
		{"workflow_events", "id", "bigint unsigned", "NO", noDefault},
		{"workflow_events", "run_id", "varchar(64)", "NO", noDefault},
		{"workflow_events", "seq", "bigint unsigned", "NO", noDefault},
		{"workflow_events", "event_type", "varchar(64)", "NO", noDefault},
		{"workflow_events", "payload", "text", "YES", noDefault},
		{"workflow_events", "payload_version", "int unsigned", "NO", ptr("1")},
		{"workflow_events", "trace_id", "varchar(64)", "YES", noDefault},
		{"workflow_events", "created_at", "datetime(3)", "YES", noDefault},

		// Spec 7.5: legacy JSON 与 Eino opaque bytes 共存。
		{"workflow_checkpoints", "id", "varchar(64)", "NO", noDefault},
		{"workflow_checkpoints", "run_id", "varchar(64)", "NO", noDefault},
		{"workflow_checkpoints", "checkpoint_key", "varchar(128)", "NO", noDefault},
		{"workflow_checkpoints", "snapshot_json", "json", "NO", noDefault},
		{"workflow_checkpoints", "eino_checkpoint_id", "varchar(128)", "YES", noDefault},
		{"workflow_checkpoints", "checkpoint_blob", "longblob", "YES", noDefault},
		{"workflow_checkpoints", "payload_sha256", "char(64)", "YES", noDefault},
		{"workflow_checkpoints", "runtime_version", "varchar(128)", "YES", noDefault},
		{"workflow_checkpoints", "runtime_compatibility_hash", "char(64)", "YES", noDefault},
		{"workflow_checkpoints", "lease_generation", "bigint unsigned", "YES", noDefault},
		{"workflow_checkpoints", "committed_at", "datetime(3)", "YES", noDefault},
		{"workflow_checkpoints", "expires_at", "datetime(3)", "YES", noDefault},
		{"workflow_checkpoints", "created_at", "datetime(3)", "YES", noDefault},
		{"workflow_checkpoints", "updated_at", "datetime(3)", "YES", noDefault},
		{"workflow_checkpoints", "deleted_at", "datetime(3)", "YES", noDefault},

		// Spec 7.6: agent_approvals。
		{"agent_approvals", "id", "varchar(128)", "NO", noDefault},
		{"agent_approvals", "run_id", "varchar(64)", "NO", noDefault},
		{"agent_approvals", "tool_call_id_observed", "varchar(128)", "YES", noDefault},
		{"agent_approvals", "tool_name", "varchar(128)", "NO", noDefault},
		{"agent_approvals", "tool_revision", "varchar(128)", "NO", noDefault},
		{"agent_approvals", "tool_schema_hash", "char(64)", "NO", noDefault},
		{"agent_approvals", "risk_level", "varchar(32)", "NO", noDefault},
		{"agent_approvals", "proposal_json_redacted", "json", "NO", noDefault},
		{"agent_approvals", "proposal_hash", "char(64)", "NO", noDefault},
		{"agent_approvals", "policy_hash", "char(64)", "NO", noDefault},
		{"agent_approvals", "runtime_compatibility_hash", "char(64)", "NO", noDefault},
		{"agent_approvals", "requested_by", "varchar(128)", "NO", noDefault},
		{"agent_approvals", "decided_by", "varchar(128)", "YES", noDefault},
		{"agent_approvals", "status", "varchar(32)", "NO", ptr("preparing")},
		{"agent_approvals", "version", "bigint unsigned", "NO", ptr("1")},
		{"agent_approvals", "decision_reason", "text", "YES", noDefault},
		{"agent_approvals", "interrupt_id", "varchar(128)", "YES", noDefault},
		{"agent_approvals", "interrupt_address", "varchar(512)", "YES", noDefault},
		{"agent_approvals", "checkpoint_id", "varchar(128)", "YES", noDefault},
		{"agent_approvals", "checkpoint_payload_sha256", "char(64)", "YES", noDefault},
		{"agent_approvals", "checkpoint_lease_generation", "bigint unsigned", "YES", noDefault},
		{"agent_approvals", "preparing_at", "datetime(3)", "NO", noDefault},
		{"agent_approvals", "published_at", "datetime(3)", "YES", noDefault},
		{"agent_approvals", "expires_at", "datetime(3)", "YES", noDefault},
		{"agent_approvals", "created_at", "datetime(3)", "NO", noDefault},
		{"agent_approvals", "decided_at", "datetime(3)", "YES", noDefault},

		// Spec 7.7: agent_effects。
		{"agent_effects", "id", "varchar(128)", "NO", noDefault},
		{"agent_effects", "run_id", "varchar(64)", "NO", noDefault},
		{"agent_effects", "tool_call_id_observed", "varchar(128)", "YES", noDefault},
		{"agent_effects", "idempotency_key", "varchar(191)", "NO", noDefault},
		{"agent_effects", "effect_role", "varchar(32)", "NO", ptr("primary")},
		{"agent_effects", "effect_step", "varchar(128)", "NO", noDefault},
		{"agent_effects", "parent_effect_id", "varchar(128)", "YES", noDefault},
		{"agent_effects", "proposal_hash", "char(64)", "NO", noDefault},
		{"agent_effects", "tool_name", "varchar(128)", "NO", noDefault},
		{"agent_effects", "tool_revision", "varchar(128)", "NO", noDefault},
		{"agent_effects", "tool_schema_hash", "char(64)", "NO", noDefault},
		{"agent_effects", "target_hash", "char(64)", "NO", noDefault},
		{"agent_effects", "request_redacted", "json", "YES", noDefault},
		{"agent_effects", "response_redacted", "json", "YES", noDefault},
		{"agent_effects", "effect_type", "varchar(64)", "NO", noDefault},
		{"agent_effects", "status", "varchar(32)", "NO", ptr("pending")},
		{"agent_effects", "version", "bigint unsigned", "NO", ptr("1")},
		{"agent_effects", "external_reference", "varchar(512)", "YES", noDefault},
		{"agent_effects", "lease_generation", "bigint unsigned", "NO", noDefault},
		{"agent_effects", "attempt", "int unsigned", "NO", ptr("0")},
		{"agent_effects", "reconciliation_attempts", "int unsigned", "NO", ptr("0")},
		{"agent_effects", "next_reconcile_at", "datetime(3)", "YES", noDefault},
		{"agent_effects", "resolution", "varchar(64)", "YES", noDefault},
		{"agent_effects", "resolution_evidence_redacted", "json", "YES", noDefault},
		{"agent_effects", "resolved_by", "varchar(128)", "YES", noDefault},
		{"agent_effects", "resolved_at", "datetime(3)", "YES", noDefault},
		{"agent_effects", "started_at", "datetime(3)", "YES", noDefault},
		{"agent_effects", "finished_at", "datetime(3)", "YES", noDefault},
		{"agent_effects", "last_error", "text", "YES", noDefault},
		{"agent_effects", "created_at", "datetime(3)", "NO", noDefault},
		{"agent_effects", "updated_at", "datetime(3)", "NO", noDefault},

		// Spec 7.8: session_state_revisions。
		{"session_state_revisions", "id", "bigint unsigned", "NO", noDefault},
		{"session_state_revisions", "session_id", "varchar(64)", "NO", noDefault},
		{"session_state_revisions", "revision", "bigint unsigned", "NO", noDefault},
		{"session_state_revisions", "state_json", "json", "NO", noDefault},
		{"session_state_revisions", "created_at", "datetime(3)", "YES", noDefault},
		{"session_state_revisions", "deleted_at", "datetime(3)", "YES", noDefault},

		// Spec 7.9: 三个既有 Knowledge 表逐项具备 Evidence metadata。
		{"knowledge_bases", "content_hash", "char(64)", "YES", noDefault},
		{"knowledge_bases", "source_version", "varchar(128)", "YES", noDefault},
		{"knowledge_bases", "access_scope", "varchar(191)", "NO", ptr("public")},
		{"knowledge_bases", "indexed_version", "bigint unsigned", "NO", ptr("0")},
		{"knowledge_bases", "updated_by", "varchar(128)", "YES", noDefault},
		{"knowledge_documents", "content_hash", "char(64)", "YES", noDefault},
		{"knowledge_documents", "source_version", "varchar(128)", "YES", noDefault},
		{"knowledge_documents", "access_scope", "varchar(191)", "NO", ptr("public")},
		{"knowledge_documents", "indexed_version", "bigint unsigned", "NO", ptr("0")},
		{"knowledge_documents", "updated_by", "varchar(128)", "YES", noDefault},
		{"knowledge_chunks", "content_hash", "char(64)", "YES", noDefault},
		{"knowledge_chunks", "source_version", "varchar(128)", "YES", noDefault},
		{"knowledge_chunks", "access_scope", "varchar(191)", "NO", ptr("public")},
		{"knowledge_chunks", "indexed_version", "bigint unsigned", "NO", ptr("0")},
		{"knowledge_chunks", "updated_by", "varchar(128)", "YES", noDefault},

		// 当前 Trace token split 必须被基线保留。
		{"agent_trace_runs", "cached_input_tokens", "bigint", "YES", ptr("0")},
		{"agent_trace_runs", "reasoning_tokens", "bigint", "YES", ptr("0")},
		{"agent_trace_nodes", "cached_input_tokens", "bigint", "YES", noDefault},
		{"agent_trace_nodes", "reasoning_tokens", "bigint", "YES", noDefault},
	}
}

type indexContract struct {
	table, name, columns string
	unique               bool
}

func assertRuntimeIndexes(t *testing.T, db *gorm.DB) {
	t.Helper()
	contracts := []indexContract{
		{"events", "idx_events_created_at", "created_at", false},
		{"agent_trace_runs", "idx_trace_runs_created", "created_at", false},
		{"workflow_runs", "uidx_workflow_runs_active_session", "active_session_key", true},
		{"workflow_runs", "idx_workflow_runs_claim", "runtime_mode,status,available_at,priority,lease_until,id", false},
		{"workflow_runs", "idx_workflow_runs_reap", "status,lease_until,lease_generation", false},
		{"workflow_runs", "idx_workflow_runs_status_updated", "status,updated_at,id", false},
		{"workflow_runs", "idx_workflow_runs_user_scope", "user_id,status,created_at,id", false},
		{"workflow_events", "idx_workflow_events_run_seq", "run_id,seq", true},
		{"workflow_events", "idx_workflow_events_replay", "run_id,seq,event_type", false},
		{"workflow_checkpoints", "uidx_workflow_checkpoints_eino_id", "eino_checkpoint_id", true},
		{"workflow_checkpoints", "idx_workflow_checkpoints_run_expiry", "run_id,expires_at", false},
		{"agent_approvals", "uidx_agent_approvals_run_proposal", "run_id,proposal_hash", true},
		{"agent_approvals", "idx_agent_approvals_status_expiry", "status,expires_at,published_at", false},
		{"agent_effects", "uidx_agent_effects_idempotency", "idempotency_key", true},
		{"agent_effects", "uidx_agent_effects_proposal_step", "run_id,proposal_hash,effect_step", true},
		{"agent_effects", "idx_agent_effects_reconcile", "status,next_reconcile_at,effect_type", false},
		{"session_state_revisions", "idx_session_state_revisions_session_revision", "session_id,revision", true},
		{"knowledge_bases", "idx_knowledge_bases_scope_version", "access_scope,indexed_version,updated_at", false},
		{"knowledge_documents", "idx_knowledge_documents_scope_version", "access_scope,indexed_version,updated_at", false},
		{"knowledge_chunks", "idx_knowledge_chunks_scope_version", "access_scope,indexed_version,doc_id", false},
		{"agent_trace_runs", "idx_trace_runs_retention", "created_at,status", false},
		{"agent_trace_nodes", "idx_trace_nodes_retention", "created_at,trace_id", false},
		{"workflow_events", "idx_workflow_events_retention", "created_at,run_id", false},
	}
	for _, contract := range contracts {
		var got struct {
			Columns   string `gorm:"column:columns"`
			NonUnique int    `gorm:"column:non_unique"`
		}
		err := db.Raw(`SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ',') AS columns,
			MIN(NON_UNIQUE) AS non_unique
			FROM information_schema.STATISTICS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?
			GROUP BY INDEX_NAME`, contract.table, contract.name).Scan(&got).Error
		if err != nil {
			t.Errorf("query index %s.%s: %v", contract.table, contract.name, err)
			continue
		}
		if got.Columns != contract.columns || (got.NonUnique == 0) != contract.unique {
			t.Errorf("index %s.%s = columns %q unique %v; want columns %q unique %v", contract.table, contract.name, got.Columns, got.NonUnique == 0, contract.columns, contract.unique)
		}
	}
}

func assertNoRuntimeForeignKeys(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	err := db.Raw(`SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
		WHERE CONSTRAINT_SCHEMA = DATABASE()
		AND CONSTRAINT_TYPE = 'FOREIGN KEY'
		AND TABLE_NAME IN ('workflow_runs','workflow_events','workflow_checkpoints','agent_approvals','agent_effects','session_state_revisions')`).Scan(&count).Error
	if err != nil {
		t.Fatalf("query runtime foreign keys: %v", err)
	}
	if count != 0 {
		t.Fatalf("runtime foreign keys = %d, want 0 for expand compatibility", count)
	}
}

func equalNullableString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return strings.EqualFold(*left, *right)
}

func printable(value *string) string {
	if value == nil {
		return "NULL"
	}
	return fmt.Sprintf("%q", *value)
}
