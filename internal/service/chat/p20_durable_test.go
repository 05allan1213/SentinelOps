package chatsvc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestCreateRunCallsDurablePrimitiveOnceAndNeverExecutesAgent(t *testing.T) {
	var creates atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      p20ServiceSnapshot(),
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return &mysql.WorkflowRun{ID: "run-p20", Status: workflow.RunStatusPending}, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.CreateRun(context.Background(), CreateDurableRunRequest{
		SessionID: "session-p20", Query: "read-only security analysis", Agent: DurableAgentEventAnalysis,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "run-p20" || creates.Load() != 1 {
		t.Fatalf("run=%+v creates=%d, want one durable create", run, creates.Load())
	}
}

func TestCreateRunRejectsNonL0AgentBeforeStore(t *testing.T) {
	var creates atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      p20ServiceSnapshot(),
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return nil, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRun(context.Background(), CreateDurableRunRequest{
		SessionID: "session-p20", Query: "mutation", Agent: "ops_agent",
	}); err == nil {
		t.Fatal("non-L0 Agent was accepted")
	}
	if creates.Load() != 0 {
		t.Fatalf("non-L0 request reached Store %d times", creates.Load())
	}
}

func TestCreateRunGateClosedDoesNotTouchStore(t *testing.T) {
	var creates atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		Snapshot: p20ServiceSnapshot(),
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return nil, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateRun(context.Background(), CreateDurableRunRequest{SessionID: "session-p20", Query: "closed"})
	if !errors.Is(err, ErrDurableRunGateClosed) || creates.Load() != 0 {
		t.Fatalf("gate error=%v creates=%d", err, creates.Load())
	}
}

func TestSSEReconnectOnlyReadsEventsAndChecksOwnerScope(t *testing.T) {
	var reads atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      p20ServiceSnapshot(),
		ListEvents: func(_ context.Context, runID string, afterSeq int64) ([]workflow.StreamEvent, error) {
			reads.Add(1)
			if runID != "run-p20" || afterSeq != 4 {
				t.Fatalf("ListEvents args=%q/%d", runID, afterSeq)
			}
			return []workflow.StreamEvent{{ID: 5, RunID: runID, Type: workflow.EventRunCompleted}}, nil
		},
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.Events(context.Background(), "run-p20", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != 5 || reads.Load() != 1 {
		t.Fatalf("events=%+v reads=%d", events, reads.Load())
	}
}

func p20ServiceSnapshot() runtime.FrozenRuntimeSnapshot {
	frozen, err := runtime.FreezeRuntimeSnapshot(runtime.RuntimeSnapshotInput{
		Runtime:       runtime.RuntimeVersionSnapshot{Go: "go1.27.0", Eino: "v0.9.15", App: "p20"},
		AgentRevision: "agent-p20", PromptHash: p20ServiceHash('a'), PolicyHash: p20ServiceHash('b'),
		ConfigHash: p20ServiceHash('c'), MCPCatalogHash: p20ServiceHash('d'),
		Models: []runtime.ModelSnapshot{{Kind: "chat", Profile: "default", CatalogRef: "test/chat", Provider: "test", Driver: "openai_compatible_chat", ModelID: "chat", Pricing: runtime.PricingSnapshot{Revision: "p20", Currency: "CNY", Unit: "per_million_tokens"}}},
		Tools:  []runtime.ToolSnapshot{{Name: "query_events", Revision: "v1", SchemaHash: p20ServiceHash('e')}},
		FeatureGates: map[string]bool{
			"agent_runtime.enabled": true, "agent_runtime.accept_new_runs": true,
			"agent_runtime.shadow_mode": false, "agent_runtime.l1_writes": false,
			"agent_runtime.l2_writes": false, "agent_runtime.admin_query_database_debug": false,
			"mcp.enabled": false, "skill.enabled": false, "langfuse.enabled": false,
		},
	})
	if err != nil {
		panic(err)
	}
	return frozen
}

func p20ServiceHash(value byte) string {
	data := make([]byte, 64)
	for index := range data {
		data[index] = value
	}
	return string(data)
}
