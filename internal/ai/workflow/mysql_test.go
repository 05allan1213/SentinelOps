package workflow

import (
	"context"
	"database/sql"
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

const fixture07DisposableDatabasePrefix = "sentinelops_phase03"

func newP07Database(t *testing.T, suffix string) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Fatal("SENTINELOPS_TEST_DSN is required and must target the phase03 throwaway MySQL")
	}
	cfg, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse SENTINELOPS_TEST_DSN: %v", err)
	}
	if cfg.DBName != fixture07DisposableDatabasePrefix {
		t.Fatalf("refuse non-disposable database %q; want %q", cfg.DBName, fixture07DisposableDatabasePrefix)
	}
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(suffix) {
		t.Fatalf("unsafe disposable database suffix %q", suffix)
	}

	databaseName := fixture07DisposableDatabasePrefix + "_" + suffix
	adminCfg := *cfg
	adminCfg.DBName = "mysql"
	adminDB := openP07SQLDatabase(t, adminCfg.FormatDSN())
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
	runP07Goose(t, testDSN)
	sqlDB := openP07SQLDatabase(t, testDSN)
	t.Cleanup(func() { _ = sqlDB.Close() })
	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable GORM database: %v", err)
	}
	return gormDB
}

func openP07SQLDatabase(t *testing.T, dsn string) *sql.DB {
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

func runP07Goose(t *testing.T, dsn string) {
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
	migrationDir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations directory: %v", err)
	}
	output, err := exec.Command(binary, "-dir", migrationDir, "mysql", dsn, "up").CombinedOutput()
	if err != nil {
		t.Fatalf("goose up: %v\n%s", err, strings.ReplaceAll(string(output), dsn, "<redacted-dsn>"))
	}
}

func ptrString(value string) *string { return &value }
