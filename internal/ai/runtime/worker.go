package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"

	"gorm.io/gorm"
)

// WorkerConfig 是 phase09 Worker 雏形所需的 lease 与有界空轮询参数。
type WorkerConfig struct {
	Owner                  string
	LeaseDuration          time.Duration
	MinPollBackoff         time.Duration
	MaxPollBackoff         time.Duration
	ApprovalExpiryBatch    int
	ReconciliationBatch    int
	QueryEffectTargetState effects.TargetStateQuery
	ClaimNext              func(context.Context) (*workflow.ClaimedRun, bool, error)
	Heartbeat              func(context.Context, workflow.LeaseToken) error
	Execute                func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error)
	Transition             func(context.Context, workflow.RunTransition) error
	Complete               func(context.Context, workflow.CompleteRunInput) error
	ProjectRevision        func(context.Context, string, []byte) error
	Retention              *RetentionCoordinator
	RuntimeVersion         string
	Gates                  *GateEvaluator
	SnapshotDB             *gorm.DB
	Observation            WorkerObservation
	PersistSnapshot        func(context.Context, WorkerObservation) error
	HeartbeatSnapshot      func(context.Context, WorkerObservation) error
	// ConsumeRecovery executes one claimed operation through the existing
	// RuntimeHandler/Eino path. It returns operation status, controlled error
	// code/reason, and must not invoke Effects directly.
	ConsumeRecovery       func(context.Context, *workflow.RecoveryOperationClaim) (status, errorCode, reason string, err error)
	ConsumeRecoveryResult func(context.Context, *workflow.RecoveryOperationClaim) (workflow.FinishRecoveryOperationResult, error)
}

// Worker 只委派唯一 workflow.GORMStore，并承载 phase20 唯一 durable poll loop。
type Worker struct {
	store                 *workflow.GORMStore
	config                WorkerConfig
	claimNext             func(context.Context) (*workflow.ClaimedRun, bool, error)
	heartbeat             func(context.Context, workflow.LeaseToken) error
	execute               func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error)
	transition            func(context.Context, workflow.RunTransition) error
	complete              func(context.Context, workflow.CompleteRunInput) error
	projectRevision       func(context.Context, string, []byte) error
	expireApprovals       func(context.Context, string, int) (int, error)
	reconciler            *effects.Reconciler
	retention             *RetentionCoordinator
	snapshotMu            sync.Mutex
	observation           WorkerObservation
	persistSnapshot       func(context.Context, WorkerObservation) error
	heartbeatSnapshot     func(context.Context, WorkerObservation) error
	consumeRecovery       func(context.Context, *workflow.RecoveryOperationClaim) (string, string, string, error)
	consumeRecoveryResult func(context.Context, *workflow.RecoveryOperationClaim) (workflow.FinishRecoveryOperationResult, error)
	snapshotStarted       bool
	cancelMu              sync.Mutex
	activeCancel          *workflow.RecoveryOperationClaim
}

// RunExecutionResult 是 Worker 交给唯一完成 primitive 的基础结果。
type RunExecutionResult struct {
	OutputPayload      string
	RevisionStateJSON  []byte
	TraceQuality       string
	TraceID            string
	TraceBarrier       AttemptTraceBarrier
	RunTransitioned    bool
	CancelOperation    *workflow.RecoveryOperationClaim
	OperationFinalized bool
}

// AttemptTraceBarrier 是现有 MySQL Trace Attempt-scoped barrier 的最薄消费契约。
type AttemptTraceBarrier interface {
	Finish(error)
	Flush(context.Context) string
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
	if config.ApprovalExpiryBatch < 0 || config.ApprovalExpiryBatch > 1000 {
		return nil, fmt.Errorf("approval expiry batch must be between 0 and 1000")
	}
	if config.ApprovalExpiryBatch == 0 {
		config.ApprovalExpiryBatch = 100
	}
	if config.ReconciliationBatch < 0 || config.ReconciliationBatch > 1000 {
		return nil, fmt.Errorf("reconciliation batch must be between 0 and 1000")
	}
	if config.ReconciliationBatch == 0 {
		config.ReconciliationBatch = 100
	}
	if config.Gates != nil && config.Execute != nil && strings.TrimSpace(config.RuntimeVersion) == "" {
		return nil, fmt.Errorf("release-controlled Worker requires an exact runtime version")
	}
	worker := &Worker{
		store: store, config: config, claimNext: config.ClaimNext, execute: config.Execute,
		heartbeat: config.Heartbeat, transition: config.Transition, complete: config.Complete, projectRevision: config.ProjectRevision,
		retention: config.Retention, observation: config.Observation,
		persistSnapshot: config.PersistSnapshot, heartbeatSnapshot: config.HeartbeatSnapshot,
		consumeRecovery:       config.ConsumeRecovery,
		consumeRecoveryResult: config.ConsumeRecoveryResult,
	}
	if worker.observation.WorkerID == "" && config.SnapshotDB != nil {
		worker.observation.WorkerID = config.Owner
	}
	if config.SnapshotDB != nil {
		if worker.persistSnapshot == nil {
			worker.persistSnapshot = func(ctx context.Context, observation WorkerObservation) error {
				return PersistWorkerSnapshot(ctx, config.SnapshotDB, observation)
			}
		}
		if worker.heartbeatSnapshot == nil {
			worker.heartbeatSnapshot = func(ctx context.Context, observation WorkerObservation) error {
				return HeartbeatWorkerSnapshot(ctx, config.SnapshotDB, observation)
			}
		}
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
	if store != nil {
		worker.expireApprovals = store.ExpireDueApprovals
	}
	if config.QueryEffectTargetState != nil {
		if store == nil {
			return nil, fmt.Errorf("workflow Store is required for Effect reconciliation")
		}
		reconciler, err := effects.NewReconciler(store)
		if err != nil {
			return nil, err
		}
		worker.reconciler = reconciler
	}
	return worker, nil
}

// ClaimNext 尝试认领一个 Run；空结果由调用方使用 NextPollBackoff 退避。
func (w *Worker) ClaimNext(ctx context.Context) (*workflow.ClaimedRun, bool, error) {
	if w.config.Gates != nil {
		current, err := w.config.Gates.Current(ctx)
		if err != nil {
			return nil, false, err
		}
		if !current.Enabled(GateAgentRuntimeEnabled) {
			return nil, false, nil
		}
	}
	return w.store.ClaimNextRun(ctx, workflow.ClaimInput{
		Owner: w.config.Owner, LeaseDuration: w.config.LeaseDuration, RuntimeVersion: w.config.RuntimeVersion,
	})
}

// RunOnce 认领并执行一个 Run；API/SSE 生命周期不参与此调用。
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil {
		return false, fmt.Errorf("durable Worker is required")
	}
	if err := w.ensureSnapshot(ctx); err != nil {
		return false, err
	}
	retained := false
	if w.retention != nil {
		var err error
		retained, _, err = w.retention.RunOnce(ctx)
		if err != nil {
			return false, err
		}
	}
	if w.execute == nil && w.consumeRecovery == nil && w.consumeRecoveryResult == nil {
		return retained, nil
	}
	expired := 0
	if w.expireApprovals != nil {
		var err error
		expired, err = w.expireApprovals(ctx, w.config.Owner, w.config.ApprovalExpiryBatch)
		if err != nil {
			return false, err
		}
	}
	reconciled, err := w.reconcileEffects(ctx)
	if err != nil {
		return false, err
	}
	if didRecovery, recoveryErr := w.consumeRecoveryOperation(ctx); didRecovery {
		return true, recoveryErr
	}
	if w.execute == nil {
		return retained || expired > 0 || reconciled, nil
	}
	claimed, ok, err := w.claimNext(ctx)
	if err != nil || !ok {
		return ok || retained || expired > 0 || reconciled, err
	}
	if err := w.refreshSnapshot(ctx, WorkerStatusRunning, claimed.Run.ID, claimed.Token.Generation, ""); err != nil {
		return true, err
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
	w.cancelMu.Lock()
	result.CancelOperation = w.activeCancel
	w.activeCancel = nil
	w.cancelMu.Unlock()
	if result.TraceBarrier != nil {
		traceErr := executionErr
		if heartbeatErr != nil {
			traceErr = heartbeatErr
		}
		result.TraceBarrier.Finish(traceErr)
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
		result.TraceQuality = result.TraceBarrier.Flush(flushCtx)
		cancel()
	}
	if heartbeatErr != nil {
		_ = w.refreshSnapshot(context.WithoutCancel(ctx), WorkerStatusRunning, claimed.Run.ID, claimed.Token.Generation, heartbeatErr.Error())
		return true, heartbeatErr
	}
	if result.RunTransitioned {
		if result.CancelOperation != nil && executionErr == nil {
			completionCtx := context.WithoutCancel(runCtx)
			err := w.completeAndProject(completionCtx, claimed.Run.ID, workflow.CompleteRunInput{
				RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: workflow.RunStatusCanceled,
				Lease: claimed.Token, ErrorMessage: "cancel requested", TraceQuality: result.TraceQuality, TraceID: result.TraceID,
				OperationID: result.CancelOperation.Operation.OperationID, OperationAction: result.CancelOperation.Operation.Action,
				OperationReason: "cancel_safe_point",
			})
			_ = w.refreshSnapshot(context.WithoutCancel(ctx), WorkerStatusIdle, "", 0, errorText(err))
			return true, err
		}
		if result.CancelOperation != nil && executionErr != nil && w.store != nil {
			// A failed safe-point request must close the command as failed while
			// leaving the Run available for a later explicit cancellation retry.
			_ = w.store.FailRecoveryOperation(context.WithoutCancel(runCtx), *result.CancelOperation, "cancel_safe_point_failed", "cancel_safe_point_failed")
		}
		_ = w.refreshSnapshot(context.WithoutCancel(ctx), WorkerStatusIdle, "", 0, errorText(executionErr))
		return true, executionErr
	}
	if executionErr == nil {
		err = w.completeAndProject(context.WithoutCancel(runCtx), claimed.Run.ID, workflow.CompleteRunInput{
			RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning,
			TargetStatus: workflow.RunStatusSucceeded, Lease: claimed.Token,
			OutputPayload: result.OutputPayload, RevisionStateJSON: result.RevisionStateJSON,
			TraceQuality: result.TraceQuality, TraceID: result.TraceID,
		})
		if err == nil {
			_ = w.refreshSnapshot(context.WithoutCancel(ctx), WorkerStatusIdle, "", 0, "")
		}
		return true, err
	}

	var classified *classifiedExecutionError
	if errors.As(executionErr, &classified) && classified.parkReason != "" {
		err = w.transition(context.WithoutCancel(runCtx), workflow.RunTransition{
			RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning,
			TargetStatus: workflow.RunStatusParked, ParkReason: classified.parkReason, Lease: claimed.Token,
			Event: workflow.WorkflowEventInput{Type: workflow.EventRunParked, TraceID: result.TraceID,
				Payload: workflow.EventPayload{Attributes: map[string]any{"park_reason": classified.parkReason}}},
		})
		if err == nil {
			_ = w.refreshSnapshot(context.WithoutCancel(ctx), WorkerStatusIdle, "", 0, executionErr.Error())
		}
		return true, err
	}
	if errors.As(executionErr, &classified) && classified.retryable && claimed.Run.Attempt < claimed.Run.MaxAttempts {
		err = w.transition(context.WithoutCancel(runCtx), workflow.RunTransition{
			RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning,
			TargetStatus: workflow.RunStatusRetryableFailed, AvailableAt: time.Now().Add(w.retryBackoff(claimed.Run.Attempt)),
			Lease: claimed.Token, Event: workflow.WorkflowEventInput{Type: workflow.EventRunFailed, TraceID: result.TraceID,
				Payload: workflow.EventPayload{Attributes: map[string]any{"retryable": true, "attempt": claimed.Run.Attempt}}},
		})
		if err == nil {
			_ = w.refreshSnapshot(context.WithoutCancel(ctx), WorkerStatusIdle, "", 0, executionErr.Error())
		}
		return true, err
	}
	target := workflow.RunStatusFailed
	if errors.Is(executionErr, context.Canceled) {
		target = workflow.RunStatusCanceled
	}
	err = w.completeAndProject(context.WithoutCancel(runCtx), claimed.Run.ID, workflow.CompleteRunInput{
		RunID: claimed.Run.ID, ExpectedStatus: workflow.RunStatusRunning, TargetStatus: target,
		Lease: claimed.Token, ErrorMessage: executionErr.Error(), TraceQuality: result.TraceQuality, TraceID: result.TraceID,
	})
	if err == nil {
		_ = w.refreshSnapshot(context.WithoutCancel(ctx), WorkerStatusIdle, "", 0, executionErr.Error())
	}
	return true, err
}

// consumeRecoveryOperation polls at most one accepted/reclaimable command per
// loop iteration. HTTP never reaches this path; Worker owns execution and
// terminal operation persistence.
func (w *Worker) consumeRecoveryOperation(ctx context.Context) (bool, error) {
	if w == nil || w.store == nil {
		return false, nil
	}
	claim, ok, err := w.store.ClaimNextRecoveryOperation(ctx, w.config.Owner, w.config.LeaseDuration)
	if err != nil || !ok {
		return ok, err
	}
	recoveryCtx, _, _, identityErr := claimedRunIdentityContext(ctx, claim.Run)
	if identityErr != nil {
		return true, identityErr
	}
	if err := w.store.StartRecoveryOperation(recoveryCtx, *claim); err != nil {
		return true, err
	}
	if claim.Operation.Action == workflow.OperationActionRestore {
		return true, w.store.RestoreRecoveryOperation(recoveryCtx, *claim)
	}
	if w.consumeRecoveryResult != nil {
		result, execErr := w.consumeRecoveryResult(recoveryCtx, claim)
		if result.OperationFinalized {
			return true, nil
		}
		if execErr != nil && claim.Operation.Action == workflow.OperationActionCancel && claim.Run.Status == workflow.RunStatusRunning {
			// Active Run heartbeat owns the safe-point and will finish this
			// started operation after the Eino executor drains.
			return true, nil
		}
		if execErr != nil && result.Status == "" {
			result.Status = workflow.OperationStatusFailed
			if result.ErrorCode == "" {
				result.ErrorCode = "recovery_execution_failed"
			}
			result.ErrorMessage = execErr.Error()
		}
		if result.RunTransitioned {
			return true, w.store.FailRecoveryOperationAfterTransition(context.WithoutCancel(recoveryCtx), *claim, "recovery_transition_unclosed", "recovery_transition_unclosed")
		}
		if result.Status == "" {
			result.Status = workflow.OperationStatusSucceeded
		}
		return true, w.store.FinishRecoveryOperationWithResult(recoveryCtx, *claim, result)
	}
	if w.consumeRecovery == nil {
		return true, w.store.FinishRecoveryOperation(recoveryCtx, *claim, workflow.OperationStatusFailed, "recovery_executor_unconfigured", "executor_unconfigured")
	}
	status, errorCode, reason, execErr := w.consumeRecovery(recoveryCtx, claim)
	if execErr != nil && status == "" {
		status = workflow.OperationStatusFailed
		if errorCode == "" {
			errorCode = "recovery_execution_failed"
		}
	}
	if status == "" {
		status = workflow.OperationStatusSucceeded
	}
	return true, w.store.FinishRecoveryOperation(recoveryCtx, *claim, status, errorCode, reason)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (w *Worker) completeAndProject(ctx context.Context, runID string, input workflow.CompleteRunInput) error {
	if err := w.complete(ctx, input); err != nil {
		return err
	}
	if w.projectRevision != nil && input.TargetStatus == workflow.RunStatusSucceeded && len(input.RevisionStateJSON) > 0 {
		_ = w.projectRevision(ctx, runID, append([]byte(nil), input.RevisionStateJSON...))
	}
	return nil
}

func (w *Worker) reconcileEffects(ctx context.Context) (bool, error) {
	if w.reconciler == nil {
		return false, nil
	}
	reaped, err := w.reconciler.ReapExpired(ctx, w.config.ReconciliationBatch)
	if err != nil {
		return false, err
	}
	if w.config.Gates != nil {
		current, gateErr := w.config.Gates.Current(ctx)
		if gateErr != nil {
			return reaped > 0, gateErr
		}
		if !current.Enabled(GateAgentRuntimeEnabled) {
			return reaped > 0, nil
		}
	}
	claim, ok, err := w.reconciler.Claim(ctx, workflow.ReconciliationClaimInput{
		Owner: w.config.Owner, LeaseDuration: w.config.LeaseDuration, RuntimeVersion: w.config.RuntimeVersion,
	})
	if err != nil || !ok {
		return reaped > 0, err
	}
	runCtx, _, _, err := claimedRunIdentityContext(ctx, claim.Run)
	if err != nil {
		return true, err
	}
	resolution, heartbeatErr := w.queryReconciliationWithHeartbeat(runCtx, claim)
	if heartbeatErr != nil {
		return true, heartbeatErr
	}
	return true, w.reconciler.Resolve(runCtx, resolution)
}

func (w *Worker) queryReconciliationWithHeartbeat(ctx context.Context, claim *workflow.ReconciliationClaim) (workflow.ResolveEffectInput, error) {
	var resolution workflow.ResolveEffectInput
	var queryErr error
	heartbeatErr := w.withLeaseHeartbeat(ctx, claim.Token, func(queryCtx context.Context) {
		query := w.config.QueryEffectTargetState
		if w.config.Gates != nil {
			allowed, gateErr := w.reconciliationGateAllowed(queryCtx, claim)
			if gateErr != nil || !allowed {
				query = func(context.Context, effects.ReconciliationTarget) (effects.TargetState, error) {
					if gateErr != nil {
						return effects.TargetState{Known: false, Evidence: map[string]any{"gate": "unavailable"}}, gateErr
					}
					return effects.TargetState{Known: false, Evidence: map[string]any{"gate": "closed"}}, effects.ErrEffectGateClosed
				}
			}
		}
		resolution, queryErr = w.reconciler.Query(queryCtx, claim, query, w.config.Owner)
	})
	if heartbeatErr != nil {
		return workflow.ResolveEffectInput{}, heartbeatErr
	}
	return resolution, queryErr
}

func (w *Worker) reconciliationGateAllowed(ctx context.Context, claim *workflow.ReconciliationClaim) (bool, error) {
	if claim == nil || w.config.Gates == nil {
		return false, fmt.Errorf("reconciliation claim and Gate evaluator are required")
	}
	snapshot, err := RuntimeSnapshotFromRun(claim.Run)
	if err != nil {
		return false, err
	}
	effective, err := w.config.Gates.Effective(ctx, snapshot)
	if err != nil {
		return false, err
	}
	if !effective.Enabled(GateAgentRuntimeEnabled) {
		return false, nil
	}
	entry, err := policy.LookupCatalog(claim.Effect.ToolName)
	if err != nil {
		return false, err
	}
	gate := GateAgentRuntimeL1Writes
	if entry.Risk == policy.RiskL2 {
		gate = GateAgentRuntimeL2Writes
	}
	return effective.Enabled(gate), nil
}

// ExpireDueApprovals 复用同一 durable poll loop 执行一次有界到期扫描。
func (w *Worker) ExpireDueApprovals(ctx context.Context) (int, error) {
	if w == nil || w.expireApprovals == nil {
		return 0, fmt.Errorf("approval expiry scan is not configured")
	}
	return w.expireApprovals(ctx, w.config.Owner, w.config.ApprovalExpiryBatch)
}

func (w *Worker) executeWithHeartbeat(ctx context.Context, claimed *workflow.ClaimedRun) (RunExecutionResult, error, error) {
	var result RunExecutionResult
	var executionErr error
	heartbeatErr := w.withLeaseHeartbeat(ctx, claimed.Token, func(executionCtx context.Context) {
		callCtx := executionCtx
		if w.store != nil {
			// Agent 执行保留 frozen identity/value，并由官方 Eino cancel 完成 safe-point；
			// lifecycle context 单独传入，避免 SIGTERM 先取消 Runner 而来不及提交 Checkpoint。
			callCtx = context.WithValue(context.WithoutCancel(executionCtx), workerLifecycleContextKey{}, executionCtx)
		}
		result, executionErr = w.execute(callCtx, claimed)
	})
	return result, executionErr, heartbeatErr
}

func (w *Worker) withLeaseHeartbeat(ctx context.Context, token workflow.LeaseToken, call func(context.Context)) error {
	if w.heartbeat == nil {
		call(ctx)
		return nil
	}
	leaseCtx, cancel := context.WithCancelCause(ctx)
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
			case <-leaseCtx.Done():
				heartbeatResult <- nil
				return
			case <-ticker.C:
				if err := w.heartbeat(leaseCtx, token); err != nil {
					cancel(err)
					heartbeatResult <- err
					return
				}
				if w.store != nil {
					requested, cancelErr := w.store.CancelRequested(leaseCtx, token)
					if cancelErr != nil {
						cancel(cancelErr)
						heartbeatResult <- cancelErr
						return
					}
					if requested {
						// Keep heartbeating until the cancel command itself has a
						// fenced started Event. A transient claim/start failure must
						// never cancel the Eino Runner without an operation identity.
						cancelClaim, claimOK, claimErr := w.store.ClaimNextRecoveryOperation(leaseCtx, token.Owner, w.config.LeaseDuration)
						if claimErr != nil || !claimOK || cancelClaim == nil || cancelClaim.Operation.Action != workflow.OperationActionCancel {
							continue
						}
						if startErr := w.store.StartRecoveryOperation(leaseCtx, *cancelClaim); startErr != nil {
							continue
						}
						w.cancelMu.Lock()
						w.activeCancel = cancelClaim
						w.cancelMu.Unlock()
						cancel(workflow.ErrCancelRequested)
						heartbeatResult <- nil
						return
					}
				}
				if err := w.heartbeatCurrentSnapshot(leaseCtx); err != nil {
					cancel(err)
					heartbeatResult <- err
					return
				}
			}
		}
	}()
	call(leaseCtx)
	close(stop)
	cancel(nil)
	return <-heartbeatResult
}

func workerLifecycleContext(ctx context.Context) context.Context {
	if lifecycle, ok := ctx.Value(workerLifecycleContextKey{}).(context.Context); ok && lifecycle != nil {
		return lifecycle
	}
	return ctx
}

// Run 持续复用同一 MySQL claim loop；没有进程内 Queue 或第二个 Scheduler。
func (w *Worker) Run(ctx context.Context) error {
	defer func() {
		drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = w.refreshCurrentSnapshot(drainCtx, WorkerStatusDraining)
	}()
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

func (w *Worker) ensureSnapshot(ctx context.Context) error {
	if w.persistSnapshot == nil || strings.TrimSpace(w.observation.WorkerID) == "" {
		return nil
	}
	w.snapshotMu.Lock()
	status, runID, generation, lastError := w.observation.Status, w.observation.ActiveRunID, w.observation.ActiveGeneration, w.observation.LastError
	if status == "" {
		status = WorkerStatusIdle
	}
	w.snapshotMu.Unlock()
	return w.refreshSnapshot(ctx, status, runID, generation, lastError)
}

func (w *Worker) refreshCurrentSnapshot(ctx context.Context, status string) error {
	w.snapshotMu.Lock()
	runID, generation, lastError := w.observation.ActiveRunID, w.observation.ActiveGeneration, w.observation.LastError
	w.snapshotMu.Unlock()
	return w.refreshSnapshot(ctx, status, runID, generation, lastError)
}

func (w *Worker) refreshSnapshot(ctx context.Context, status, runID string, generation uint64, lastError string) error {
	if w.persistSnapshot == nil || strings.TrimSpace(w.observation.WorkerID) == "" {
		return nil
	}
	w.snapshotMu.Lock()
	w.observation.HeartbeatAt = time.Now()
	w.observation.Status = status
	w.observation.ActiveRunID = runID
	w.observation.ActiveGeneration = generation
	w.observation.LastError = lastError
	observation := w.observation
	started := w.snapshotStarted
	w.snapshotMu.Unlock()
	var err error
	if !started {
		err = w.persistSnapshot(ctx, observation)
	} else if w.heartbeatSnapshot != nil {
		err = w.heartbeatSnapshot(ctx, observation)
	} else {
		err = w.persistSnapshot(ctx, observation)
	}
	if err == nil {
		w.snapshotMu.Lock()
		w.snapshotStarted = true
		w.snapshotMu.Unlock()
	}
	return err
}

func (w *Worker) heartbeatCurrentSnapshot(ctx context.Context) error {
	w.snapshotMu.Lock()
	status, runID, generation, lastError := w.observation.Status, w.observation.ActiveRunID, w.observation.ActiveGeneration, w.observation.LastError
	w.snapshotMu.Unlock()
	return w.refreshSnapshot(ctx, status, runID, generation, lastError)
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
