package mysql

import (
	"context"
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
