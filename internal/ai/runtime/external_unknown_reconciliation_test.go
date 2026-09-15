package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/ops/actions"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// S2 Case A：外部系统实际已执行、但客户端得到超时/响应丢失 →
// Runtime 落 unknown + parked（不盲目重试）→ 真实 Worker 对账 → 用同一幂等键复打
// 下游查真值 → 补记成功 → Run 回到可执行并最终完成，副作用只发生一次。
//
// 真实部分：真实 Eino Agent + RuntimeHandler、真实 Approval/Effect 账本与 lease/fencing、
// 真实 HTTP 下游（幂等语义）、真实 Worker 对账循环（Claim → Query → Resolve）。
// 受控部分：下游是本地 httptest 服务；模型是确定性 fake（不依赖外部 Provider）。

type unknownEffectDownstream struct {
	mu        sync.Mutex
	applied   map[string]string
	requests  int
	mutations int
	replays   int
	keys      []string
	release   chan struct{}
	releaseOn sync.Once
}

func newUnknownEffectDownstream() *unknownEffectDownstream {
	return &unknownEffectDownstream{applied: map[string]string{}, release: make(chan struct{})}
}

// Close 释放被故意挂起的响应，保证 httptest.Server.Close 不会永久等待。
func (d *unknownEffectDownstream) Close() {
	d.releaseOn.Do(func() { close(d.release) })
}

// serve 实现 provider 幂等语义：同一 Idempotency-Key 只产生一次副作用；
// 第一次请求在副作用生效后不返回响应（模拟真执行 + 响应丢失），
// 之后同一 key 的请求直接回放已生效结果。
func (d *unknownEffectDownstream) serve(writer http.ResponseWriter, request *http.Request) {
	key := request.Header.Get("Idempotency-Key")
	d.mu.Lock()
	d.requests++
	d.keys = append(d.keys, key)
	if reference, ok := d.applied[key]; ok {
		d.replays++
		d.mu.Unlock()
		writer.Header().Set("X-Request-ID", reference)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"replayed":true}`))
		return
	}
	reference := fmt.Sprintf("downstream-ref-%d", d.mutations+1)
	d.applied[key] = reference
	d.mutations++
	d.mu.Unlock()

	// 副作用已经真实生效，但响应在调用方 deadline 之前不会返回。
	select {
	case <-request.Context().Done():
	case <-d.release:
	case <-time.After(30 * time.Second):
	}
}

func (d *unknownEffectDownstream) snapshot() (requests, mutations, replays int, keys []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.requests, d.mutations, d.replays, append([]string(nil), d.keys...)
}

// unknownWebhookModel 只在「本轮对话还没有工具结果」时请求 webhook_out。
// 这样第一次执行、Resume、Replay 都会真实走到 Tool/Effect 路径，
// 而不是第二轮回退成一句普通回答，从而让「Effect 复用」可以被外部计数验证。
type unknownWebhookModel struct {
	url   string
	calls int
}

func (m *unknownWebhookModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	for _, message := range input {
		if message.Role == schema.Tool {
			return schema.AssistantMessage("webhook persisted", nil), nil
		}
	}
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: "phase-unknown-webhook-call", Type: "function", Function: schema.FunctionCall{
			Name: "webhook_out", Arguments: fmt.Sprintf(`{"url":%q,"payload":"{}","method":"POST"}`, m.url),
		},
	}}}, nil
}

func (m *unknownWebhookModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*unknownWebhookModel) BindTools([]*schema.ToolInfo) error { return nil }

func TestExternalUnknownTimeoutThenReconciliationClosesLoop(t *testing.T) {
	downstream := newUnknownEffectDownstream()
	server := httptest.NewServer(http.HandlerFunc(downstream.serve))
	defer server.Close()
	defer downstream.Close()
	// webhook_out 读取应用配置决定是否注入出站凭据；测试里显式使用空配置。
	oldConfig, _ := appconfig.Current()
	appconfig.SetCurrent(&appconfig.Config{})
	defer appconfig.SetCurrent(oldConfig)

	db := fixture12NewRuntimeDatabase(t, "phase_unknown_reconcile")
	const suffix = "unknown-timeout"
	store, ctx := fixture22RuntimeContext(t, db, suffix, "webhook_out", policy.RiskL2)
	runID := "run-phase22-" + suffix
	fixture23InstallBudgetTruth(t, db, runID)
	chatModel := &unknownWebhookModel{url: server.URL}
	runner, attempt := fixture24NewExternalRunnerHarness(t, store, ctx, chatModel)

	// ── 第一次执行：真实 Eino 路径直到 Approval 中断 ─────────────────────────────
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "send approved webhook",
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
		t.Fatal("external Tool call emitted no Approval interrupt")
	}
	durable := &DurableExecutor{store: store}
	if err := durable.publishApprovalInterrupt(ctx, attempt, interruptContexts); err != nil {
		t.Fatal(err)
	}
	var approval mysql.AgentApproval
	if err := db.First(&approval, "run_id = ?", attempt.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	approverID := "approver-unknown-reconcile"
	approverCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: approverID, Role: policy.RoleApprover, Scope: policy.Scope{UserID: approverID},
	})
	if _, err := store.DecideApprovalAndWakeRun(approverCtx, workflow.DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: workflow.ApprovalStatusApproved,
	}); err != nil {
		t.Fatal(err)
	}
	if requests, mutations, _, _ := downstream.snapshot(); requests != 0 || mutations != 0 {
		t.Fatalf("downstream touched before Approval resume: requests=%d mutations=%d", requests, mutations)
	}

	// ── 第二次执行：真实外部调用超时（副作用已生效，响应丢失）────────────────────
	// effect 调用 deadline 被夹在 lease 安全边界内：7s 租约 → 约 2s 调用窗口。
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-unknown-timeout", LeaseDuration: 7 * time.Second,
	})
	if err != nil || !ok || claimed.Run.ID != runID {
		t.Fatalf("claim approved external Run: claimed=%#v ok=%t err=%v", claimed, ok, err)
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
		ReservationIdentity: "unknown-resume-model", CatalogRef: modelSnapshot.CatalogRef,
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
	var resumeErrors []string
	for {
		event, ok := resumed.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			resumeErrors = append(resumeErrors, event.Err.Error())
		}
	}
	if requests, mutations, _, _ := downstream.snapshot(); requests != 1 || mutations != 1 {
		var diagnostic mysql.WorkflowRun
		_ = db.First(&diagnostic, "id = ?", runID).Error
		t.Fatalf("timeout phase downstream requests=%d mutations=%d, want 1/1; run=%s resume_errors=%v",
			requests, mutations, killexDescribeRun(&diagnostic), resumeErrors)
	}

	var parkedRun mysql.WorkflowRun
	if err := db.First(&parkedRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	var unknownEffect mysql.AgentEffect
	if err := db.First(&unknownEffect, "run_id = ? AND effect_step = ?", runID, workflow.EffectStepPrimary).Error; err != nil {
		t.Fatal(err)
	}
	if unknownEffect.Status != workflow.EffectStatusUnknown || unknownEffect.FinishedAt == nil {
		t.Fatalf("effect after timeout = status %s finished %v (errors=%v)", unknownEffect.Status, unknownEffect.FinishedAt, resumeErrors)
	}
	if parkedRun.Status != workflow.RunStatusParked || parkedRun.ParkReason == nil || *parkedRun.ParkReason != workflow.ParkReasonEffectUnknown {
		t.Fatalf("run after timeout = %s park_reason=%v", parkedRun.Status, parkedRun.ParkReason)
	}
	if parkedRun.LeaseOwner != nil || parkedRun.LeaseUntil != nil {
		t.Fatalf("parked run still holds a lease: owner=%v until=%v", parkedRun.LeaseOwner, parkedRun.LeaseUntil)
	}
	timeoutEvents := killexEventFacts(t, db, runID)
	if !killexHasEvent(timeoutEvents, workflow.EventEffectUnknown) || !killexHasEvent(timeoutEvents, workflow.EventRunParked) {
		t.Fatalf("timeout events = %#v", timeoutEvents)
	}
	// 未执行的副作用不允许被伪装成 definite failure。
	var failedEffects int64
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ? AND status = ?", runID, workflow.EffectStatusFailed).Count(&failedEffects).Error; err != nil {
		t.Fatal(err)
	}
	if failedEffects != 0 {
		t.Fatalf("timeout produced %d definite-failure Effect rows", failedEffects)
	}

	// ── 对账：真实 Worker 循环用同一幂等键复打下游查真值 ────────────────────────
	// 生产 wiring 在 internal/bootstrap/worker.go::queryEffectTargetState；
	// 这里复用它调用的同一 primitive：actions.Get(ToolName).Execute(ctx, parameters)。
	queryCalls := 0
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-unknown-reconcile", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return nil, false, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, nil
		},
		QueryEffectTargetState: func(queryCtx context.Context, target effects.ReconciliationTarget) (effects.TargetState, error) {
			queryCalls++
			executor, ok := actions.Get(target.ToolName)
			if !ok {
				return effects.TargetState{}, fmt.Errorf("action %q is not registered", target.ToolName)
			}
			result, execErr := executor.Execute(queryCtx, target.Parameters)
			if execErr != nil {
				return effects.TargetState{Evidence: map[string]any{"same_key_retry": "unknown"}}, execErr
			}
			response, marshalErr := json.Marshal(result.Output)
			if marshalErr != nil {
				return effects.TargetState{}, marshalErr
			}
			return effects.TargetState{
				Known: true, Applied: result.Success, Response: string(response),
				ExternalReference: result.Output["external_reference"], Evidence: result.Output,
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// ClaimEffectReconciliation 需要 Run 的调用身份；Worker 在真实循环中用
	// claimedRunIdentityContext(run) 生成同一身份，这里显式提供等价身份。
	workerCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "operator-phase22-" + suffix, Role: policy.RoleOperator,
		Scope: policy.Scope{UserID: "operator-phase22-" + suffix},
	})
	worked, err := worker.RunOnce(workerCtx)
	if err != nil || !worked || queryCalls != 1 {
		t.Fatalf("reconciliation RunOnce worked=%t query_calls=%d err=%v", worked, queryCalls, err)
	}
	requests, mutations, replays, keys := downstream.snapshot()
	if requests != 2 || mutations != 1 || replays != 1 {
		t.Fatalf("reconciliation downstream requests=%d mutations=%d replays=%d", requests, mutations, replays)
	}
	if keys[0] == "" || keys[0] != keys[1] || keys[0] != unknownEffect.IdempotencyKey {
		t.Fatalf("reconciliation did not reuse the ledger idempotency key: keys=%v effect_key=%s", keys, unknownEffect.IdempotencyKey)
	}
	var resolvedEffect mysql.AgentEffect
	if err := db.First(&resolvedEffect, "id = ?", unknownEffect.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resolvedEffect.Status != workflow.EffectStatusSucceeded || resolvedEffect.Resolution == nil ||
		*resolvedEffect.Resolution != workflow.EffectResolutionExecuted {
		t.Fatalf("resolved effect = status %s resolution %v", resolvedEffect.Status, resolvedEffect.Resolution)
	}
	if resolvedEffect.ExternalReference == nil || *resolvedEffect.ExternalReference != "downstream-ref-1" ||
		resolvedEffect.ResolvedBy == nil || *resolvedEffect.ResolvedBy != "worker-unknown-reconcile" {
		t.Fatalf("resolved effect reference=%v resolved_by=%v", resolvedEffect.ExternalReference, resolvedEffect.ResolvedBy)
	}
	var pendingRun mysql.WorkflowRun
	if err := db.First(&pendingRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if pendingRun.Status != workflow.RunStatusPending || pendingRun.ParkReason != nil {
		t.Fatalf("run after resolution = %s park_reason=%v", pendingRun.Status, pendingRun.ParkReason)
	}

	// ── 继续执行：第三次认领后 Resume（显式 resume target，来自已批准的 Approval），
	// 已成功 Effect 被复用，不再调用外部系统。
	// 注意：不带 checkpoint 的 Replay 会 fail closed（APPROVAL_RESUME_TARGET_REQUIRED），
	// 不会因为「审批已经批过」就自动重新执行副作用。 ─────────────────────────────
	continued, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-unknown-continue", LeaseDuration: time.Hour,
	})
	if err != nil || !ok || continued.Run.ID != runID {
		t.Fatalf("claim resumed Run: claimed=%#v ok=%t err=%v", continued, ok, err)
	}
	continuedCtx, continuedAttempt, err := BuildAttemptContext(context.Background(), *continued, budgets)
	if err != nil {
		t.Fatal(err)
	}
	continuedCtx, err = WithModelInvocation(continuedCtx, ModelInvocation{
		ReservationIdentity: "unknown-continue-model", CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	continuedParams, err := durable.approvalResumeParams(continuedCtx, continuedAttempt)
	if err != nil {
		t.Fatal(err)
	}
	continuedExecution, err := InvokeRecoveryRunner(continuedCtx, runner, continuedAttempt, RecoveryDecision{
		Mode: workflow.RecoveryModeResume, CheckpointID: checkpointID,
	}, continuedParams)
	if err != nil {
		t.Fatal(err)
	}
	for {
		event, ok := continuedExecution.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("continuation event: %v", event.Err)
		}
	}
	requests, mutations, _, _ = downstream.snapshot()
	if requests != 2 || mutations != 1 {
		t.Fatalf("continuation re-invoked the downstream: requests=%d mutations=%d", requests, mutations)
	}
	if err := store.CompleteRunAndCommitSession(continuedCtx, workflow.CompleteRunInput{
		RunID: runID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: workflow.RunStatusSucceeded,
		Lease: continuedAttempt.Lease, RevisionStateJSON: []byte(`{"continued":true}`),
		TraceQuality: "exact",
	}); err != nil {
		t.Fatalf("complete continued run: %v", err)
	}
	var finalRun mysql.WorkflowRun
	if err := db.First(&finalRun, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if finalRun.Status != workflow.RunStatusSucceeded || finalRun.Attempt != 3 {
		t.Fatalf("final run = %s attempt=%d", finalRun.Status, finalRun.Attempt)
	}
	events := killexEventFacts(t, db, runID)
	for _, want := range []string{
		workflow.EventEffectUnknown, workflow.EventRunParked, workflow.EventRunReconciling,
		workflow.EventEffectReconciling, workflow.EventEffectResolved, workflow.EventRunCompleted,
	} {
		if !killexHasEvent(events, want) {
			t.Fatalf("missing event %s in %#v", want, events)
		}
	}
	// 复用已成功 Effect 时不会重新发布 effect.started：整条 Run 只调用过一次外部系统。
	var startedEvents int
	for _, event := range events {
		if event.EventType == workflow.EventEffectStarted {
			startedEvents++
		}
	}
	if startedEvents != 1 {
		t.Fatalf("effect.started events = %d, want exactly 1 (reused ledger, not re-executed)", startedEvents)
	}
	t.Logf("closed loop: per-run events=%v", eventTypes(events))
	t.Logf("conflicting double-write check: downstream requests=%d mutations=%d (effect idempotency key=%s)",
		requests, mutations, unknownEffect.IdempotencyKey)
}

func killexHasEvent(events []killexEventFact, eventType string) bool {
	for _, event := range events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}

func eventTypes(events []killexEventFact) string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, fmt.Sprintf("%d:%s", event.Seq, event.EventType))
	}
	return strings.Join(types, ",")
}
