package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/ops/actions"
	"SentinelOps/internal/ai/policy"
	aitools "SentinelOps/internal/ai/tools"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// S2 Case B：外部确定没有执行 → 对账结论 not_executed → Run 回到可执行 →
// 同一逻辑动作安全执行一次，不产生重复副作用。
//
// 与 Case A 的差别：Case A 是「下游已生效」，这里下游明确「没有生效」，
// 所以对账允许 Run 重新排队执行；安全依据是 Effect 账本仍是同一幂等键，
// 且执行前仍要经过 Gate/Approval/lease 重验。

// testBlocklistTarget 是 block_ip 目标系统的测试替身：用进程内状态代替 nginx。
// 生产实现的同一位置是 internal/ai/ops/actions/system.go::BlockIPAction。
type testBlocklistTarget struct {
	mu       sync.Mutex
	applied  map[string]bool
	blocked  int
	reloads  int
	failNext bool
}

func newTestBlocklistTarget() *testBlocklistTarget {
	return &testBlocklistTarget{applied: map[string]bool{}}
}

func (*testBlocklistTarget) Name() string { return "block_ip" }

func (t *testBlocklistTarget) Execute(ctx context.Context, params map[string]string) (actions.ActionResult, error) {
	if err := effects.RequireMutationRoute(ctx); err != nil {
		return actions.ActionResult{}, err
	}
	ip := params["ip"]
	if ip == "" {
		return actions.ActionResult{}, effects.NewInvocationError(effects.InvocationSafeNotSent, false, nil, fmt.Errorf("block_ip: ip 不能为空"))
	}
	metadata, metadataErr := effects.ExecutionMetadataFromContext(ctx)
	t.mu.Lock()
	defer t.mu.Unlock()
	if metadataErr == nil && metadata.EffectStep == "nginx_reload" {
		if !t.applied[ip] {
			return actions.ActionResult{}, effects.NewInvocationError(effects.InvocationSafeNotSent, true, nil,
				fmt.Errorf("block_ip: target rule missing before reload"))
		}
		t.reloads++
		return actions.ActionResult{Success: true, Message: "reload confirmed", Output: map[string]string{"nginx_reload": "confirmed"}}, nil
	}
	if t.failNext {
		t.failNext = false
		// 请求离开本进程后结果不可判定；目标系统实际没有生效。
		return actions.ActionResult{}, effects.NewInvocationError(effects.InvocationUnknown, false,
			map[string]any{"target": "unconfirmed"}, fmt.Errorf("block_ip: provider result unknown"))
	}
	t.applied[ip] = true
	t.blocked++
	return actions.ActionResult{Success: true, Message: "IP blocked", Output: map[string]string{
		"ip": ip, "blocked": "true", "external_reference": "blocklist-ref-" + ip,
	}}, nil
}

// QueryTargetState 只读核对目标状态：生产实现的同名方法读取数据库与 nginx 黑名单文件。
func (t *testBlocklistTarget) QueryTargetState(_ context.Context, params map[string]string) (actions.TargetState, error) {
	ip := params["ip"]
	if ip == "" {
		return actions.TargetState{}, fmt.Errorf("block_ip: ip 不能为空")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	applied := t.applied[ip]
	evidence := map[string]any{"target": "absent", "blocklist_entries": len(t.applied)}
	if applied {
		evidence["target"] = "present"
	}
	return actions.TargetState{Known: true, Applied: applied, Evidence: evidence}, nil
}

func (t *testBlocklistTarget) counts() (blocked, reloads int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.blocked, t.reloads
}

type blockIPToolCallingModel struct {
	ip    string
	calls int
}

// 只在「本轮对话还没有工具结果」时请求 block_ip，保证 Resume 与 Replay
// 都会真实走到 Tool/Effect 路径（否则回退成普通回答就验证不到安全重放）。
func (m *blockIPToolCallingModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	for _, message := range input {
		if message.Role == schema.Tool {
			return schema.AssistantMessage("block ip processed", nil), nil
		}
	}
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: "phase-unknown-not-executed-call", Type: "function", Function: schema.FunctionCall{
			Name: "block_ip", Arguments: fmt.Sprintf(`{"ip":%q,"reason":"phase unknown not executed"}`, m.ip),
		},
	}}}, nil
}

func (m *blockIPToolCallingModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*blockIPToolCallingModel) BindTools([]*schema.ToolInfo) error { return nil }

// fixtureBlockIPRunnerHarness 与 phase24 harness 同构，但把 Tool 换成 block_ip。
func fixtureBlockIPRunnerHarness(t *testing.T, store *workflow.GORMStore, ctx context.Context, chatModel model.BaseChatModel) (*adk.Runner, *AttemptContext) {
	t.Helper()
	handler, err := NewHITLRuntimeHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	registered := aitools.Get("block_ip")
	if registered == nil {
		t.Fatal("block_ip Tool is not registered")
	}
	handlers, err := RuntimeHandlerFirst(handler)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "phase_unknown_not_executed_agent", Description: "block_ip not-executed contract",
		Model: chatModel, GenModelInput: LiteralGenModelInput, Handlers: handlers, MaxIterations: 2,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{registered}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewDurableRunner(ctx, agent, store, false)
	if err != nil {
		t.Fatal(err)
	}
	return runner, attempt
}

func TestExternalUnknownNotExecutedThenSafeReplay(t *testing.T) {
	target := newTestBlocklistTarget()
	target.failNext = true
	previous, hadPrevious := actions.Get("block_ip")
	actions.Register(target)
	defer func() {
		if hadPrevious && previous != nil {
			actions.Register(previous)
		}
	}()

	db := fixture12NewRuntimeDatabase(t, "phase_unknown_not_executed")
	const suffix = "unknown-not-executed"
	store, ctx := fixture22RuntimeContext(t, db, suffix, "block_ip", policy.RiskL2)
	runID := "run-phase22-" + suffix
	fixture23InstallBudgetTruth(t, db, runID)
	chatModel := &blockIPToolCallingModel{ip: "203.0.113.77"}
	runner, attempt := fixtureBlockIPRunnerHarness(t, store, ctx, chatModel)
	durable := &DurableExecutor{store: store}

	// 第一次执行直到 Approval 中断。
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "block the reported IP",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var interruptContexts []*adk.InterruptCtx
	for {
		event, ok := execution.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			interruptContexts = event.Action.Interrupted.InterruptContexts
		}
	}
	if len(interruptContexts) == 0 {
		t.Fatal("block_ip call emitted no Approval interrupt")
	}
	if err := durable.publishApprovalInterrupt(ctx, attempt, interruptContexts); err != nil {
		t.Fatal(err)
	}
	var approval mysql.AgentApproval
	if err := db.First(&approval, "run_id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	approverID := "approver-unknown-not-executed"
	approverCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: approverID, Role: policy.RoleApprover, Scope: policy.Scope{UserID: approverID},
	})
	if _, err := store.DecideApprovalAndWakeRun(approverCtx, workflow.DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: workflow.ApprovalStatusApproved,
	}); err != nil {
		t.Fatal(err)
	}

	// 第二次执行：外部结果未知（且目标实际没有生效）。
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-unknown-not-executed", LeaseDuration: time.Hour,
	})
	if err != nil || !ok || claimed.Run.ID != runID {
		t.Fatalf("claim approved block_ip Run: claimed=%#v ok=%t err=%v", claimed, ok, err)
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
		ReservationIdentity: "not-executed-model", CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	params, err := durable.approvalResumeParams(resumeCtx, resumeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, err := workflow.EinoCheckpointID(runID)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := InvokeRecoveryRunner(resumeCtx, runner, resumeAttempt, RecoveryDecision{
		Mode: workflow.RecoveryModeResume, CheckpointID: checkpointID,
	}, params)
	if err != nil {
		t.Fatal(err)
	}
	for {
		event, ok := resumed.Events.Next()
		if !ok {
			break
		}
		_ = event
	}
	blocked, reloads := target.counts()
	if blocked != 0 || reloads != 0 {
		t.Fatalf("target touched although result was unknown: blocked=%d reloads=%d", blocked, reloads)
	}
	var unknownEffect mysql.AgentEffect
	if err := db.First(&unknownEffect, "run_id = ? AND effect_step = ?", runID, workflow.EffectStepPrimary).Error; err != nil {
		t.Fatal(err)
	}
	if unknownEffect.Status != workflow.EffectStatusUnknown {
		t.Fatalf("primary Effect after unknown window = %s", unknownEffect.Status)
	}
	var parkedRun mysql.WorkflowRun
	if err := db.First(&parkedRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if parkedRun.Status != workflow.RunStatusParked {
		t.Fatalf("run after unknown window = %s", parkedRun.Status)
	}

	// 对账：目标状态查询确认「没有执行」→ Effect 回到 pending、Run 回到 pending。
	queryCalls := 0
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-unknown-not-executed-reconcile", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return nil, false, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, nil
		},
		// 生产 wiring 的可对账分支：actions.QueryTargetState(工具名, 参数)。
		QueryEffectTargetState: func(queryCtx context.Context, targetFact effects.ReconciliationTarget) (effects.TargetState, error) {
			queryCalls++
			state, queryErr := actions.QueryTargetState(queryCtx, targetFact.ToolName, targetFact.Parameters)
			if queryErr != nil {
				return effects.TargetState{Known: state.Known, Applied: state.Applied, Evidence: state.Evidence}, queryErr
			}
			response, marshalErr := json.Marshal(state.Evidence)
			if marshalErr != nil {
				return effects.TargetState{}, marshalErr
			}
			return effects.TargetState{Known: state.Known, Applied: state.Applied, Response: string(response), Evidence: state.Evidence}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	workerCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "operator-phase22-" + suffix, Role: policy.RoleOperator,
		Scope: policy.Scope{UserID: "operator-phase22-" + suffix},
	})
	worked, err := worker.RunOnce(workerCtx)
	if err != nil || !worked || queryCalls != 1 {
		t.Fatalf("reconciliation RunOnce worked=%t query_calls=%d err=%v", worked, queryCalls, err)
	}
	var resolvedEffect mysql.AgentEffect
	if err := db.First(&resolvedEffect, "id = ?", unknownEffect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resolvedEffect.Status != workflow.EffectStatusPending || resolvedEffect.Resolution == nil ||
		*resolvedEffect.Resolution != workflow.EffectResolutionNotExecuted {
		t.Fatalf("resolved Effect = status %s resolution %v", resolvedEffect.Status, resolvedEffect.Resolution)
	}
	var requeuedRun mysql.WorkflowRun
	if err := db.First(&requeuedRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if requeuedRun.Status != workflow.RunStatusPending {
		t.Fatalf("run after not_executed resolution = %s", requeuedRun.Status)
	}

	// 安全重放：同一逻辑动作真正执行一次，派生 reload 只发生一次。
	continued, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-unknown-not-executed-replay", LeaseDuration: time.Hour,
	})
	if err != nil || !ok || continued.Run.ID != runID {
		t.Fatalf("claim requeued Run: claimed=%#v ok=%t err=%v", continued, ok, err)
	}
	continuedCtx, continuedAttempt, err := BuildAttemptContext(context.Background(), *continued, budgets)
	if err != nil {
		t.Fatal(err)
	}
	continuedCtx, err = WithModelInvocation(continuedCtx, ModelInvocation{
		ReservationIdentity: "not-executed-replay-model", CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 继续执行走显式 resume target：不带 checkpoint 的 Replay 会因为
	// APPROVAL_RESUME_TARGET_REQUIRED fail closed，不会自动重放已批准的动作。
	continuedParams, err := durable.approvalResumeParams(continuedCtx, continuedAttempt)
	if err != nil {
		t.Fatal(err)
	}
	replayExecution, err := InvokeRecoveryRunner(continuedCtx, runner, continuedAttempt, RecoveryDecision{
		Mode: workflow.RecoveryModeResume, CheckpointID: checkpointID,
	}, continuedParams)
	if err != nil {
		t.Fatal(err)
	}
	for {
		event, ok := replayExecution.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("replay event: %v", event.Err)
		}
	}
	blocked, reloads = target.counts()
	if blocked != 1 || reloads != 1 {
		t.Fatalf("safe replay blocked=%d reloads=%d, want 1/1", blocked, reloads)
	}
	var succeededEffect mysql.AgentEffect
	if err := db.First(&succeededEffect, "id = ?", unknownEffect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if succeededEffect.Status != workflow.EffectStatusSucceeded {
		t.Fatalf("primary Effect after safe replay = %s", succeededEffect.Status)
	}
	if err := store.CompleteRunAndCommitSession(continuedCtx, workflow.CompleteRunInput{
		RunID: runID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: workflow.RunStatusSucceeded,
		Lease: continuedAttempt.Lease, RevisionStateJSON: []byte(`{"blocked":true}`), TraceQuality: "exact",
	}); err != nil {
		t.Fatalf("complete requeued run: %v", err)
	}
	events := killexEventFacts(t, db, runID)
	for _, want := range []string{
		workflow.EventEffectUnknown, workflow.EventRunParked, workflow.EventRunReconciling,
		workflow.EventEffectResolved, workflow.EventRunCompleted,
	} {
		if !killexHasEvent(events, want) {
			t.Fatalf("missing event %s in %#v", want, events)
		}
	}
	var sealed mysql.AgentEffect
	if err := db.First(&sealed, "run_id = ? AND effect_step = ?", runID, "nginx_reload").Error; err != nil {
		t.Fatal(err)
	}
	if sealed.Status != workflow.EffectStatusSucceeded {
		t.Fatalf("derived Effect status = %s", sealed.Status)
	}
	t.Logf("case B closed loop: events=%v blocked=%d reloads=%d", eventTypes(events), blocked, reloads)
}
