package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestDurableSnapshotContainsCatalogToolsAndFrozenWriteGates(t *testing.T) {
	config := &appconfig.Config{
		AgentRuntime: appconfig.AgentRuntime{Enabled: true, AcceptNewRuns: true},
		Providers: map[string]appconfig.Provider{
			"test": {Endpoints: map[string]string{appconfig.DriverOpenAICompatibleChat: "https://example.invalid/v1"}},
		},
		ModelCatalog: map[string]appconfig.Model{
			"test/chat": {
				ModelID: "test-chat", Driver: appconfig.DriverOpenAICompatibleChat,
				Pricing: appconfig.Pricing{Revision: "phase20", Currency: "CNY", Unit: "per_million_tokens", Input: 1, Output: 2},
			},
		},
		Routing: appconfig.Routing{Chat: map[string]appconfig.ChatRoute{"default": {Candidates: []appconfig.Route{{Model: "test/chat"}}}}},
	}
	frozen, err := BuildDurableRuntimeSnapshot(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(frozen.Tools()) == 0 || !frozen.document.FeatureGates["agent_runtime.accept_new_runs"] ||
		frozen.document.FeatureGates["agent_runtime.l1_writes"] || frozen.document.FeatureGates["agent_runtime.l2_writes"] {
		t.Fatalf("durable snapshot tools=%+v gates=%+v", frozen.Tools(), frozen.document.FeatureGates)
	}
	mutationSeen := false
	for _, tool := range frozen.Tools() {
		entry, err := policy.LookupCatalog(tool.Name)
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name == "query_database" {
			t.Fatalf("durable snapshot exposed admin-only Tool %+v", tool)
		}
		if entry.Risk != policy.RiskL0 {
			mutationSeen = true
		}
	}
	if !mutationSeen {
		t.Fatal("durable snapshot does not contain the phase26 Mutation inventory")
	}
}

func TestStaticMutationCapsRequireExplicitConfig(t *testing.T) {
	config := &appconfig.Config{
		App:          appconfig.App{Environment: "test"},
		AgentRuntime: appconfig.AgentRuntime{Enabled: true, AcceptNewRuns: true},
		Providers: map[string]appconfig.Provider{
			"test": {Endpoints: map[string]string{appconfig.DriverOpenAICompatibleChat: "https://example.invalid/v1"}},
		},
		ModelCatalog: map[string]appconfig.Model{
			"test/chat": {
				ModelID: "test-chat", Driver: appconfig.DriverOpenAICompatibleChat,
				Pricing: appconfig.Pricing{Revision: "phase38", Currency: "CNY", Unit: "per_million_tokens", Input: 1, Output: 2},
			},
		},
		Routing: appconfig.Routing{Chat: map[string]appconfig.ChatRoute{"default": {Candidates: []appconfig.Route{{Model: "test/chat"}}}}},
	}
	closed, err := BuildDurableRuntimeSnapshot(config)
	if err != nil {
		t.Fatal(err)
	}
	if closed.FeatureGate("agent_runtime.l1_writes") || closed.FeatureGate("agent_runtime.l2_writes") {
		t.Fatalf("test gates opened without explicit opt-in: %+v", closed.document.FeatureGates)
	}
	config.AgentRuntime.L1Writes = true
	config.AgentRuntime.L2Writes = true
	open, err := BuildDurableRuntimeSnapshot(config)
	if err != nil {
		t.Fatal(err)
	}
	if !open.FeatureGate("agent_runtime.l1_writes") || !open.FeatureGate("agent_runtime.l2_writes") {
		t.Fatalf("explicit static caps did not open gates: %+v", open.document.FeatureGates)
	}
}

func TestWorkerRunsAfterAPIContextIsGone(t *testing.T) {
	var executions, completions atomic.Int32
	claimed := &workflow.ClaimedRun{Run: mysql.WorkflowRun{
		ID: "run-worker", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 3,
	}, Token: workflow.LeaseToken{RunID: "run-worker", Owner: "worker-phase20", Generation: 1}}
	worker, err := NewWorker(nil, WorkerConfig{
		Owner:          "worker-phase20",
		LeaseDuration:  time.Minute,
		MinPollBackoff: time.Millisecond,
		MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			return claimed, true, nil
		},
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			executions.Add(1)
			return RunExecutionResult{OutputPayload: `{"answer":"done"}`, RevisionStateJSON: []byte(`{"history":[]}`)}, nil
		},
		Complete: func(context.Context, workflow.CompleteRunInput) error {
			completions.Add(1)
			return nil
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if requestCtx.Err() == nil {
		t.Fatal("request context should be canceled")
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 1 || completions.Load() != 1 {
		t.Fatalf("executions=%d completions=%d, want independent Worker execution", executions.Load(), completions.Load())
	}
}

func TestWorkerRetryableFailureUsesBoundedBackoffWithoutResettingAttempt(t *testing.T) {
	claimed := &workflow.ClaimedRun{Run: mysql.WorkflowRun{
		ID: "run-retry", Status: workflow.RunStatusRunning, Attempt: 2, MaxAttempts: 3,
	}, Token: workflow.LeaseToken{RunID: "run-retry", Owner: "worker-phase20", Generation: 4}}
	var transition workflow.RunTransition
	worker, err := NewWorker(nil, WorkerConfig{
		Owner:          "worker-phase20",
		LeaseDuration:  time.Minute,
		MinPollBackoff: time.Millisecond,
		MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext:      func(context.Context) (*workflow.ClaimedRun, bool, error) { return claimed, true, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, Retryable(errors.New("provider timeout"))
		},
		Transition: func(_ context.Context, got workflow.RunTransition) error {
			transition = got
			return nil
		},
		Complete: func(context.Context, workflow.CompleteRunInput) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transition.TargetStatus != workflow.RunStatusRetryableFailed || transition.Lease != claimed.Token || transition.Event.Type != workflow.EventRunFailed {
		t.Fatalf("retry transition=%+v, want fenced retryable_failed", transition)
	}
	if claimed.Run.Attempt != 2 || claimed.Run.LeaseGeneration != 0 && claimed.Run.LeaseGeneration != 4 {
		t.Fatalf("Worker reset attempt/generation: %+v", claimed.Run)
	}
}

func TestWorkerFatalFailureUsesOnlyCompletePrimitive(t *testing.T) {
	claimed := &workflow.ClaimedRun{Run: mysql.WorkflowRun{
		ID: "run-fatal", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 1,
	}, Token: workflow.LeaseToken{RunID: "run-fatal", Owner: "worker-phase20", Generation: 1}}
	var completions atomic.Int32
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase20", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return claimed, true, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, errors.New("invalid input")
		},
		Complete: func(_ context.Context, input workflow.CompleteRunInput) error {
			completions.Add(1)
			if input.TargetStatus != workflow.RunStatusFailed || input.Lease != claimed.Token {
				t.Fatalf("completion input=%+v", input)
			}
			return nil
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if completions.Load() != 1 {
		t.Fatalf("completion calls=%d, want one", completions.Load())
	}
}

func TestWorkerCanceledFailureUsesOnlyCompletePrimitive(t *testing.T) {
	claimed := &workflow.ClaimedRun{Run: mysql.WorkflowRun{
		ID: "run-canceled", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 1,
	}, Token: workflow.LeaseToken{RunID: "run-canceled", Owner: "worker-phase20", Generation: 1}}
	var transitions, completions atomic.Int32
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase20", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return claimed, true, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, context.Canceled
		},
		Transition: func(context.Context, workflow.RunTransition) error {
			transitions.Add(1)
			return nil
		},
		Complete: func(_ context.Context, input workflow.CompleteRunInput) error {
			completions.Add(1)
			if input.TargetStatus != workflow.RunStatusCanceled || input.Lease != claimed.Token {
				t.Fatalf("completion input=%+v", input)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transitions.Load() != 0 || completions.Load() != 1 {
		t.Fatalf("canceled writes transition=%d completion=%d, want only completion", transitions.Load(), completions.Load())
	}
}

func TestWorkerParkedFailureUsesFencedTransition(t *testing.T) {
	claimed := &workflow.ClaimedRun{Run: mysql.WorkflowRun{
		ID: "run-parked", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 3,
	}, Token: workflow.LeaseToken{RunID: "run-parked", Owner: "worker-phase20", Generation: 1}}
	var transition workflow.RunTransition
	var completions atomic.Int32
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase20", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return claimed, true, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, Parked(errors.New("checkpoint dependency is corrupt"), workflow.ParkReasonCheckpointCorrupt)
		},
		Transition: func(_ context.Context, input workflow.RunTransition) error {
			transition = input
			return nil
		},
		Complete: func(context.Context, workflow.CompleteRunInput) error {
			completions.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transition.TargetStatus != workflow.RunStatusParked || transition.ParkReason != workflow.ParkReasonCheckpointCorrupt ||
		transition.Lease != claimed.Token || completions.Load() != 0 {
		t.Fatalf("parked transition=%+v completions=%d", transition, completions.Load())
	}
}

func TestWorkerLeaseLossCancelsExecutionAndSkipsTruthWrites(t *testing.T) {
	claimed := &workflow.ClaimedRun{Run: mysql.WorkflowRun{
		ID: "run-lease-loss", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 3,
	}, Token: workflow.LeaseToken{RunID: "run-lease-loss", Owner: "worker-phase20", Generation: 2}}
	var transitions, completions atomic.Int32
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase20", LeaseDuration: 3 * time.Millisecond,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return claimed, true, nil },
		Heartbeat: func(context.Context, workflow.LeaseToken) error {
			return workflow.ErrLeaseLost
		},
		Execute: func(ctx context.Context, _ *workflow.ClaimedRun) (RunExecutionResult, error) {
			<-ctx.Done()
			if !errors.Is(context.Cause(ctx), workflow.ErrLeaseLost) {
				t.Fatalf("execution cancel cause=%v, want ErrLeaseLost", context.Cause(ctx))
			}
			return RunExecutionResult{}, ctx.Err()
		},
		Transition: func(context.Context, workflow.RunTransition) error {
			transitions.Add(1)
			return nil
		},
		Complete: func(context.Context, workflow.CompleteRunInput) error {
			completions.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOnce(context.Background()); !worked || !errors.Is(err, workflow.ErrLeaseLost) {
		t.Fatalf("RunOnce worked=%t err=%v, want fenced lease loss", worked, err)
	}
	if transitions.Load() != 0 || completions.Load() != 0 {
		t.Fatalf("lease-lost writes transition=%d completion=%d, want zero", transitions.Load(), completions.Load())
	}
}

func TestWorkerPollLoopContinuesAfterLeaseLoss(t *testing.T) {
	claimed := &workflow.ClaimedRun{Run: mysql.WorkflowRun{
		ID: "run-loop-lease-loss", Status: workflow.RunStatusRunning, Attempt: 1, MaxAttempts: 3,
	}, Token: workflow.LeaseToken{RunID: "run-loop-lease-loss", Owner: "worker-phase20", Generation: 2}}
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	var claims atomic.Int32
	worker, err := NewWorker(nil, WorkerConfig{
		Owner: "worker-phase20", LeaseDuration: 3 * time.Millisecond,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) {
			if claims.Add(1) == 1 {
				return claimed, true, nil
			}
			cancel()
			return nil, false, nil
		},
		Heartbeat: func(context.Context, workflow.LeaseToken) error { return workflow.ErrLeaseLost },
		Execute: func(ctx context.Context, _ *workflow.ClaimedRun) (RunExecutionResult, error) {
			<-ctx.Done()
			return RunExecutionResult{}, ctx.Err()
		},
		Transition: func(context.Context, workflow.RunTransition) error { return nil },
		Complete:   func(context.Context, workflow.CompleteRunInput) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(lifecycle); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error=%v, want caller cancellation after lease-loss recovery", err)
	}
	if claims.Load() < 2 {
		t.Fatalf("claims=%d, want poll loop to continue after lease loss", claims.Load())
	}
}

func TestAPIWorkerFocusedIntegration(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase20_api_worker")
	store := workflow.NewGORMStore(db)
	frozen, err := FreezeRuntimeSnapshot(fixture11SnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "user-phase20", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "user-phase20"},
	})
	immutableInput := []byte(`{"agent":"event_analysis_agent","query":"focused L0 replay"}`)
	run, err := store.CreateRunWithSessionLock(ctx, workflow.CreateRunInput{
		ID: "run-phase20-focused", WorkflowKey: "chat.intent", SessionID: "session-phase20-focused",
		QueryText: "focused L0 replay", ImmutableInputJSON: immutableInput,
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: []byte(`{"max_tokens":4096}`),
		DeadlineAt:   time.Now().Add(time.Minute),
		CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	modelCalls := atomic.Int32{}
	executor, err := NewDurableExecutor(store, func(context.Context, string) (adk.Agent, error) {
		return &fixture20FinalAgent{calls: &modelCalls}, nil
	}, frozen.CompatibilityHash())
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-phase20-focused", LeaseDuration: 30 * time.Second,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		Execute: executor.ExecuteClaimedRun,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("RunOnce worked=%t err=%v", worked, err)
	}
	var completed mysql.WorkflowRun
	if err := db.Where("id = ?", run.ID).First(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed.Status != workflow.RunStatusSucceeded || completed.ActiveSessionKey != nil || modelCalls.Load() != 1 {
		t.Fatalf("completed=%+v model_calls=%d", completed, modelCalls.Load())
	}
	events, err := store.ListEventsAfter(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var replayedEvent, agentEvent, completedEvent bool
	for _, event := range events {
		switch event.Type {
		case workflow.EventRunReplayed:
			replayedEvent = true
		case workflow.EventAgentPlan:
			agentEvent = true
		case workflow.EventRunCompleted:
			completedEvent = true
		}
	}
	if !replayedEvent || !agentEvent || !completedEvent {
		t.Fatalf("event types=%+v, want Replay, Agent projection and terminal completion", events)
	}
	beforeReplay := modelCalls.Load()
	replayed, err := store.ListEventsAfter(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(events) || modelCalls.Load() != beforeReplay {
		t.Fatalf("SSE replay events=%d/%d model_calls=%d/%d", len(replayed), len(events), modelCalls.Load(), beforeReplay)
	}
	otherUser := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "other-user-phase20", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "other-user-phase20"},
	})
	if _, err := store.ListEventsAfter(otherUser, run.ID, 0); !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("cross-owner SSE error=%v, want forbidden", err)
	}
	if _, err := store.CreateRunWithSessionLock(ctx, workflow.CreateRunInput{
		ID: "run-phase20-after-terminal", WorkflowKey: "chat.intent", SessionID: run.SessionID,
		QueryText: "next", ImmutableInputJSON: []byte(`{"agent":"event_analysis_agent","query":"next"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: []byte(`{}`), DeadlineAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("terminal completion did not release Session: %v", err)
	}
	var revisionCount int64
	if err := db.Model(&mysql.SessionStateRevision{}).Where("session_id = ?", run.SessionID).Count(&revisionCount).Error; err != nil || revisionCount != 2 {
		t.Fatalf("revision count=%d err=%v", revisionCount, err)
	}
}

func TestWorkerRetryableBackoffPersistsAndReclaims(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase20_retry_backoff")
	store := workflow.NewGORMStore(db)
	frozen, err := FreezeRuntimeSnapshot(fixture11SnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "user-phase20-retry", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "user-phase20-retry"},
	})
	run, err := store.CreateRunWithSessionLock(ctx, workflow.CreateRunInput{
		ID: "run-phase20-retry", WorkflowKey: "chat.intent", SessionID: "session-phase20-retry",
		QueryText: "retry", ImmutableInputJSON: []byte(`{"agent":"event_analysis_agent","query":"retry"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: []byte(`{"max_tokens":4096}`),
		DeadlineAt: time.Now().Add(time.Minute), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	var executions atomic.Int32
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-phase20-retry", LeaseDuration: time.Minute,
		MinPollBackoff: time.Second, MaxPollBackoff: 4 * time.Second,
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			if executions.Add(1) == 1 {
				return RunExecutionResult{}, Retryable(errors.New("temporary provider failure"))
			}
			return RunExecutionResult{
				OutputPayload:     `{"answer":"recovered"}`,
				RevisionStateJSON: []byte(`{"schema":"sentinelops/session-state/v1","revision":1,"history":[]}`),
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("first RunOnce worked=%t err=%v", worked, err)
	}
	var retrying mysql.WorkflowRun
	if err := db.Where("id = ?", run.ID).First(&retrying).Error; err != nil {
		t.Fatal(err)
	}
	if retrying.Status != workflow.RunStatusRetryableFailed || retrying.Attempt != 1 ||
		retrying.LeaseOwner != nil || retrying.LeaseUntil != nil || retrying.HeartbeatAt != nil ||
		!retrying.AvailableAt.After(time.Now()) {
		t.Fatalf("retrying Run=%+v, want released lease and future backoff", retrying)
	}
	if worked, err := worker.RunOnce(context.Background()); err != nil || worked {
		t.Fatalf("backoff RunOnce worked=%t err=%v, want no eligible Run", worked, err)
	}
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).
		Update("available_at", gorm.Expr("DATE_SUB(CURRENT_TIMESTAMP(3), INTERVAL 1 SECOND)")).Error; err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("retry-ready RunOnce worked=%t err=%v", worked, err)
	}
	var completed mysql.WorkflowRun
	if err := db.Where("id = ?", run.ID).First(&completed).Error; err != nil {
		t.Fatal(err)
	}
	if completed.Status != workflow.RunStatusSucceeded || completed.Attempt != 2 || executions.Load() != 2 {
		t.Fatalf("completed=%+v executions=%d, want second-attempt success", completed, executions.Load())
	}
	if completed.RuntimeCompatibilityHash == nil || run.RuntimeCompatibilityHash == nil ||
		*completed.RuntimeCompatibilityHash != *run.RuntimeCompatibilityHash || completed.BudgetLimitsJSON == nil ||
		run.BudgetLimitsJSON == nil || !jsonEqual([]byte(*completed.BudgetLimitsJSON), []byte(*run.BudgetLimitsJSON)) {
		t.Fatal("retry changed frozen runtime snapshot or budget limits")
	}
}

func TestWorkerSafePointCancelCommitsCheckpointForHandoff(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase20_safe_point")
	store := workflow.NewGORMStore(db)
	frozen, err := FreezeRuntimeSnapshot(fixture11SnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	ownerCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "user-phase20-drain", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "user-phase20-drain"},
	})
	run, err := store.CreateRunWithSessionLock(ownerCtx, workflow.CreateRunInput{
		ID: "run-phase20-drain", WorkflowKey: "chat.intent", SessionID: "session-phase20-drain",
		QueryText: "drain", ImmutableInputJSON: []byte(`{"agent":"event_analysis_agent","query":"drain"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: []byte(`{"max_tokens":4096}`),
		DeadlineAt: time.Now().Add(time.Minute), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	executor, err := NewDurableExecutor(store, func(context.Context, string) (adk.Agent, error) {
		return &fixture20SafePointAgent{started: started, release: release}, nil
	}, frozen.CompatibilityHash())
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-phase20-drain", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		Execute: executor.ExecuteClaimedRun,
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	type workerResult struct {
		worked bool
		err    error
	}
	result := make(chan workerResult, 1)
	go func() {
		worked, runErr := worker.RunOnce(lifecycle)
		result <- workerResult{worked: worked, err: runErr}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("durable Agent did not start")
	}
	cancel()
	time.Sleep(20 * time.Millisecond)
	close(release)
	var got workerResult
	select {
	case workerOutcome := <-result:
		got = workerOutcome
	case <-time.After(2 * time.Second):
		t.Fatal("safe-point Worker did not hand off")
	}
	var stored mysql.WorkflowRun
	if err := db.Where("id = ?", run.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != workflow.RunStatusRunning || stored.ActiveSessionKey == nil || stored.LeaseOwner == nil {
		t.Fatalf("safe-point Run=%+v, want resumable running lease with Session lock", stored)
	}
	checkpointID, err := workflow.EinoCheckpointID(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoints int64
	if err := db.Model(&mysql.WorkflowCheckpoint{}).
		Where("run_id = ? AND eino_checkpoint_id = ? AND checkpoint_blob IS NOT NULL", run.ID, checkpointID).
		Count(&checkpoints).Error; err != nil || checkpoints != 1 {
		t.Fatalf("checkpoint count=%d query_err=%v worker=%t/%v, want one fenced Eino checkpoint", checkpoints, err, got.worked, got.err)
	}
	if !got.worked || got.err != nil {
		t.Fatalf("safe-point RunOnce worked=%t err=%v", got.worked, got.err)
	}
}

func TestSessionRunParkedKeepsExclusiveLock(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase20_parked_lock")
	store := workflow.NewGORMStore(db)
	frozen, err := FreezeRuntimeSnapshot(fixture11SnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "user-phase20-parked", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "user-phase20-parked"},
	})
	input := workflow.CreateRunInput{
		ID: "run-phase20-parked", WorkflowKey: "chat.intent", SessionID: "session-phase20-parked",
		QueryText: "park", ImmutableInputJSON: []byte(`{"agent":"event_analysis_agent","query":"park"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: []byte(`{}`), DeadlineAt: time.Now().Add(time.Minute),
	}
	if _, err := store.CreateRunWithSessionLock(ctx, input); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextRun(ctx, workflow.ClaimInput{Owner: "worker-phase20-parked", LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("claim ok=%t err=%v", ok, err)
	}
	if err := store.TransitionRunWithEvent(ctx, workflow.RunTransition{
		RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: workflow.RunStatusParked,
		ParkReason: workflow.ParkReasonCheckpointCorrupt, Lease: claimed.Token,
		Event: workflow.WorkflowEventInput{Type: workflow.EventRunParked},
	}); err != nil {
		t.Fatal(err)
	}
	input.ID = "run-phase20-conflict"
	if _, err := store.CreateRunWithSessionLock(ctx, input); !errors.Is(err, workflow.ErrSessionRunActive) {
		t.Fatalf("second parked Session Run error=%v, want ErrSessionRunActive", err)
	}
}

type fixture20FinalAgent struct{ calls *atomic.Int32 }

func (*fixture20FinalAgent) Name(context.Context) string        { return "FinalL0Agent" }
func (*fixture20FinalAgent) Description(context.Context) string { return "phase20 L0 scripted Agent" }
func (a *fixture20FinalAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	a.calls.Add(1)
	iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(&adk.AgentEvent{AgentName: "FinalL0Agent", Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{Message: schema.AssistantMessage("focused answer", nil)}}})
	generator.Close()
	return iter
}

type fixture20SafePointAgent struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (*fixture20SafePointAgent) Name(context.Context) string { return "SafePointAgent" }
func (*fixture20SafePointAgent) Description(context.Context) string {
	return "phase20 safe-point Agent"
}
func (a *fixture20SafePointAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		a.started <- struct{}{}
		<-a.release
		generator.Send(adk.StatefulInterrupt(ctx, "phase20-safe-point", fixture12RegisteredRecoveryState{Marker: "phase20"}))
		generator.Close()
	}()
	return iter
}
