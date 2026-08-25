package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/workflow"
)

// WorkerConfig 是 P09 Worker 雏形所需的 lease 与有界空轮询参数。
type WorkerConfig struct {
	Owner          string
	LeaseDuration  time.Duration
	MinPollBackoff time.Duration
	MaxPollBackoff time.Duration
	ClaimNext      func(context.Context) (*workflow.ClaimedRun, bool, error)
	Heartbeat      func(context.Context, workflow.LeaseToken) error
	Execute        func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error)
	Transition     func(context.Context, workflow.RunTransition) error
	Complete       func(context.Context, workflow.CompleteRunInput) error
}

// Worker 只委派唯一 workflow.GORMStore，并承载 P20 唯一 durable poll loop。
type Worker struct {
	store      *workflow.GORMStore
	config     WorkerConfig
	claimNext  func(context.Context) (*workflow.ClaimedRun, bool, error)
	heartbeat  func(context.Context, workflow.LeaseToken) error
	execute    func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error)
	transition func(context.Context, workflow.RunTransition) error
	complete   func(context.Context, workflow.CompleteRunInput) error
}

// RunExecutionResult 是 Worker 交给唯一完成 primitive 的基础结果。
type RunExecutionResult struct {
	OutputPayload     string
	RevisionStateJSON []byte
	TraceQuality      string
	TraceID           string
	RunTransitioned   bool
}

type classifiedExecutionError struct {
	cause      error
	retryable  bool
	parkReason string
}

type workerLifecycleContextKey struct{}

func (e *classifiedExecutionError) Error() string { return e.cause.Error() }
func (e *classifiedExecutionError) Unwrap() error { return e.cause }

// Retryable 标记允许在 max_attempts 内按 available_at 有界退避的执行错误。
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &classifiedExecutionError{cause: err, retryable: true}
}

// Parked 标记必须 fail-closed 保持 Session 占用的恢复错误。
func Parked(err error, reason string) error {
	if err == nil {
		return nil
	}
	return &classifiedExecutionError{cause: err, parkReason: reason}
}

// NewWorker 创建无进程内调度真值的 Worker 雏形。
func NewWorker(store *workflow.GORMStore, config WorkerConfig) (*Worker, error) {
	if store == nil && (config.ClaimNext == nil || config.Transition == nil || config.Complete == nil) {
		return nil, fmt.Errorf("workflow Store is required")
	}
	if strings.TrimSpace(config.Owner) == "" || len(config.Owner) > 128 {
		return nil, fmt.Errorf("worker owner must contain 1 to 128 bytes")
	}
	if config.LeaseDuration < time.Millisecond || config.LeaseDuration > 24*time.Hour {
		return nil, fmt.Errorf("worker lease duration must be between 1ms and 24h")
	}
	if config.MinPollBackoff <= 0 || config.MaxPollBackoff < config.MinPollBackoff {
		return nil, fmt.Errorf("worker poll backoff bounds are invalid")
	}
	worker := &Worker{
		store: store, config: config, claimNext: config.ClaimNext, execute: config.Execute,
		heartbeat: config.Heartbeat, transition: config.Transition, complete: config.Complete,
	}
	if worker.claimNext == nil {
		worker.claimNext = worker.ClaimNext
	}
	if worker.transition == nil {
		worker.transition = store.TransitionRunWithEvent
	}
	if worker.heartbeat == nil && store != nil {
		worker.heartbeat = func(ctx context.Context, token workflow.LeaseToken) error {
			return store.HeartbeatLease(ctx, token, config.LeaseDuration)
		}
	}
	if worker.complete == nil {
		worker.complete = store.CompleteRunAndCommitSession
	}
	return worker, nil
}

// ClaimNext 尝试认领一个 Run；空结果由调用方使用 NextPollBackoff 退避。
func (w *Worker) ClaimNext(ctx context.Context) (*workflow.ClaimedRun, bool, error) {
	return w.store.ClaimNextRun(ctx, workflow.ClaimInput{
		Owner: w.config.Owner, LeaseDuration: w.config.LeaseDuration,
	})
}

// RunOnce 认领并执行一个 Run；API/SSE 生命周期不参与此调用。
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.execute == nil {
		return false, fmt.Errorf("durable Run executor is required")
	}
	claimed, ok, err := w.claimNext(ctx)
	if err != nil || !ok {
		return ok, err
	}
	runCtx := ctx
	if w.store != nil {
		var identityErr error
		runCtx, _, _, identityErr = claimedRunIdentityContext(ctx, claimed.Run)
		if identityErr != nil {
			return true, identityErr
		}
	}
	result, executionErr, heartbeatErr := w.executeWithHeartbeat(runCtx, claimed)
	if heartbeatErr != nil {
		return true, heartbeatErr
	}
	if result.RunTransitioned {
		return true, executionErr
	}
	if executionErr == nil {
		return true, w.complete(runCtx, workflow.CompleteRunInput{
			RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning,
			TargetStatus: workflow.RunStatusSucceeded, Lease: claimed.Token,
			OutputPayload: result.OutputPayload, RevisionStateJSON: result.RevisionStateJSON,
			TraceQuality: result.TraceQuality, TraceID: result.TraceID,
		})
	}

	var classified *classifiedExecutionError
	if errors.As(executionErr, &classified) && classified.parkReason != "" {
		return true, w.transition(runCtx, workflow.RunTransition{
			RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning,
			TargetStatus: workflow.RunStatusParked, ParkReason: classified.parkReason, Lease: claimed.Token,
			Event: workflow.WorkflowEventInput{Type: workflow.EventRunParked, TraceID: result.TraceID,
				Payload: workflow.EventPayload{Attributes: map[string]any{"park_reason": classified.parkReason}}},
		})
	}
	if errors.As(executionErr, &classified) && classified.retryable && claimed.Run.Attempt < claimed.Run.MaxAttempts {
		return true, w.transition(runCtx, workflow.RunTransition{
			RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning,
			TargetStatus: workflow.RunStatusRetryableFailed, AvailableAt: time.Now().Add(w.retryBackoff(claimed.Run.Attempt)),
			Lease: claimed.Token, Event: workflow.WorkflowEventInput{Type: workflow.EventRunFailed, TraceID: result.TraceID,
				Payload: workflow.EventPayload{Attributes: map[string]any{"retryable": true, "attempt": claimed.Run.Attempt}}},
		})
	}
	target := workflow.RunStatusFailed
	if errors.Is(executionErr, context.Canceled) {
		target = workflow.RunStatusCanceled
	}
	return true, w.complete(runCtx, workflow.CompleteRunInput{
		RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: target,
		Lease: claimed.Token, ErrorMessage: executionErr.Error(), TraceQuality: result.TraceQuality, TraceID: result.TraceID,
	})
}

func (w *Worker) executeWithHeartbeat(ctx context.Context, claimed *workflow.ClaimedRun) (RunExecutionResult, error, error) {
	if w.heartbeat == nil {
		result, err := w.execute(ctx, claimed)
		return result, err, nil
	}
	executionCtx, cancel := context.WithCancelCause(ctx)
	stop := make(chan struct{})
	heartbeatResult := make(chan error, 1)
	interval := w.config.LeaseDuration / 3
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				heartbeatResult <- nil
				return
			case <-executionCtx.Done():
				heartbeatResult <- nil
				return
			case <-ticker.C:
				if err := w.heartbeat(executionCtx, claimed.Token); err != nil {
					cancel(err)
					heartbeatResult <- err
					return
				}
			}
		}
	}()
	callCtx := executionCtx
	if w.store != nil {
		// Agent 执行保留 frozen identity/value，并由官方 Eino cancel 完成 safe-point；
		// lifecycle context 单独传入，避免 SIGTERM 先取消 Runner 而来不及提交 Checkpoint。
		callCtx = context.WithValue(context.WithoutCancel(executionCtx), workerLifecycleContextKey{}, executionCtx)
	}
	result, executionErr := w.execute(callCtx, claimed)
	close(stop)
	cancel(nil)
	heartbeatErr := <-heartbeatResult
	return result, executionErr, heartbeatErr
}

func workerLifecycleContext(ctx context.Context) context.Context {
	if lifecycle, ok := ctx.Value(workerLifecycleContextKey{}).(context.Context); ok && lifecycle != nil {
		return lifecycle
	}
	return ctx
}

// Run 持续复用同一 MySQL claim loop；没有进程内 Queue 或第二个 Scheduler。
func (w *Worker) Run(ctx context.Context) error {
	empty := 0
	for {
		didWork, err := w.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, workflow.ErrLeaseLost) || errors.Is(err, workflow.ErrRunCASConflict) {
				timer := time.NewTimer(w.config.MinPollBackoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return err
		}
		if didWork {
			empty = 0
			continue
		}
		delay := w.NextPollBackoff(empty)
		empty++
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *Worker) retryBackoff(attempt uint) time.Duration {
	delay := w.config.MinPollBackoff
	for current := uint(1); current < attempt; current++ {
		if delay >= w.config.MaxPollBackoff/2 {
			return w.config.MaxPollBackoff
		}
		delay *= 2
	}
	if delay > w.config.MaxPollBackoff {
		return w.config.MaxPollBackoff
	}
	return delay
}

// Heartbeat 延长当前 generation 的有效 lease。
func (w *Worker) Heartbeat(ctx context.Context, token workflow.LeaseToken) error {
	if w == nil || w.heartbeat == nil {
		return fmt.Errorf("durable Worker heartbeat is not configured")
	}
	return w.heartbeat(ctx, token)
}

// ReapExpired 有界失效过期 lease；不启动第二个 Scheduler 或 Queue。
func (w *Worker) ReapExpired(ctx context.Context, limit int) (int64, error) {
	return w.store.ReapExpiredLeases(ctx, workflow.ReapInput{Limit: limit})
}

// NextPollBackoff 返回指数增长且受配置上限约束的空轮询等待时间。
func (w *Worker) NextPollBackoff(consecutiveEmpty int) time.Duration {
	if consecutiveEmpty <= 0 {
		return w.config.MinPollBackoff
	}
	delay := w.config.MinPollBackoff
	for range consecutiveEmpty {
		if delay >= w.config.MaxPollBackoff/2 {
			return w.config.MaxPollBackoff
		}
		delay *= 2
	}
	if delay > w.config.MaxPollBackoff {
		return w.config.MaxPollBackoff
	}
	return delay
}
