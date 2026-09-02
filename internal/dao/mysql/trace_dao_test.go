package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"

	"gorm.io/gorm"
)

func TestRuntimeTraceQueriesAggregateAttemptsWithScopeAndSafeProjection(t *testing.T) {
	_, db := runtimeQueryStore(t, "runtime_trace_projection")
	now := time.Now().UTC().Truncate(time.Millisecond)
	owned := WorkflowRun{
		ID: "trace-owned-run", WorkflowKey: "wf", UserID: "alice", Status: "succeeded", RuntimeMode: "durable_v1",
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	other := WorkflowRun{
		ID: "trace-other-run", WorkflowKey: "wf", UserID: "bob", Status: "succeeded", RuntimeMode: "durable_v1",
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&[]WorkflowRun{owned, other}).Error; err != nil {
		t.Fatal(err)
	}
	seedTraceRunsForQuery(t, db, now, owned.ID, other.ID)

	alice := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"},
	})
	dao := NewTraceDAOWithDB(db)
	rows, err := dao.ListRunsByWorkflowRunID(alice, owned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].TraceID != "trace-owned-1" || rows[1].TraceID != "trace-owned-2" {
		t.Fatalf("owned trace attempts=%#v, want both attempts in order", rows)
	}
	for _, row := range rows {
		if row.QueryText != "" || row.ErrorMessage != "" {
			t.Fatalf("trace metadata projection loaded raw fields: %#v", row)
		}
	}
	if cross, err := dao.ListRunsByWorkflowRunID(alice, other.ID); err != nil || len(cross) != 0 {
		t.Fatalf("cross-scope trace rows=%d err=%v, want empty", len(cross), err)
	}

	latest, err := dao.GetRunByWorkflowRunID(alice, owned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.TraceID != "trace-owned-2" || latest.QueryText != "" || latest.ErrorMessage != "" {
		t.Fatalf("latest trace projection=%#v", latest)
	}
	if _, err := dao.GetRunByWorkflowRunID(alice, other.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-scope latest trace err=%v, want record not found", err)
	}

	count, err := dao.CountNodesByTraceID(alice, "trace-owned-1")
	if err != nil || count != 1 {
		t.Fatalf("owned node count=%d err=%v, want 1", count, err)
	}
	crossCount, err := dao.CountNodesByTraceID(alice, "trace-other")
	if err != nil || crossCount != 0 {
		t.Fatalf("cross-scope node count=%d err=%v, want 0", crossCount, err)
	}
	retrieval, err := dao.ListRetrievalMetadataByTraceID(alice, "trace-owned-1")
	if err != nil || len(retrieval) != 1 || retrieval[0].RetrievedDocs != `[{"evidence_id":"evidence-v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","score":0.9}]` {
		t.Fatalf("retrieval metadata=%#v err=%v", retrieval, err)
	}

	admin := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "admin", Role: policy.RoleAdmin, Scope: policy.Scope{All: true},
	})
	adminRows, err := dao.ListRunsByWorkflowRunID(admin, other.ID)
	if err != nil || len(adminRows) != 1 || adminRows[0].TraceID != "trace-other" {
		t.Fatalf("admin trace rows=%#v err=%v", adminRows, err)
	}
}

func seedTraceRunsForQuery(t *testing.T, db *gorm.DB, now time.Time, ownedID, otherID string) {
	t.Helper()
	traceRows := []TraceRun{
		{
			TraceID: "trace-owned-1", TraceName: "durable.attempt", EntryPoint: "worker", SessionID: "session-owned",
			QueryText: "secret query attempt one", Status: "success", ErrorMessage: "secret error one", ErrorCode: "",
			StartTime: now, EndTime: timePtr(now.Add(time.Second)), DurationMs: 100, TotalInputTokens: 10, TotalOutputTokens: 20,
			Tags:      `{"run_id":"` + ownedID + `","server_user_id":"alice","attempt":1,"trace_quality":"complete"}`,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			TraceID: "trace-owned-2", TraceName: "durable.attempt", EntryPoint: "worker", SessionID: "session-owned",
			QueryText: "secret query attempt two", Status: "success", ErrorMessage: "secret error two", ErrorCode: "",
			StartTime: now.Add(time.Second), EndTime: timePtr(now.Add(2 * time.Second)), DurationMs: 200, TotalInputTokens: 30, TotalOutputTokens: 40,
			Tags:      `{"run_id":"` + ownedID + `","server_user_id":"alice","attempt":2,"trace_quality":"incomplete"}`,
			CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
		},
		{
			TraceID: "trace-other", TraceName: "durable.attempt", EntryPoint: "worker", SessionID: "session-other",
			QueryText: "other secret query", Status: "success", ErrorMessage: "other secret error",
			StartTime: now, EndTime: timePtr(now.Add(time.Second)), DurationMs: 100,
			Tags:      `{"run_id":"` + otherID + `","server_user_id":"bob","attempt":1,"trace_quality":"complete"}`,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := db.Create(&traceRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&TraceNode{
		TraceID: "trace-owned-1", NodeID: "node-owned-1", NodeType: "retrieval", NodeName: "retrieval",
		Status: "success", StartTime: now, EndTime: timePtr(now.Add(100 * time.Millisecond)),
		PromptText: "secret prompt", CompletionText: "secret completion", QueryText: "secret node query",
		RetrievedDocs: `[{"evidence_id":"evidence-v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","score":0.9}]`,
		Metadata:      `{"tool_input":"secret tool input"}`, CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func timePtr(value time.Time) *time.Time { return &value }
