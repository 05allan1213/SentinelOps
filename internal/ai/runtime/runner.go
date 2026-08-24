package runtime

import (
	"context"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/workflow"

	"github.com/cloudwego/eino/adk"
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
