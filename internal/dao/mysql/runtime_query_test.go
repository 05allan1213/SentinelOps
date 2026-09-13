package mysql

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"gorm.io/gorm"
)

func runtimeQueryStore(t *testing.T, name string) (*GORMStore, *gorm.DB) {
	t.Helper()
	_, db, dsn := newDisposableDatabase(t, name)
	requireMigrationsUp(t, dsn)
	return NewGORMStore(db), db
}

func seedRuntimeRuns(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	rows := []WorkflowRun{
		{ID: "runtime-owned", WorkflowKey: "wf", UserID: "alice", SessionID: "s1", Status: "running", RuntimeMode: "durable_v1", ImmutableInputJSON: ptrString(`{"agent":"agent-a"}`), StartedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "runtime-other", WorkflowKey: "wf", UserID: "bob", SessionID: "s2", Status: "succeeded", RuntimeMode: "durable_v1", ImmutableInputJSON: ptrString(`{"agent":"agent-b"}`), StartedAt: now.Add(time.Second), CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
		{ID: "legacy-owned", WorkflowKey: "wf", UserID: "alice", SessionID: "s3", Status: "success", RuntimeMode: "legacy", StartedAt: now.Add(2 * time.Second), CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)},
		{ID: "runtime-missing", WorkflowKey: "wf", UserID: "alice", SessionID: "s5", Status: "running", RuntimeMode: "durable_v1", StartedAt: now.Add(4 * time.Second), CreatedAt: now.Add(4 * time.Second), UpdatedAt: now.Add(4 * time.Second)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
}

func TestListRuntimeRunsSafeAgentProjectionQuality(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_projection")
	seedRuntimeRuns(t, db)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	rows, _, err := store.ListRuntimeRuns(ctx, RuntimeRunFilter{Sort: "id", Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]WorkflowRun{}
	for _, row := range rows {
		seen[row.ID] = row
	}
	if seen["runtime-owned"].RuntimeAgent != "agent-a" || seen["runtime-owned"].RuntimeAgentQuality != "complete" {
		t.Fatalf("valid projection=%+v", seen["runtime-owned"])
	}
	if seen["runtime-missing"].RuntimeAgentQuality != "missing" {
		t.Fatalf("quality missing=%q", seen["runtime-missing"].RuntimeAgentQuality)
	}
}

func ptrString(v string) *string { return &v }

// TestListRuntimeRunsCarriesBudgetState 保护 Run 列表的 Budget 契约：
// 列表 DTO 暴露 model_calls/tool_calls 等计数，因此 list projection 必须
// 携带 budget_* 列；否则每一行都会被读模型判定为 invalid_budget_json。
func TestListRuntimeRunsCarriesBudgetState(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_budget")
	now := time.Now().UTC().Truncate(time.Millisecond)
	limits := `{"max_model_calls":8,"max_l0_tool_calls":4,"max_duration_ms":60000}`
	usage := `{"schema":"sentinelops/run-base-budget/v1","model_calls":2,"l0_tool_calls":1}`
	reservations := `{"schema":"sentinelops/run-base-budget/v1","items":{}}`
	row := WorkflowRun{
		ID: "runtime-budget", WorkflowKey: "wf", UserID: "alice", SessionID: "s-budget",
		Status: "succeeded", RuntimeMode: "durable_v1",
		BudgetLimitsJSON: &limits, BudgetUsageJSON: &usage, BudgetReservationsJSON: &reservations,
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	rows, _, err := store.ListRuntimeRuns(ctx, RuntimeRunFilter{Sort: "id", Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1", len(rows))
	}
	got := rows[0]
	var gotLimits struct {
		MaxModelCalls  int64 `json:"max_model_calls"`
		MaxL0ToolCalls int64 `json:"max_l0_tool_calls"`
		MaxDurationMS  int64 `json:"max_duration_ms"`
	}
	if got.BudgetLimitsJSON == nil || json.Unmarshal([]byte(*got.BudgetLimitsJSON), &gotLimits) != nil || gotLimits.MaxModelCalls != 8 || gotLimits.MaxL0ToolCalls != 4 || gotLimits.MaxDurationMS != 60000 {
		t.Fatalf("budget limits=%v want %s", got.BudgetLimitsJSON, limits)
	}
	var gotUsage struct {
		Schema     string `json:"schema"`
		ModelCalls int64  `json:"model_calls"`
	}
	if got.BudgetUsageJSON == nil || json.Unmarshal([]byte(*got.BudgetUsageJSON), &gotUsage) != nil || gotUsage.Schema != "sentinelops/run-base-budget/v1" || gotUsage.ModelCalls != 2 {
		t.Fatalf("budget usage=%v want %s", got.BudgetUsageJSON, usage)
	}
	var gotReservations struct {
		Schema string          `json:"schema"`
		Items  json.RawMessage `json:"items"`
	}
	if got.BudgetReservationsJSON == nil || json.Unmarshal([]byte(*got.BudgetReservationsJSON), &gotReservations) != nil || gotReservations.Schema != "sentinelops/run-base-budget/v1" || len(gotReservations.Items) == 0 {
		t.Fatalf("budget reservations=%v want %s", got.BudgetReservationsJSON, reservations)
	}
}

func TestListRuntimeRunsDefaultsToDurable(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_default")
	seedRuntimeRuns(t, db)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	rows, total, err := store.ListRuntimeRuns(ctx, RuntimeRunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("rows=%v total=%d", rows, total)
	}
}

func TestListRuntimeRunsLegacyRequiresExplicitFilter(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_legacy")
	seedRuntimeRuns(t, db)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	rows, total, err := store.ListRuntimeRuns(ctx, RuntimeRunFilter{IncludeLegacy: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("rows=%v total=%d", rows, total)
	}
}

func TestGetRuntimeRunCrossScope(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_scope")
	seedRuntimeRuns(t, db)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	_, err := store.GetRuntimeRun(ctx, "runtime-other")
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("err=%v, want record not found", err)
	}
}

func TestListRuntimeRunsScopeAndFilters(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_filters")
	seedRuntimeRuns(t, db)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "admin", Role: policy.RoleAdmin, Scope: policy.Scope{All: true}})
	rows, total, err := store.ListRuntimeRuns(ctx, RuntimeRunFilter{Scope: "all", Agent: "agent-b", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].UserID != "bob" {
		t.Fatalf("rows=%v total=%d", rows, total)
	}
}

func TestListRuntimeRunsStablePagination(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_pagination")
	seedRuntimeRuns(t, db)
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "admin", Role: policy.RoleAdmin, Scope: policy.Scope{All: true}})
	f := RuntimeRunFilter{Page: 1, PageSize: 1, Sort: "created_at", Direction: "asc"}
	a, _, err := store.ListRuntimeRuns(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := store.ListRuntimeRuns(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID {
		t.Fatalf("pagination unstable: %v vs %v", a, b)
	}
}

func TestRuntimeRunBoundaryContracts(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_boundaries")
	seedRuntimeRuns(t, db)
	viewer := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	rows, _, err := store.ListRuntimeRuns(viewer, RuntimeRunFilter{RuntimeMode: "legacy", Scope: "all", Sort: "bogus", Direction: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.RuntimeMode != "durable_v1" || row.InputPayload != "" || row.OutputPayload != "" {
			t.Fatalf("boundary row=%+v", row)
		}
	}
	if _, err := store.GetRuntimeRun(viewer, "legacy-owned"); err != gorm.ErrRecordNotFound {
		t.Fatalf("default legacy err=%v", err)
	}
	legacy, err := store.GetRuntimeRun(viewer, "legacy-owned", true)
	if err != nil || legacy.RuntimeMode != "legacy" {
		t.Fatalf("explicit legacy=%v err=%v", legacy, err)
	}
	admin := policy.WithIdentity(context.Background(), policy.Identity{UserID: "admin", Role: policy.RoleAdmin, Scope: policy.Scope{All: true}})
	if _, err := store.GetRuntimeRun(admin, "legacy-owned", true); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRunPaginationTieBreaksByID(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_query_ties")
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []string{"tie-b", "tie-a"} {
		if err := db.Create(&WorkflowRun{ID: id, WorkflowKey: "wf", UserID: "alice", Status: "running", RuntimeMode: "durable_v1", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"}})
	a, _, err := store.ListRuntimeRuns(ctx, RuntimeRunFilter{Page: 1, PageSize: 1, Sort: "created_at", Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := store.ListRuntimeRuns(ctx, RuntimeRunFilter{Page: 2, PageSize: 1, Sort: "created_at", Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || len(b) != 1 || a[0].ID == b[0].ID || a[0].ID != "tie-a" || b[0].ID != "tie-b" {
		t.Fatalf("tie pages=%v %v", a, b)
	}
}
