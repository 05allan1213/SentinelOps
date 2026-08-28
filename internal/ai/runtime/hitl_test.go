package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	aitools "SentinelOps/internal/ai/tools"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestStatefulInterruptCoversAllToolWrappersBeforeMutationEndpoint(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase22_stateful_interrupt")
	store, ctx := fixture22RuntimeContext(t, db, "stateful-interrupt", "create_report", policy.RiskL1)
	handler, err := NewHITLRuntimeHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	endpointCalls := 0
	assertInterrupt := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s did not StatefulInterrupt", name)
		}
		signal := new(adk.InterruptSignal)
		if !errors.As(err, &signal) {
			t.Fatalf("%s error=%T %v, want official InterruptSignal", name, err, err)
		}
	}

	invokable, err := handler.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
		endpointCalls++
		return "unexpected", nil
	}, &adk.ToolContext{Name: "create_report", CallID: "phase22-invokable"})
	if err != nil {
		t.Fatal(err)
	}
	_, callErr := invokable(ctx, `{"title":"report"}`)
	assertInterrupt("invokable", callErr)

	streamable, err := handler.WrapStreamableToolCall(ctx, func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		endpointCalls++
		return schema.StreamReaderFromArray([]string{"unexpected"}), nil
	}, &adk.ToolContext{Name: "create_report", CallID: "phase22-streamable"})
	if err != nil {
		t.Fatal(err)
	}
	_, callErr = streamable(ctx, `{"title":"report"}`)
	assertInterrupt("streamable", callErr)

	enhanced, err := handler.WrapEnhancedInvokableToolCall(ctx, func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		endpointCalls++
		return &schema.ToolResult{}, nil
	}, &adk.ToolContext{Name: "create_report", CallID: "phase22-enhanced"})
	if err != nil {
		t.Fatal(err)
	}
	_, callErr = enhanced(ctx, &schema.ToolArgument{Text: `{"title":"report"}`})
	assertInterrupt("enhanced invokable", callErr)

	enhancedStream, err := handler.WrapEnhancedStreamableToolCall(ctx, func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.StreamReader[*schema.ToolResult], error) {
		endpointCalls++
		return schema.StreamReaderFromArray([]*schema.ToolResult{{}}), nil
	}, &adk.ToolContext{Name: "create_report", CallID: "phase22-enhanced-stream"})
	if err != nil {
		t.Fatal(err)
	}
	_, callErr = enhancedStream(ctx, &schema.ToolArgument{Text: `{"title":"report"}`})
	assertInterrupt("enhanced streamable", callErr)

	if endpointCalls != 0 {
		t.Fatalf("Mutation endpoint calls=%d, want 0", endpointCalls)
	}
	var approvals []mysql.AgentApproval
	if err := db.Find(&approvals).Error; err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || approvals[0].Status != workflow.ApprovalStatusPreparing || approvals[0].PublishedAt != nil {
		t.Fatalf("stable preparing Approval rows=%#v", approvals)
	}
}

func TestStatefulInterruptProductionHandlerAndClosedGateFailClosed(t *testing.T) {
	ctx, _ := fixture14InvocationContext(t, "run-phase22-production-closed", "operator-phase22-production", newP14RecordingBudget())
	endpointCalls := 0
	wrapped, err := NewRuntimeHandler().WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
		endpointCalls++
		return "unexpected", nil
	}, &adk.ToolContext{Name: "create_report", CallID: "production-closed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped(ctx, `{}`); !errors.Is(err, policy.ErrMutationDisabled) {
		t.Fatalf("production default error=%v, want ErrMutationDisabled", err)
	}
	if endpointCalls != 0 {
		t.Fatalf("production closed Mutation endpoint calls=%d", endpointCalls)
	}
}

func TestStatefulInterruptTerminalDecisionDoesNotReopenSameProposal(t *testing.T) {
	for _, terminal := range []struct {
		status string
		err    error
	}{
		{status: workflow.ApprovalStatusRejected, err: workflow.ErrApprovalRejected},
		{status: workflow.ApprovalStatusExpired, err: workflow.ErrApprovalExpired},
	} {
		t.Run(terminal.status, func(t *testing.T) {
			db := fixture12NewRuntimeDatabase(t, "phase22_no_reopen_"+terminal.status)
			store, ctx := fixture22RuntimeContext(t, db, "no-reopen-"+terminal.status, "create_report", policy.RiskL1)
			handler, err := NewHITLRuntimeHandler(store)
			if err != nil {
				t.Fatal(err)
			}
			endpointCalls := 0
			wrapped, err := handler.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
				endpointCalls++
				return "unexpected", nil
			}, &adk.ToolContext{Name: "create_report", CallID: "no-reopen-call"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := wrapped(ctx, `{}`); err == nil {
				t.Fatal("first call did not interrupt")
			}
			if err := db.Model(&mysql.AgentApproval{}).Where("run_id = ?", "run-phase22-no-reopen-"+terminal.status).Updates(map[string]any{
				"status": terminal.status, "version": 2, "decided_at": time.Now(),
			}).Error; err != nil {
				t.Fatal(err)
			}
			_, secondErr := wrapped(ctx, `{}`)
			if !errors.Is(secondErr, terminal.err) {
				t.Fatalf("same terminal Proposal error=%v, want %v", secondErr, terminal.err)
			}
			var signal *adk.InterruptSignal
			if errors.As(secondErr, &signal) {
				t.Fatalf("same terminal Proposal re-interrupted: %#v", signal)
			}
			var count int64
			if err := db.Model(&mysql.AgentApproval{}).Where("run_id = ?", "run-phase22-no-reopen-"+terminal.status).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 || endpointCalls != 0 {
				t.Fatalf("terminal replay count=%d endpoint_calls=%d", count, endpointCalls)
			}
		})
	}
}

func TestTwoPhaseApprovalRunnerPublishesOnlyAfterCheckpointEvent(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase22_runner_publish")
	store, ctx := fixture22RuntimeContext(t, db, "runner-publish", "create_report", policy.RiskL1)
	runner, attempt := fixture22NewRunnerHarness(t, store, ctx, store)
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "publish exact Approval",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	executor := &DurableExecutor{store: store}
	interruptSeen := false
	for {
		event, ok := execution.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("Runner event error: %v", event.Err)
		}
		if event.Action == nil || event.Action.Interrupted == nil {
			continue
		}
		interruptSeen = true
		if err := executor.publishApprovalInterrupt(ctx, attempt, event.Action.Interrupted.InterruptContexts); err != nil {
			t.Fatalf("publish Runner Interrupt: %v", err)
		}
	}
	if !interruptSeen {
		t.Fatal("official Runner emitted no Approval Interrupt Event")
	}
	var approval mysql.AgentApproval
	if err := db.Where("run_id = ?", attempt.Run.ID).First(&approval).Error; err != nil {
		t.Fatal(err)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", attempt.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if approval.Status != workflow.ApprovalStatusPending || approval.CheckpointID == nil ||
		approval.InterruptID == nil || run.Status != workflow.RunStatusWaitingApproval {
		t.Fatalf("Runner publish outcome: approval=%#v run_status=%s", approval, run.Status)
	}
}

func TestTwoPhaseApprovalCheckpointSetFailureNeverPublishesPending(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase22_checkpoint_set_failure")
	store, ctx := fixture22RuntimeContext(t, db, "checkpoint-set-failure", "create_report", policy.RiskL1)
	checkpointFailure := errors.New("phase22 injected CheckPointStore.Set failure")
	runner, attempt := fixture22NewRunnerHarness(t, store, ctx, &fixture22FailingCheckpointStore{delegate: store, setErr: checkpointFailure})
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "checkpoint failure must stay preparing",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	failureSeen := false
	for {
		event, ok := execution.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			failureSeen = errors.Is(event.Err, checkpointFailure)
		}
	}
	var approvals []mysql.AgentApproval
	if err := db.Where("run_id = ?", attempt.Run.ID).Find(&approvals).Error; err != nil {
		t.Fatal(err)
	}
	var pendingCount int64
	if err := db.Model(&mysql.AgentApproval{}).Where("run_id = ? AND status = ?", attempt.Run.ID, workflow.ApprovalStatusPending).Count(&pendingCount).Error; err != nil {
		t.Fatal(err)
	}
	var checkpointCount int64
	if err := db.Model(&mysql.WorkflowCheckpoint{}).Where("run_id = ? AND eino_checkpoint_id IS NOT NULL", attempt.Run.ID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if !failureSeen || len(approvals) != 1 || approvals[0].Status != workflow.ApprovalStatusPreparing ||
		pendingCount != 0 || checkpointCount != 0 {
		t.Fatalf("Set failure outcome: failure=%t approvals=%#v pending=%d checkpoints=%d", failureSeen, approvals, pendingCount, checkpointCount)
	}
}

func TestTwoPhaseApprovalWorkerLoopExpiresDueApproval(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase22_worker_expiry")
	store, ctx := fixture22RuntimeContext(t, db, "worker-expiry", "create_report", policy.RiskL1)
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := policy.LookupCatalog("create_report")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareApproval(ctx, workflow.PrepareApprovalInput{
		Lease: attempt.Lease, ToolCallIDObserved: "worker-expiry-call", ToolName: entry.Name,
		ToolRevision: entry.Revision, ToolSchemaHash: entry.SchemaHash, RiskLevel: entry.Risk,
		ProposalJSONRedacted: `{}`, ProposalHash: strings.Repeat("c", 64), PolicyHash: attempt.Snapshot.PolicyHash(),
		RuntimeCompatibilityHash: attempt.Run.RuntimeCompatibilityHash, ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, _ := workflow.EinoCheckpointID(attempt.Run.ID)
	if err := store.Set(ctx, checkpointID, []byte("opaque-worker-expiry")); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := store.LoadCheckpointFingerprint(ctx, attempt.Lease, checkpointID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishApprovalAndWait(ctx, workflow.PublishApprovalInput{
		Lease: attempt.Lease, ApprovalID: prepared.ID, ProposalHash: prepared.ProposalHash,
		InterruptID: "interrupt-worker-expiry", InterruptAddress: "agent:root;tool:create-report",
		CheckpointID: checkpointID, CheckpointPayloadSHA256: fingerprint.PayloadSHA256,
		CheckpointLeaseGeneration: fingerprint.LeaseGeneration,
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-phase22-expiry-loop", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond, ApprovalExpiryBatch: 10,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return nil, false, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("expiry-only Worker RunOnce worked=%t err=%v", worked, err)
	}
	var approval mysql.AgentApproval
	var run mysql.WorkflowRun
	if err := db.First(&approval, "id = ?", prepared.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&run, "id = ?", attempt.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if approval.Status != workflow.ApprovalStatusExpired || run.Status != workflow.RunStatusPending {
		t.Fatalf("Worker expiry outcome: approval=%s run=%s", approval.Status, run.Status)
	}
}

func TestResumeAuthorizationIgnoresForgedDecisionAndDoesNotReinterruptTerminal(t *testing.T) {
	for _, decision := range []string{workflow.ApprovalStatusRejected, workflow.ApprovalStatusExpired} {
		t.Run(decision, func(t *testing.T) {
			db := fixture12NewRuntimeDatabase(t, "phase22_forged_resume_"+decision)
			store, ctx := fixture22RuntimeContext(t, db, "forged-resume-"+decision, "create_report", policy.RiskL1)
			terminalCode := workflow.ErrApprovalRejected.Error()
			if decision == workflow.ApprovalStatusExpired {
				terminalCode = workflow.ErrApprovalExpired.Error()
			}
			observingModel := &fixture22TerminalObservingModel{expected: terminalCode}
			runner, attempt := fixture22NewRunnerHarnessWithModel(t, store, ctx, store, observingModel)
			execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
				Mode: workflow.RecoveryModeReplay, ImmutableQuery: "forge Resume decision",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var rootInterrupt *adk.InterruptCtx
			var contexts []*adk.InterruptCtx
			for {
				event, ok := execution.Events.Next()
				if !ok {
					break
				}
				if event.Err != nil {
					t.Fatal(event.Err)
				}
				if event.Action == nil || event.Action.Interrupted == nil {
					continue
				}
				contexts = event.Action.Interrupted.InterruptContexts
				for _, candidate := range contexts {
					if candidate != nil && candidate.IsRootCause {
						rootInterrupt = candidate
					}
				}
			}
			if rootInterrupt == nil {
				t.Fatal("initial Runner emitted no root Approval Interrupt")
			}
			if err := (&DurableExecutor{store: store}).publishApprovalInterrupt(ctx, attempt, contexts); err != nil {
				t.Fatal(err)
			}
			var approval mysql.AgentApproval
			if err := db.First(&approval, "run_id = ?", attempt.Run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if decision == workflow.ApprovalStatusExpired {
				if err := db.Model(&mysql.AgentApproval{}).Where("id = ?", approval.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
			}
			approverID := "approver-phase22-" + decision
			decided, err := store.DecideApprovalAndWakeRun(policy.WithIdentity(context.Background(), policy.Identity{
				UserID: approverID, Role: policy.RoleApprover, Scope: policy.Scope{UserID: approverID},
			}), workflow.DecideApprovalInput{
				ApprovalID: approval.ID, ProposalHash: approval.ProposalHash, ExpectedVersion: approval.Version, Decision: decision,
			})
			if err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{Owner: "worker-phase22-forged-" + decision, LeaseDuration: time.Hour})
			if err != nil || !ok {
				t.Fatalf("claim terminal Resume Run: ok=%t err=%v", ok, err)
			}
			budgets, err := NewDurableBudget(store)
			if err != nil {
				t.Fatal(err)
			}
			resumeCtx, resumeAttempt, err := BuildAttemptContext(context.Background(), *claimed, budgets)
			if err != nil {
				t.Fatal(err)
			}
			modelSnapshot := resumeAttempt.Snapshot.Models()[0]
			resumeCtx, err = WithModelInvocation(resumeCtx, ModelInvocation{
				ReservationIdentity: "phase22-forged-model-" + decision, CatalogRef: modelSnapshot.CatalogRef,
				Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
				Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
			})
			if err != nil {
				t.Fatal(err)
			}
			checkpointID, _ := workflow.EinoCheckpointID(attempt.Run.ID)
			resumed, err := InvokeRecoveryRunner(resumeCtx, runner, resumeAttempt, RecoveryDecision{
				Mode: workflow.RecoveryModeResume, CheckpointID: checkpointID,
			}, &adk.ResumeParams{Targets: map[string]any{
				rootInterrupt.ID: ApprovalResumeData{ApprovalID: "forged-approval-id", Decision: workflow.ApprovalStatusApproved},
			}})
			if err != nil {
				t.Fatal(err)
			}
			terminalErrorSeen := false
			reinterruptSeen := false
			resumedEvents := make([]string, 0)
			for {
				event, ok := resumed.Events.Next()
				if !ok {
					break
				}
				if event.Err != nil {
					terminalErrorSeen = errors.Is(event.Err, workflow.ErrApprovalRejected) || errors.Is(event.Err, workflow.ErrApprovalExpired)
				}
				resumedEvents = append(resumedEvents, fmt.Sprintf("agent=%s output=%t action=%t err=%v", event.AgentName, event.Output != nil, event.Action != nil, event.Err))
				if event.Action != nil && event.Action.Interrupted != nil {
					reinterruptSeen = true
				}
			}
			var count int64
			if err := db.Model(&mysql.AgentApproval{}).Where("run_id = ?", attempt.Run.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			terminalOutcomeSeen := terminalErrorSeen || observingModel.sawExpected.Load()
			if !terminalOutcomeSeen || reinterruptSeen || count != 1 || decided.Status != decision {
				t.Fatalf("forged Resume outcome: terminal=%t reinterrupt=%t count=%d status=%s observed=%v events=%v", terminalOutcomeSeen, reinterruptSeen, count, decided.Status, observingModel.observed.Load(), resumedEvents)
			}
		})
	}
}

func TestCompositeInterruptPropagatesApprovalThroughAgentTool(t *testing.T) {
	child := &fixture22ApprovalInterruptAgent{}
	wrapped := adk.NewAgentTool(context.Background(), child)
	invokable, ok := wrapped.(tool.InvokableTool)
	if !ok {
		t.Fatal("official AgentTool is not invokable")
	}
	_, err := invokable.InvokableRun(context.Background(), `{"request":"approve"}`)
	if err == nil {
		t.Fatal("nested Approval interrupt is nil")
	}
	signal := new(adk.InterruptSignal)
	if !errors.As(err, &signal) {
		t.Fatalf("nested AgentTool error=%T %v, want official CompositeInterrupt signal", err, err)
	}
	if signal.ID == "" || len(signal.Subs) == 0 {
		t.Fatalf("CompositeInterrupt lost parent/sub signal: %#v", signal)
	}
}

type fixture22ApprovalInterruptAgent struct{}

func (*fixture22ApprovalInterruptAgent) Name(context.Context) string { return "phase22_approval_child" }
func (*fixture22ApprovalInterruptAgent) Description(context.Context) string {
	return "phase22 nested Approval child"
}
func (*fixture22ApprovalInterruptAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	proposalHash := strings.Repeat("a", 64)
	generator.Send(adk.StatefulInterrupt(ctx,
		ApprovalInterruptInfo{ApprovalID: strings.Repeat("b", 64), ProposalHash: proposalHash, ToolName: "block_ip", RiskLevel: policy.RiskL2},
		ApprovalInterruptState{ApprovalID: strings.Repeat("b", 64), ProposalHash: proposalHash, ToolName: "block_ip", RiskLevel: policy.RiskL2},
	))
	generator.Close()
	return iterator
}

func fixture22RuntimeContext(t *testing.T, db *gorm.DB, suffix, toolName string, risk policy.RiskLevel, additionalTools ...string) (*workflow.GORMStore, context.Context) {
	t.Helper()
	entry, err := policy.LookupCatalog(toolName)
	if err != nil || entry.Risk != risk {
		t.Fatalf("phase22 Catalog fixture entry=%#v err=%v", entry, err)
	}
	input := fixture14SnapshotInput(t)
	input.Tools = append(input.Tools, ToolSnapshot{Name: entry.Name, Revision: entry.Revision, SchemaHash: entry.SchemaHash})
	for _, name := range additionalTools {
		additional, lookupErr := policy.LookupCatalog(name)
		if lookupErr != nil {
			t.Fatalf("phase22 additional Catalog fixture %q: %v", name, lookupErr)
		}
		input.Tools = append(input.Tools, ToolSnapshot{Name: additional.Name, Revision: additional.Revision, SchemaHash: additional.SchemaHash})
	}
	input.FeatureGates["agent_runtime.l1_writes"] = risk == policy.RiskL1
	input.FeatureGates["agent_runtime.l2_writes"] = risk == policy.RiskL2
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	userID := "operator-phase22-" + suffix
	identity := policy.Identity{UserID: userID, Role: policy.RoleOperator, Scope: policy.Scope{UserID: userID}}
	identityContext := policy.WithIdentity(context.Background(), identity)
	store := workflow.NewGORMStore(db)
	runID := "run-phase22-" + suffix
	run, err := store.CreateRunWithSessionLock(identityContext, workflow.CreateRunInput{
		ID: runID, WorkflowKey: "phase22-runtime-test", SessionID: "session-phase22-" + suffix,
		QueryText: "phase22 HITL", ImmutableInputJSON: json.RawMessage(`{"agent":"test","query":"phase22 HITL"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: json.RawMessage(`{}`),
		DeadlineAt: time.Now().Add(time.Hour), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{Owner: "worker-phase22-" + suffix, LeaseDuration: time.Hour})
	if err != nil || !ok || claimed.Run.ID != run.ID {
		t.Fatalf("claim phase22 Runtime Run: claimed=%#v ok=%v err=%v", claimed, ok, err)
	}
	ctx, err := workflow.ContextWithLeaseToken(identityContext, claimed.Token)
	if err != nil {
		t.Fatal(err)
	}
	attempt := &AttemptContext{
		Run: RunIdentity{ID: run.ID, SessionID: run.SessionID, Attempt: claimed.Run.Attempt,
			LeaseGeneration: claimed.Token.Generation, RuntimeVersion: *run.RuntimeVersion,
			RuntimeCompatibilityHash: frozen.CompatibilityHash()},
		Identity: identity, Scope: identity.Scope, Budget: newP14RecordingBudget(), Lease: claimed.Token,
		Trace: TraceIdentity{ID: "trace-phase22-" + suffix}, Deadline: time.Now().Add(time.Hour), Snapshot: frozen,
	}
	ctx = context.WithValue(ctx, attemptContextKey{}, attempt)
	modelSnapshot := frozen.Models()[0]
	ctx, err = WithModelInvocation(ctx, ModelInvocation{
		ReservationIdentity: "phase22-model-" + suffix, CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, ctx
}

func fixture22NewRunnerHarness(t *testing.T, store *workflow.GORMStore, ctx context.Context, checkpointStore adk.CheckPointStore) (*adk.Runner, *AttemptContext) {
	t.Helper()
	return fixture22NewRunnerHarnessWithModel(t, store, ctx, checkpointStore, &fixture14MutationToolCallingModel{})
}

func fixture22NewRunnerHarnessWithModel(
	t *testing.T,
	store *workflow.GORMStore,
	ctx context.Context,
	checkpointStore adk.CheckPointStore,
	chatModel model.BaseChatModel,
) (*adk.Runner, *AttemptContext) {
	t.Helper()
	handler, err := NewHITLRuntimeHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	registered := aitools.Get("create_report")
	if registered == nil {
		t.Fatal("create_report Tool is not registered")
	}
	handlers, err := RuntimeHandlerFirst(handler)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "phase22_runner_agent", Description: "phase22 Runner publish contract", Model: chatModel,
		GenModelInput: LiteralGenModelInput, Handlers: handlers, MaxIterations: 2,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{registered}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewDurableRunner(ctx, agent, checkpointStore, false)
	if err != nil {
		t.Fatal(err)
	}
	return runner, attempt
}

type fixture22FailingCheckpointStore struct {
	delegate adk.CheckPointStore
	setErr   error
}

func (s *fixture22FailingCheckpointStore) Get(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	return s.delegate.Get(ctx, checkpointID)
}

func (s *fixture22FailingCheckpointStore) Set(context.Context, string, []byte) error {
	return s.setErr
}

type fixture22TerminalObservingModel struct {
	expected    string
	calls       atomic.Int32
	sawExpected atomic.Bool
	observed    atomic.Value
}

func (m *fixture22TerminalObservingModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if m.calls.Add(1) == 1 {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "phase22-forged-resume-call", Type: "function",
			Function: schema.FunctionCall{Name: "create_report", Arguments: `{}`},
		}}}, nil
	}
	observed := make([]string, 0, len(input))
	for _, message := range input {
		if message != nil {
			observed = append(observed, fmt.Sprintf("%s:%s", message.Role, message.Content))
		}
		if message != nil && strings.Contains(message.Content, m.expected) {
			m.sawExpected.Store(true)
		}
	}
	m.observed.Store(strings.Join(observed, " | "))
	return schema.AssistantMessage("terminal Approval observed", nil), nil
}

func (m *fixture22TerminalObservingModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*fixture22TerminalObservingModel) BindTools([]*schema.ToolInfo) error { return nil }

func (a *fixture22ApprovalInterruptAgent) String() string { return fmt.Sprintf("%T", a) }
