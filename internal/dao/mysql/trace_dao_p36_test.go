package mysql

import (
	"context"
	"testing"
	"time"
)

func TestPhysicalDeleteTracePayloadUsesTerminalRunAnchor(t *testing.T) {
	_, db, dsn := newDisposableDatabase(t, "p36_trace_retention")
	requireMigrationsUp(t, dsn)
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-181 * 24 * time.Hour)
	terminalFinished := old
	terminal := WorkflowRun{
		ID: "run-p36-trace-terminal", WorkflowKey: "p36", UserID: "user-p36", SessionID: "session-terminal",
		Status: "succeeded", RuntimeMode: "durable_v1", AvailableAt: old, MaxAttempts: 3,
		StartedAt: old.Add(-time.Minute), FinishedAt: &terminalFinished, CreatedAt: old, UpdatedAt: old,
	}
	parked := WorkflowRun{
		ID: "run-p36-trace-parked", WorkflowKey: "p36", UserID: "user-p36", SessionID: "session-parked",
		Status: "parked", RuntimeMode: "durable_v1", AvailableAt: old, MaxAttempts: 3,
		StartedAt: old.Add(-time.Minute), CreatedAt: old, UpdatedAt: old,
	}
	if err := db.Create(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&parked).Error; err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ traceID, runID string }{
		{"trace-p36-terminal", terminal.ID}, {"trace-p36-parked", parked.ID},
	} {
		tags := `{"run_id":"` + fixture.runID + `","trace_quality":"complete"}`
		if err := db.Create(&TraceRun{
			TraceID: fixture.traceID, TraceName: "durable.attempt", SessionID: "session-p36",
			QueryText: "sensitive query", Status: "success", StartTime: old, EndTime: &old,
			Tags: tags, CreatedAt: old, UpdatedAt: old,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&TraceNode{
			TraceID: fixture.traceID, NodeID: "node-" + fixture.traceID, NodeType: "TOOL", NodeName: "tool",
			Status: "success", StartTime: old, EndTime: &old,
			PromptText: "sensitive prompt", CompletionText: "sensitive completion", QueryText: "sensitive query",
			RetrievedDocs: `[{"summary":"sensitive"}]`, Metadata: `{"tool_input":"sensitive","tool_output":"sensitive","model":{"catalog_ref":"provider/model"}}`,
			CreatedAt: old,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := physicalDeleteTracePayloads(context.Background(), db, now.Add(-30*24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("trace payload rows=%d, want 1", deleted)
	}
	var terminalNode, parkedNode TraceNode
	if err := db.First(&terminalNode, "trace_id = ?", "trace-p36-terminal").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&parkedNode, "trace_id = ?", "trace-p36-parked").Error; err != nil {
		t.Fatal(err)
	}
	if terminalNode.PromptText != "" || terminalNode.CompletionText != "" || terminalNode.QueryText != "" || terminalNode.RetrievedDocs != "" || terminalNode.Metadata == "" || containsTracePayloadKeys(terminalNode.Metadata) {
		t.Fatalf("terminal trace payload survived: %#v", terminalNode)
	}
	if parkedNode.PromptText == "" || parkedNode.Metadata == "" {
		t.Fatal("parked trace payload was deleted")
	}

	metadataDeleted, err := physicalDeleteTraceMetadata(context.Background(), db, now.Add(-180*24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if metadataDeleted != 1 {
		t.Fatalf("trace metadata rows=%d, want 1", metadataDeleted)
	}
	var parkedCount int64
	if err := db.Model(&TraceRun{}).Where("trace_id = ?", "trace-p36-parked").Count(&parkedCount).Error; err != nil || parkedCount != 1 {
		t.Fatalf("parked trace count=%d err=%v", parkedCount, err)
	}
}
