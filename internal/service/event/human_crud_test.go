package eventsvc

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestHumanCRUDKeepsServiceTransactionPathOutsideAgentRuntime(t *testing.T) {
	source, err := os.ReadFile("event.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, forbidden := range []string{"internal/ai/runtime", "internal/ai/effects", "AgentApproval", "AgentEffect"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("ordinary event Service CRUD depends on %q", forbidden)
		}
	}
	if !strings.Contains(text, "dao.UpdateEventStatus(ctx, id, status)") {
		t.Fatal("ordinary event status update no longer delegates directly to the existing DAO transaction path")
	}
}

func TestHumanCRUDChangesDataWithoutApprovalOrEffect(t *testing.T) {
	db := fixture26HumanCRUDDatabase(t)
	event := dao.Event{
		ID: "phase26-human-crud", Title: "phase26 human CRUD", Content: "manual operation",
		EventType: "manual", Severity: "medium", Source: "phase26", Status: "new",
		Metadata: `{}`, RawPayload: `{}`,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "admin-phase26", Role: policy.RoleAdmin, Scope: policy.Scope{All: true},
	})
	if err := UpdateStatus(ctx, event.ID, "resolved"); err != nil {
		t.Fatal(err)
	}
	var stored dao.Event
	if err := db.First(&stored, "id = ?", event.ID).Error; err != nil {
		t.Fatal(err)
	}
	var approvalCount, effectCount int64
	if err := db.Model(&dao.AgentApproval{}).Count(&approvalCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&dao.AgentEffect{}).Count(&effectCount).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "resolved" || approvalCount != 0 || effectCount != 0 {
		t.Fatalf("human CRUD status=%q approvals=%d effects=%d", stored.Status, approvalCount, effectCount)
	}
}

func fixture26HumanCRUDDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Fatal("SENTINELOPS_TEST_DSN is required for phase26 human CRUD integration")
	}
	config, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse phase26 test DSN: %v", err)
	}
	if config.DBName != "sentinelops_phase03" {
		t.Fatalf("refuse non-disposable database %q", config.DBName)
	}
	databaseName := "sentinelops_phase03_phase26_human_crud"
	adminConfig := *config
	adminConfig.DBName = "mysql"
	adminDB, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	quotedName := "`" + databaseName + "`"
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + quotedName + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.Exec("DROP DATABASE IF EXISTS " + quotedName) })

	testConfig := *config
	testConfig.DBName = databaseName
	testDSN := testConfig.FormatDSN()
	gooseBinary := os.Getenv("SENTINELOPS_GOOSE_BIN")
	if gooseBinary == "" {
		gooseBinary, err = exec.LookPath("goose")
		if err != nil {
			t.Fatal("goose v3.27.3 is required for phase26 human CRUD integration")
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
	output, err := exec.Command(gooseBinary, "-dir", migrationDirectory, "mysql", testDSN, "up").CombinedOutput()
	if err != nil {
		t.Fatalf("phase26 goose up: %v\n%s", err, strings.ReplaceAll(string(output), testDSN, "<redacted-dsn>"))
	}
	if err := dao.InitWithDSN(context.Background(), []byte(testDSN)); err != nil {
		t.Fatal(err)
	}
	db, err := dao.DB(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return db
}
