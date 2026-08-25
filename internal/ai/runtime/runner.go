package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

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

// DurableExecutor 组合 P10-P14 primitive 与官方 Runner，不定义第二套 Agent Loop。
type DurableExecutor struct {
	store                    *workflow.GORMStore
	budgets                  BudgetHandleFactory
	resolveAgent             AgentResolver
	currentCompatibilityHash string
}

// NewDurableExecutor 创建 Worker 唯一执行入口。
func NewDurableExecutor(store *workflow.GORMStore, resolver AgentResolver, currentCompatibilityHash string) (*DurableExecutor, error) {
	if store == nil || resolver == nil {
		return nil, fmt.Errorf("durable Store and Agent resolver are required")
	}
	if err := validateSnapshotHash("current runtime compatibility hash", currentCompatibilityHash); err != nil {
		return nil, err
	}
	budgets, err := NewDurableBudget(store)
	if err != nil {
		return nil, err
	}
	return &DurableExecutor{store: store, budgets: budgets, resolveAgent: resolver, currentCompatibilityHash: currentCompatibilityHash}, nil
}

type immutableRunInput struct {
	Agent string `json:"agent"`
	Query string `json:"query"`
}

// ExecuteClaimedRun 从 MySQL 快照重建 Context，并只通过 P12 StartRecovery 调用官方 Runner。
func (e *DurableExecutor) ExecuteClaimedRun(ctx context.Context, claimed *workflow.ClaimedRun) (RunExecutionResult, error) {
	if e == nil || claimed == nil || claimed.Run.ImmutableInputJSON == nil {
		return RunExecutionResult{}, fmt.Errorf("claimed durable Run input is required")
	}
	var input immutableRunInput
	decoder := json.NewDecoder(strings.NewReader(*claimed.Run.ImmutableInputJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return RunExecutionResult{}, fmt.Errorf("decode immutable durable input: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return RunExecutionResult{}, fmt.Errorf("decode immutable durable input: trailing JSON value")
	}
	if strings.TrimSpace(input.Agent) == "" || strings.TrimSpace(input.Query) == "" {
		return RunExecutionResult{}, fmt.Errorf("immutable durable input requires agent and query")
	}
	attemptCtx, attempt, err := BuildAttemptContext(ctx, *claimed, e.budgets)
	if err != nil {
		return RunExecutionResult{}, err
	}
	defer attempt.Cancel()
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
			return RunExecutionResult{TraceID: attempt.Trace.ID, RunTransitioned: true}, resumeErr
		}
		return RunExecutionResult{TraceID: attempt.Trace.ID}, resumeErr
	}
	started, startErr := StartRecovery(attemptCtx, e.store, runner, attempt, e.currentCompatibilityHash, resumeParams)
	if started != nil && started.Decision.Mode == workflow.RecoveryModeParked {
		return RunExecutionResult{TraceID: attempt.Trace.ID, RunTransitioned: true}, startErr
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
			return RunExecutionResult{TraceID: attempt.Trace.ID, RunTransitioned: true}, nil
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
	outputPayload, err := json.Marshal(map[string]any{"answer": finalOutput, "trace_id": attempt.Trace.ID})
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	revision, err := buildP20Revision(claimed.Run, input.Query, finalOutput)
	if err != nil {
		return RunExecutionResult{TraceID: attempt.Trace.ID}, err
	}
	return RunExecutionResult{
		OutputPayload: string(outputPayload), RevisionStateJSON: revision,
		TraceQuality: "unknown", TraceID: attempt.Trace.ID,
	}, nil
}

func (e *DurableExecutor) approvalResumeParams(ctx context.Context, attempt *AttemptContext) (*adk.ResumeParams, error) {
	target, err := e.store.LoadApprovalResumeTarget(ctx, attempt.Lease)
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

func buildP20Revision(run mysql.WorkflowRun, query, output string) ([]byte, error) {
	if run.ContextSnapshotJSON == nil {
		return nil, fmt.Errorf("durable Run Context Snapshot is missing")
	}
	var snapshot workflow.DurableContextSnapshot
	if err := json.Unmarshal([]byte(*run.ContextSnapshotJSON), &snapshot); err != nil {
		return nil, err
	}
	state := map[string]any{}
	if err := json.Unmarshal(snapshot.History, &state); err != nil {
		return nil, err
	}
	history, _ := state["history"].([]any)
	history = append(history,
		map[string]any{"role": "user", "content": query},
		map[string]any{"role": "assistant", "content": output},
	)
	state["schema"] = workflow.SessionStateSchemaV1
	if run.SessionRevision != nil {
		state["revision"] = *run.SessionRevision + 1
	}
	state["history"] = history
	return json.Marshal(state)
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

// NewDurableRunner 直接把 P10 Store 注入官方 Eino RunnerConfig。
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
		}); err != nil {
			return nil, err
		}
		return &RecoveryStartResult{Decision: decision}, nil
	}
	if err := store.RecordRecoverySelection(ctx, workflow.RecoverySelectionRecord{
		Lease: attempt.Lease, Mode: decision.Mode, Attempt: attempt.Run.Attempt,
		TraceID: attempt.Trace.ID, RuntimeVersion: attempt.Run.RuntimeVersion,
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
