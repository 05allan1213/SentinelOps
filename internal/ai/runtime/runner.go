package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/policy"
	aitrace "SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// RecoveryExecution 暴露官方 Eino Event iterator 和 cancel function，不定义项目 Runner 接口。
type RecoveryExecution struct {
	Events *adk.AsyncIterator[*adk.AgentEvent]
	Cancel adk.AgentCancelFunc
}

// RecoveryStartResult 同时返回 selector 结论和可选的官方 Runner 执行。
// parked 结论没有 Execution；Resume 反序列化失败会先 fenced parked 再返回错误。
type RecoveryStartResult struct {
	Decision  RecoveryDecision
	Execution *RecoveryExecution
}

// AgentResolver 只把 immutable input 中白名单 Agent 名映射到真实 Eino Agent。
type AgentResolver func(context.Context, string) (adk.Agent, error)

// DurableExecutor 组合 phase10-phase14 primitive 与官方 Runner，不定义第二套 Agent Loop。
type DurableExecutor struct {
	store                 *workflow.GORMStore
	budgets               BudgetHandleFactory
	resolveAgent          AgentResolver
	gates                 *GateEvaluator
	loadLangfuse          func(context.Context) (*aitrace.LangfuseRuntime, error)
	expectedCompatibility func(context.Context, FrozenRuntimeSnapshot) (string, error)
}

// NewDurableExecutor 创建 Worker 唯一执行入口。
func NewDurableExecutor(store *workflow.GORMStore, resolver AgentResolver, compatibilityHash ...string) (*DurableExecutor, error) {
	if store == nil || resolver == nil {
		return nil, fmt.Errorf("durable Store and Agent resolver are required")
	}
	if len(compatibilityHash) > 1 {
		return nil, fmt.Errorf("at most one compatibility hash is accepted")
	}
	if len(compatibilityHash) == 1 {
		if err := validateSnapshotHash("legacy constructor compatibility hash", compatibilityHash[0]); err != nil {
			return nil, err
		}
	}
	budgets, err := NewDurableBudget(store)
	if err != nil {
		return nil, err
	}
	executor := &DurableExecutor{store: store, budgets: budgets, resolveAgent: resolver}
	if len(compatibilityHash) == 1 {
		legacyCompatibilityHash := compatibilityHash[0]
		executor.expectedCompatibility = func(context.Context, FrozenRuntimeSnapshot) (string, error) {
			return legacyCompatibilityHash, nil
		}
	}
	return executor, nil
}

// NewDurableExecutorWithReleaseControls 为生产 Worker 接入当前 Gate 与 Attempt-scoped Langfuse factory。
func NewDurableExecutorWithReleaseControls(
	store *workflow.GORMStore,
	resolver AgentResolver,
	gates *GateEvaluator,
	loadLangfuse func(context.Context) (*aitrace.LangfuseRuntime, error),
	expectedCompatibility func(context.Context, FrozenRuntimeSnapshot) (string, error),
) (*DurableExecutor, error) {
	if gates == nil || expectedCompatibility == nil {
		return nil, fmt.Errorf("gate evaluator and compatibility resolver are required")
	}
	executor, err := NewDurableExecutor(store, resolver)
	if err != nil {
		return nil, err
	}
	executor.gates = gates
	executor.loadLangfuse = loadLangfuse
	executor.expectedCompatibility = expectedCompatibility
	return executor, nil
}

// ExecuteClaimedRun 从 MySQL 快照重建 Context，并只通过 phase12 StartRecovery 调用官方 Runner。
func (e *DurableExecutor) ExecuteClaimedRun(ctx context.Context, claimed *workflow.ClaimedRun) (result RunExecutionResult, execErr error) {
	if e == nil || claimed == nil || claimed.Run.ImmutableInputJSON == nil {
		return RunExecutionResult{}, fmt.Errorf("claimed durable Run input is required")
	}
	input, err := BuildImmutableRunInput(claimed.Run)
	if err != nil {
		return RunExecutionResult{}, err
	}
	attemptCtx, attempt, err := BuildAttemptContext(ctx, *claimed, e.budgets)
	if err != nil {
		return RunExecutionResult{}, err
	}
	defer attempt.Cancel()
	langfuseRuntime, err := e.attemptLangfuse(attemptCtx, attempt.Snapshot)
	if err != nil {
		return RunExecutionResult{}, err
	}
	if langfuseRuntime != nil {
		attempt.callbacks = append(attempt.callbacks, langfuseRuntime.Handler())
	}
	models := make([]aitrace.ModelMetadata, 0, len(attempt.Snapshot.Models()))
	for _, model := range attempt.Snapshot.Models() {
		models = append(models, aitrace.ModelMetadata{
			Kind: model.Kind, CatalogRef: model.CatalogRef, Provider: model.Provider, Driver: model.Driver,
			ModelID: model.ModelID, Profile: model.Profile,
			RouteOptions:     map[string]any{"enable_thinking": model.RouteOptions.EnableThinking, "instruct": model.RouteOptions.Instruct},
			SnapshotIdentity: model.Identity(), PricingRevision: model.Pricing.Revision,
			PricingCurrency: model.Pricing.Currency, PricingUnit: model.Pricing.Unit,
			InputPrice: model.Pricing.Input, CachedInputPrice: model.Pricing.CachedInput, OutputPrice: model.Pricing.Output,
		})
	}
	attemptCtx, traceBarrier, err := aitrace.StartAttempt(attemptCtx, aitrace.AttemptMetadata{
		TraceID: attempt.Trace.ID, RunID: attempt.Run.ID, SessionID: attempt.Run.SessionID,
		Attempt: attempt.Run.Attempt, LeaseGeneration: attempt.Run.LeaseGeneration,
		RuntimeVersion: attempt.Run.RuntimeVersion, Query: input.Query, Models: models,
	}, langfuseRuntime)
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	defer func() {
		result.TraceID = attempt.Trace.ID
		result.TraceBarrier = traceBarrier
	}()
	if err := e.store.AttachAttemptTrace(attemptCtx, attempt.Lease, attempt.Trace.ID); err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	agent, err := e.resolveAgent(attemptCtx, input.Agent)
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	runner, err := NewDurableRunner(attemptCtx, agent, e.store, false)
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	resumeParams, resumeErr := e.approvalResumeParams(attemptCtx, attempt)
	if resumeErr != nil {
		if errors.Is(resumeErr, workflow.ErrApprovalCheckpointMismatch) || errors.Is(resumeErr, workflow.ErrApprovalInvalidated) {
			return RunExecutionResult{TraceID: attempt.Trace.ID, RunTransitioned: true, OperationFinalized: attempt.OperationID != ""}, resumeErr
		}
		return RunExecutionResult{TraceID: attempt.Trace.ID}, resumeErr
	}
	expectedCompatibility := RecoveryCompatibilityHash(attempt.Snapshot)
	if e.expectedCompatibility != nil {
		expectedCompatibility, err = e.expectedCompatibility(attemptCtx, attempt.Snapshot)
		if err != nil {
			return RunExecutionResult{TraceID: attempt.Trace.ID}, err
		}
	}
	started, startErr := StartRecovery(attemptCtx, e.store, runner, attempt, expectedCompatibility, resumeParams)
	if started != nil && started.Decision.Mode == workflow.RecoveryModeParked {
		return RunExecutionResult{TraceID: attempt.Trace.ID, RunTransitioned: true, OperationFinalized: attempt.OperationID != ""}, startErr
	}
	if startErr != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, classifyRunnerError(startErr)
	}
	if started == nil || started.Execution == nil || started.Execution.Events == nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, fmt.Errorf("official Runner returned no execution")
	}
	lifecycle := workerLifecycleContext(ctx)
	cancelStop := make(chan struct{})
	cancelResult := make(chan executionCancelResult, 1)
	go watchExecutionCancellation(lifecycle, e.store, attempt, started.Execution.Cancel, cancelStop, cancelResult)
	defer close(cancelStop)

	var finalOutput string
	for {
		event, ok := started.Execution.Events.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			if canceled, result, cancelErr := resolveExecutionCancellation(lifecycle, cancelResult, attempt.Trace.ID); canceled {
				return result, cancelErr
			}
			return RunExecutionResult{TraceID: attempt.Trace.ID}, classifyRunnerError(event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			if err := e.publishApprovalInterrupt(attemptCtx, attempt, event.Action.Interrupted.InterruptContexts); err != nil {
				return RunExecutionResult{TraceID: attempt.Trace.ID}, err
			}
			return RunExecutionResult{TraceID: attempt.Trace.ID, RunTransitioned: true, OperationFinalized: attempt.OperationID != ""}, nil
		}
		message, _, messageErr := adk.GetMessage(event)
		if messageErr != nil {
			return RunExecutionResult{TraceID: attempt.Trace.ID}, classifyRunnerError(messageErr)
		}
		if message == nil {
			continue
		}
		projected, projectErr := projectAgentEvent(event.AgentName, message, attempt.Trace.ID)
		if projectErr != nil {
			return RunExecutionResult{TraceID: attempt.Trace.ID}, projectErr
		}
		for _, durableEvent := range projected {
			if err := e.store.AppendRunEvent(attemptCtx, claimed.Token, durableEvent); err != nil {
				return RunExecutionResult{TraceID: attempt.Trace.ID}, err
			}
		}
		if message.Role == schema.Assistant && len(message.ToolCalls) == 0 && strings.TrimSpace(message.Content) != "" {
			finalOutput = message.Content
		}
	}
	if canceled, result, cancelErr := resolveExecutionCancellation(lifecycle, cancelResult, attempt.Trace.ID); canceled {
		return result, cancelErr
	}
	if strings.TrimSpace(finalOutput) == "" {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, fmt.Errorf("durable L0 Agent returned no final answer")
	}
	// Durable chat/v2 返回结构化 grounding 元数据，保持 answer 为模型原文。
	// 旧的 FinalizeCollectedAnswer 仍保留给兼容调用方（其文本提示语义不变）。
	var answerValidation evidence.AnswerValidation
	finalOutput, answerValidation, err = evidence.ValidateCollectedAnswer(attemptCtx, finalOutput)
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	if collector, ok := evidence.CollectorFromContext(attemptCtx); ok {
		runEvidence := collector.RunEvidence()
		if len(runEvidence.Refs) > 0 {
			ids := make([]string, 0, len(runEvidence.Refs))
			for id := range runEvidence.Refs {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			if err := e.store.AppendRunEvent(attemptCtx, claimed.Token, workflow.WorkflowEventInput{Type: workflow.EventEvidenceRetrieved, TraceID: attempt.Trace.ID, Payload: workflow.EventPayload{Summary: "Evidence references retrieved", Attributes: map[string]any{"evidence_ids": ids, "count": len(ids)}}}); err != nil {
				return RunExecutionResult{TraceID: attempt.Trace.ID}, err
			}
			if len(evidence.ExtractCitations(finalOutput)) > 0 {
				if err := e.store.AppendRunEvent(attemptCtx, claimed.Token, workflow.WorkflowEventInput{Type: workflow.EventEvidenceCited, TraceID: attempt.Trace.ID, Payload: workflow.EventPayload{Summary: "Evidence references cited", Attributes: map[string]any{"evidence_ids": ids}}}); err != nil {
					return RunExecutionResult{TraceID: attempt.Trace.ID}, err
				}
			}
		}
	}
	output := map[string]any{"answer": finalOutput, "trace_id": attempt.Trace.ID}
	if answerValidation.Grounding != "" {
		output["grounding"] = answerValidation.Grounding
		output["grounding_reason"] = answerValidation.Reason
		if len(answerValidation.Citations) > 0 {
			output["citations"] = answerValidation.Citations
		}
	}
	outputPayload, err := json.Marshal(output)
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	if claimed.Run.ContextSnapshotJSON == nil || claimed.Run.SessionRevision == nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, fmt.Errorf("durable Run Context Snapshot or Session Revision is missing")
	}
	var snapshot workflow.DurableContextSnapshot
	if err := json.Unmarshal([]byte(*claimed.Run.ContextSnapshotJSON), &snapshot); err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, fmt.Errorf("decode durable Context Snapshot: %w", err)
	}
	revision, err := workflow.BuildSessionRevisionPayload(snapshot.History, *claimed.Run.SessionRevision+1, input.Query, finalOutput)
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	return RunExecutionResult{
		OutputPayload: string(outputPayload), RevisionStateJSON: revision,
		TraceQuality: "unknown", TraceID: attempt.Trace.ID, TraceBarrier: traceBarrier,
	}, nil
}

func (e *DurableExecutor) attemptLangfuse(ctx context.Context, frozen FrozenRuntimeSnapshot) (*aitrace.LangfuseRuntime, error) {
	if e == nil || e.gates == nil {
		return nil, nil
	}
	effective, err := e.gates.Effective(ctx, frozen)
	if err != nil {
		return nil, err
	}
	if !effective.Enabled(GateLangfuseEnabled) {
		return nil, nil
	}
	if e.loadLangfuse == nil {
		return nil, fmt.Errorf("langfuse Gate is open without an Attempt factory")
	}
	runtime, err := e.loadLangfuse(ctx)
	if err != nil {
		return nil, err
	}
	if runtime == nil || runtime.Handler() == nil {
		if runtime != nil {
			_ = runtime.Shutdown(ctx)
		}
		return nil, fmt.Errorf("langfuse Attempt factory returned no official handler")
	}
	effective, err = e.gates.Effective(ctx, frozen)
	if err != nil {
		_ = runtime.Shutdown(ctx)
		return nil, err
	}
	if !effective.Enabled(GateLangfuseEnabled) {
		_ = runtime.Shutdown(ctx)
		return nil, nil
	}
	return runtime, nil
}

func (e *DurableExecutor) approvalResumeParams(ctx context.Context, attempt *AttemptContext) (*adk.ResumeParams, error) {
	target, err := e.store.LoadApprovalResumeTarget(ctx, attempt.Lease, attempt.OperationID, attempt.OperationAction)
	if err != nil || target == nil {
		return nil, err
	}
	params := &adk.ResumeParams{Targets: map[string]any{}}
	if target.InterruptID != "" {
		params.Targets[target.InterruptID] = ApprovalResumeData{ApprovalID: target.ApprovalID, Decision: target.Decision}
	}
	return params, nil
}

func (e *DurableExecutor) publishApprovalInterrupt(ctx context.Context, attempt *AttemptContext, contexts []*adk.InterruptCtx) error {
	var selected *adk.InterruptCtx
	var info ApprovalInterruptInfo
	for _, interruptContext := range contexts {
		if interruptContext == nil || !interruptContext.IsRootCause {
			continue
		}
		candidate, ok := interruptContext.Info.(ApprovalInterruptInfo)
		if !ok {
			if pointer, pointerOK := interruptContext.Info.(*ApprovalInterruptInfo); pointerOK && pointer != nil {
				candidate, ok = *pointer, true
			}
		}
		if !ok {
			continue
		}
		if selected != nil {
			return fmt.Errorf("multiple Approval root interrupts require a new Spec decision")
		}
		selected, info = interruptContext, candidate
	}
	if selected == nil || info.ApprovalID == "" || info.ProposalHash == "" {
		return fmt.Errorf("durable Interrupt did not contain one Approval root cause")
	}
	checkpointID, err := workflow.EinoCheckpointID(attempt.Run.ID)
	if err != nil {
		return err
	}
	fingerprint, err := e.store.LoadCheckpointFingerprint(ctx, attempt.Lease, checkpointID)
	if err != nil {
		return err
	}
	_, err = e.store.PublishApprovalAndWait(ctx, workflow.PublishApprovalInput{
		Lease: attempt.Lease, ApprovalID: info.ApprovalID, ProposalHash: info.ProposalHash,
		InterruptID: selected.ID, InterruptAddress: selected.Address.String(),
		CheckpointID: checkpointID, CheckpointPayloadSHA256: fingerprint.PayloadSHA256,
		CheckpointLeaseGeneration: fingerprint.LeaseGeneration, TraceID: attempt.Trace.ID,
		OperationID: attempt.OperationID, OperationAction: attempt.OperationAction,
		OperationErrorCode: "approval_required", OperationReason: "approval_required",
	})
	return err
}

type executionCancelResult struct {
	handoff bool
	err     error
}

func watchExecutionCancellation(
	lifecycle context.Context,
	store *workflow.GORMStore,
	attempt *AttemptContext,
	cancel adk.AgentCancelFunc,
	stop <-chan struct{},
	result chan<- executionCancelResult,
) {
	select {
	case <-stop:
		return
	case <-lifecycle.Done():
	}
	cause := context.Cause(lifecycle)
	if errors.Is(cause, workflow.ErrLeaseLost) {
		handle, err := RequestLostLeaseCancel(cancel)
		if err == nil {
			err = handle.Wait()
		}
		if err != nil {
			result <- executionCancelResult{err: fmt.Errorf("cancel lost-lease execution: %w", err)}
			return
		}
		result <- executionCancelResult{err: workflow.ErrLeaseLost}
		return
	}
	handle, err := RequestDrain(cancel, DrainAfterToolCalls, 5*time.Second)
	if err != nil {
		result <- executionCancelResult{err: fmt.Errorf("request safe-point drain: %w", err)}
		return
	}
	confirmCtx, confirmCancel := context.WithTimeout(policy.WithIdentity(context.Background(), attempt.Identity), 5*time.Second)
	defer confirmCancel()
	if err := confirmSafePointEventually(confirmCtx, store, attempt, handle); err != nil {
		result <- executionCancelResult{err: err}
		return
	}
	result <- executionCancelResult{handoff: true}
}

func confirmSafePointEventually(ctx context.Context, store *workflow.GORMStore, attempt *AttemptContext, handle *adk.CancelHandle) error {
	for {
		err := ConfirmSafePointHandoff(ctx, store, attempt, handle)
		if err == nil || !errors.Is(err, workflow.ErrRecoveryHandoffDenied) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for fenced safe-point checkpoint: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func resolveExecutionCancellation(
	lifecycle context.Context,
	result <-chan executionCancelResult,
	traceID string,
) (bool, RunExecutionResult, error) {
	if lifecycle.Err() == nil {
		return false, RunExecutionResult{}, nil
	}
	outcome := <-result
	executionResult := RunExecutionResult{TraceID: traceID, RunTransitioned: true}
	if outcome.handoff {
		return true, executionResult, nil
	}
	if outcome.err != nil {
		return true, executionResult, outcome.err
	}
	return true, executionResult, context.Cause(lifecycle)
}

func projectAgentEvent(agentName string, message *schema.Message, traceID string) ([]workflow.WorkflowEventInput, error) {
	events := make([]workflow.WorkflowEventInput, 0, len(message.ToolCalls)+1)
	for _, call := range message.ToolCalls {
		events = append(events, workflow.WorkflowEventInput{Type: workflow.EventAgentToolCall, TraceID: traceID,
			Payload: workflow.EventPayload{Summary: "durable L0 Tool call", Attributes: map[string]any{"agent_name": agentName, "tool_name": call.Function.Name}}})
	}
	if message.Role == schema.Tool {
		events = append(events, workflow.WorkflowEventInput{Type: workflow.EventAgentToolResult, TraceID: traceID,
			Payload: workflow.EventPayload{Summary: truncateEventSummary(message.Content), Attributes: map[string]any{"agent_name": agentName, "tool_name": message.ToolName}}})
	}
	if message.Role == schema.Assistant && len(message.ToolCalls) == 0 && strings.TrimSpace(message.Content) != "" {
		events = append(events, workflow.WorkflowEventInput{Type: workflow.EventAgentPlan, TraceID: traceID,
			Payload: workflow.EventPayload{Summary: truncateEventSummary(message.Content), Reference: traceID, Attributes: map[string]any{"agent_name": agentName}}})
	}
	return events, nil
}

func truncateEventSummary(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return string(runes)
}

func classifyRunnerError(err error) error {
	if err == nil {
		return nil
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		return Retryable(err)
	}
	return err
}

// NewDurableRunner 直接把 phase10 Store 注入官方 Eino RunnerConfig。
func NewDurableRunner(ctx context.Context, agent adk.Agent, store adk.CheckPointStore, enableStreaming bool) (*adk.Runner, error) {
	if ctx == nil {
		return nil, fmt.Errorf("runner context is required")
	}
	if isNilInterface(agent) {
		return nil, fmt.Errorf("durable Agent is required")
	}
	if isNilInterface(store) {
		return nil, fmt.Errorf("durable CheckPointStore is required")
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent: agent, EnableStreaming: enableStreaming, CheckPointStore: store,
	}), nil
}

// StartRecovery 从唯一 GORMStore 读取事实、持久化选择，再进入官方 Eino Runner。
func StartRecovery(
	ctx context.Context,
	store *workflow.GORMStore,
	runner *adk.Runner,
	attempt *AttemptContext,
	currentCompatibilityHash string,
	resumeParams *adk.ResumeParams,
) (*RecoveryStartResult, error) {
	if ctx == nil || store == nil || runner == nil || attempt == nil {
		return nil, fmt.Errorf("recovery context, store, runner and attempt are required")
	}
	facts, err := store.LoadRecoveryFacts(ctx, attempt.Lease)
	if err != nil {
		return nil, err
	}
	decision, err := SelectRecovery(facts, currentCompatibilityHash)
	if err != nil {
		return nil, err
	}
	if decision.Mode == workflow.RecoveryModeParked {
		if err := store.ParkRecovery(ctx, workflow.RecoveryParkInput{
			Lease: attempt.Lease, ExpectedStatus: workflow.RunStatusRunning, Reason: decision.ParkReason,
			Attempt: attempt.Run.Attempt, TraceID: attempt.Trace.ID, RuntimeVersion: attempt.Run.RuntimeVersion,
			OperationID: attempt.OperationID, OperationAction: attempt.OperationAction,
			OperationErrorCode: "recovery_parked", OperationReason: decision.ParkReason,
		}); err != nil {
			return nil, err
		}
		return &RecoveryStartResult{Decision: decision}, nil
	}
	if err := store.RecordRecoverySelection(ctx, workflow.RecoverySelectionRecord{
		Lease: attempt.Lease, Mode: decision.Mode, Attempt: attempt.Run.Attempt,
		TraceID: attempt.Trace.ID, RuntimeVersion: attempt.Run.RuntimeVersion,
		OperationID: attempt.OperationID,
	}); err != nil {
		return nil, err
	}
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, decision, resumeParams)
	if err == nil {
		return &RecoveryStartResult{Decision: decision, Execution: execution}, nil
	}
	if decision.Mode != workflow.RecoveryModeResume {
		return nil, err
	}
	parkDecision := RecoveryDecision{Mode: workflow.RecoveryModeParked, ParkReason: workflow.ParkReasonCheckpointCorrupt}
	if parkErr := store.ParkRecovery(ctx, workflow.RecoveryParkInput{
		Lease: attempt.Lease, ExpectedStatus: workflow.RunStatusRunning, Reason: parkDecision.ParkReason,
		Attempt: attempt.Run.Attempt, TraceID: attempt.Trace.ID, RuntimeVersion: attempt.Run.RuntimeVersion,
		OperationID: attempt.OperationID, OperationAction: attempt.OperationAction,
		OperationErrorCode: "checkpoint_corrupt", OperationReason: parkDecision.ParkReason,
	}); parkErr != nil {
		return nil, fmt.Errorf("runner Resume failed (%v) and fenced park failed: %w", err, parkErr)
	}
	return &RecoveryStartResult{Decision: parkDecision}, fmt.Errorf("runner Resume failed and Run was parked: %w", err)
}

// InvokeRecoveryRunner 只调用官方 Query / Resume / ResumeWithParams，并固定 checkpoint-safe SessionValues。
func InvokeRecoveryRunner(
	ctx context.Context,
	runner *adk.Runner,
	attempt *AttemptContext,
	decision RecoveryDecision,
	resumeParams *adk.ResumeParams,
) (*RecoveryExecution, error) {
	if ctx == nil || runner == nil || attempt == nil {
		return nil, fmt.Errorf("runner, context and typed attempt are required")
	}
	if strings.TrimSpace(attempt.Run.ID) == "" || attempt.Run.Attempt == 0 || attempt.Run.LeaseGeneration == 0 ||
		attempt.Lease.RunID != attempt.Run.ID || attempt.Lease.Generation != attempt.Run.LeaseGeneration ||
		strings.TrimSpace(attempt.Trace.ID) == "" {
		return nil, fmt.Errorf("typed Attempt identity is incomplete")
	}
	sessionValues, err := SafeSessionValues(map[string]any{
		SessionRunIDKey:             attempt.Run.ID,
		SessionRuntimeVersionKey:    attempt.Run.RuntimeVersion,
		SessionCompatibilityHashKey: attempt.Run.RuntimeCompatibilityHash,
	})
	if err != nil {
		return nil, err
	}
	cancelOption, cancel := adk.WithCancel()
	commonOptions := []adk.AgentRunOption{cancelOption, adk.WithSessionValues(sessionValues)}
	if len(attempt.callbacks) > 0 {
		commonOptions = append(commonOptions, adk.WithCallbacks(attempt.callbacks...))
	}

	var events *adk.AsyncIterator[*adk.AgentEvent]
	switch decision.Mode {
	case workflow.RecoveryModeReplay:
		if strings.TrimSpace(decision.ImmutableQuery) == "" || resumeParams != nil {
			return nil, fmt.Errorf("replay requires immutable query and no ResumeParams")
		}
		checkpointID, err := workflow.EinoCheckpointID(attempt.Run.ID)
		if err != nil {
			return nil, err
		}
		events = runner.Query(ctx, decision.ImmutableQuery, append(commonOptions, adk.WithCheckPointID(checkpointID))...)
	case workflow.RecoveryModeResume:
		expectedCheckpointID, err := workflow.EinoCheckpointID(attempt.Run.ID)
		if err != nil {
			return nil, err
		}
		if decision.CheckpointID != expectedCheckpointID {
			return nil, workflow.ErrCheckpointIDMismatch
		}
		if resumeParams == nil {
			events, err = runner.Resume(ctx, decision.CheckpointID, commonOptions...)
		} else {
			events, err = runner.ResumeWithParams(ctx, decision.CheckpointID, resumeParams, commonOptions...)
		}
		if err != nil {
			return nil, fmt.Errorf("resume official Eino Runner: %w", err)
		}
	default:
		return nil, fmt.Errorf("cannot invoke Runner for recovery mode %q", decision.Mode)
	}
	return &RecoveryExecution{Events: events, Cancel: cancel}, nil
}
