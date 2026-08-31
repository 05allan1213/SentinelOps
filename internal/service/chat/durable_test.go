package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestCreateRunCallsDurablePrimitiveOnceAndNeverExecutesAgent(t *testing.T) {
	var creates atomic.Int32
	var captured workflow.CreateRunInput
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      fixture20ServiceSnapshot(),
		CreateRun: func(_ context.Context, input workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			captured = input
			return &mysql.WorkflowRun{ID: "run-phase20", Status: workflow.RunStatusPending}, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.CreateRun(context.Background(), CreateDurableRunRequest{
		SessionID: "session-phase20", Query: "durable security plan", Agent: DurableAgentPlan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "run-phase20" || creates.Load() != 1 {
		t.Fatalf("run=%+v creates=%d, want one durable create", run, creates.Load())
	}
	var limits workflow.BaseBudgetLimits
	if err := json.Unmarshal(captured.BudgetLimitsJSON, &limits); err != nil {
		t.Fatalf("decode budget limits: %v", err)
	}
	if limits.MaxModelCalls <= 0 || limits.MaxL0ToolCalls <= 0 || limits.MaxDurationMS <= 0 {
		t.Fatalf("budget limits=%+v, want all phase14 base limits positive", limits)
	}
}

func TestCreateRunRejectsNonL0AgentBeforeStore(t *testing.T) {
	var creates atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      fixture20ServiceSnapshot(),
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
		SessionID: "session-phase20", Query: "mutation", Agent: "ops_agent",
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
		Snapshot: fixture20ServiceSnapshot(),
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return nil, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateRun(context.Background(), CreateDurableRunRequest{SessionID: "session-phase20", Query: "closed"})
	if !errors.Is(err, ErrDurableRunGateClosed) || creates.Load() != 0 {
		t.Fatalf("gate error=%v creates=%d", err, creates.Load())
	}
}

func TestEffectiveGateCreateRunReloadsAndFreezesEachRequest(t *testing.T) {
	var loads atomic.Int32
	var creates atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		SnapshotLoader: func(context.Context) (runtime.FrozenRuntimeSnapshot, error) {
			return fixture20ServiceSnapshotWithAccept(loads.Add(1) == 1), nil
		},
		CreateRun: func(_ context.Context, input workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return &mysql.WorkflowRun{ID: input.ID}, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := CreateDurableRunRequest{SessionID: "session-phase42", Query: "reload gates"}
	if _, err := service.CreateRun(context.Background(), request); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := service.CreateRun(context.Background(), request); !errors.Is(err, ErrDurableRunGateClosed) {
		t.Fatalf("second create error=%v, want closed Gate", err)
	}
	if loads.Load() != 2 || creates.Load() != 1 {
		t.Fatalf("snapshot loads=%d creates=%d", loads.Load(), creates.Load())
	}
}

func TestSSEReconnectOnlyReadsEventsAndChecksOwnerScope(t *testing.T) {
	var reads atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      fixture20ServiceSnapshot(),
		ListEvents: func(_ context.Context, runID string, afterSeq int64) ([]workflow.StreamEvent, error) {
			reads.Add(1)
			if runID != "run-phase20" || afterSeq != 4 {
				t.Fatalf("ListEvents args=%q/%d", runID, afterSeq)
			}
			return []workflow.StreamEvent{{ID: 5, RunID: runID, Type: workflow.EventRunCompleted}}, nil
		},
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.Events(context.Background(), "run-phase20", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != 5 || reads.Load() != 1 {
		t.Fatalf("events=%+v reads=%d", events, reads.Load())
	}
}

func TestResumeRunValidatesOwnerSessionAndCursorWithoutCreating(t *testing.T) {
	var creates atomic.Int32
	var reads atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		AcceptNewRuns: true,
		Snapshot:      fixture20ServiceSnapshot(),
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return nil, nil
		},
		GetRun: func(_ context.Context, runID string) (*mysql.WorkflowRun, error) {
			reads.Add(1)
			if runID != "run-c07-owned" {
				t.Fatalf("GetRun runID=%q", runID)
			}
			return &mysql.WorkflowRun{ID: runID, UserID: "owner-c07", SessionID: "session-c07", Status: workflow.RunStatusRunning, RuntimeMode: workflow.RuntimeModeDurableV1}, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.ResumeRun(c07IdentityContext(), "run-c07-owned", "session-c07", 17)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "run-c07-owned" || run.SessionID != "session-c07" || reads.Load() != 1 || creates.Load() != 0 {
		t.Fatalf("run=%+v reads=%d creates=%d", run, reads.Load(), creates.Load())
	}
}

func TestResumeRunRejectsMissingForbiddenAndSessionMismatch(t *testing.T) {
	tests := []struct {
		name    string
		getRun  func(context.Context, string) (*mysql.WorkflowRun, error)
		session string
		want    error
	}{
		{name: "missing", getRun: func(context.Context, string) (*mysql.WorkflowRun, error) { return nil, gorm.ErrRecordNotFound }, session: "session-c07", want: ErrDurableRunNotFound},
		{name: "forbidden", getRun: func(context.Context, string) (*mysql.WorkflowRun, error) { return nil, policy.ErrForbidden }, session: "session-c07", want: ErrDurableRunForbidden},
		{name: "session mismatch", getRun: func(context.Context, string) (*mysql.WorkflowRun, error) {
			return &mysql.WorkflowRun{ID: "run-c07", UserID: "owner-c07", SessionID: "other-session", RuntimeMode: workflow.RuntimeModeDurableV1}, nil
		}, session: "session-c07", want: ErrDurableReconnectInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewDurableService(DurableServiceConfig{
				CreateRun:  func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
				GetRun:     test.getRun,
				ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.ResumeRun(c07IdentityContext(), "run-c07", test.session, 0); !errors.Is(err, test.want) {
				t.Fatalf("ResumeRun error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestResumeRunRejectsNegativeCursorBeforeStore(t *testing.T) {
	var reads atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
		GetRun: func(context.Context, string) (*mysql.WorkflowRun, error) {
			reads.Add(1)
			return nil, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResumeRun(c07IdentityContext(), "run-c07", "session-c07", -1); !errors.Is(err, ErrDurableReconnectInvalid) {
		t.Fatalf("ResumeRun error=%v, want invalid reconnect", err)
	}
	if reads.Load() != 0 {
		t.Fatalf("negative cursor reached Store %d times", reads.Load())
	}
}

func TestTerminalResumeRunIsReadOnly(t *testing.T) {
	var creates atomic.Int32
	service, err := NewDurableService(DurableServiceConfig{
		CreateRun: func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) {
			creates.Add(1)
			return nil, nil
		},
		GetRun: func(context.Context, string) (*mysql.WorkflowRun, error) {
			return &mysql.WorkflowRun{ID: "run-c07-terminal", UserID: "owner-c07", SessionID: "session-c07", Status: workflow.RunStatusSucceeded, RuntimeMode: workflow.RuntimeModeDurableV1}, nil
		},
		ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.ResumeRun(c07IdentityContext(), "run-c07-terminal", "session-c07", 9)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != workflow.RunStatusSucceeded || creates.Load() != 0 {
		t.Fatalf("run=%+v creates=%d", run, creates.Load())
	}
}

func c07IdentityContext() context.Context {
	return policy.WithIdentity(context.Background(), policy.Identity{UserID: "owner-c07", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "owner-c07"}})
}

func TestResumeRunRechecksReturnedOwnerAndDurableMode(t *testing.T) {
	tests := []struct {
		name string
		run  *mysql.WorkflowRun
		want error
	}{
		{name: "owner mismatch", run: &mysql.WorkflowRun{ID: "run-c07", UserID: "other-owner", SessionID: "session-c07", RuntimeMode: workflow.RuntimeModeDurableV1}, want: ErrDurableRunForbidden},
		{name: "legacy run", run: &mysql.WorkflowRun{ID: "run-c07", UserID: "owner-c07", SessionID: "session-c07", RuntimeMode: workflow.RuntimeModeLegacy}, want: ErrDurableRunNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewDurableService(DurableServiceConfig{
				CreateRun:  func(context.Context, workflow.CreateRunInput) (*mysql.WorkflowRun, error) { return nil, nil },
				GetRun:     func(context.Context, string) (*mysql.WorkflowRun, error) { return test.run, nil },
				ListEvents: func(context.Context, string, int64) ([]workflow.StreamEvent, error) { return nil, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.ResumeRun(c07IdentityContext(), "run-c07", "session-c07", 0); !errors.Is(err, test.want) {
				t.Fatalf("ResumeRun error=%v, want %v", err, test.want)
			}
		})
	}
}

func fixture20ServiceSnapshot() runtime.FrozenRuntimeSnapshot {
	return fixture20ServiceSnapshotWithAccept(true)
}

func fixture20ServiceSnapshotWithAccept(acceptNewRuns bool) runtime.FrozenRuntimeSnapshot {
	frozen, err := runtime.FreezeRuntimeSnapshot(runtime.RuntimeSnapshotInput{
		Runtime:       runtime.RuntimeVersionSnapshot{Go: "go1.27.0", Eino: "v0.9.15", App: "phase20"},
		AgentRevision: "agent-phase20", PromptHash: fixture20ServiceHash('a'), PolicyHash: fixture20ServiceHash('b'),
		ConfigHash: fixture20ServiceHash('c'), MCPCatalogHash: fixture20ServiceHash('d'),
		Models: []runtime.ModelSnapshot{{Kind: "chat", Profile: "default", CatalogRef: "test/chat", Provider: "test", Driver: "openai_compatible_chat", ModelID: "chat", Pricing: runtime.PricingSnapshot{Revision: "phase20", Currency: "CNY", Unit: "per_million_tokens"}}},
		Tools:  []runtime.ToolSnapshot{{Name: "query_events", Revision: "v1", SchemaHash: fixture20ServiceHash('e')}},
		FeatureGates: map[string]bool{
			"agent_runtime.enabled": true, "agent_runtime.accept_new_runs": acceptNewRuns,
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

func fixture20ServiceHash(value byte) string {
	data := make([]byte, 64)
	for index := range data {
		data[index] = value
	}
	return string(data)
}
