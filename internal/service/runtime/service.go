package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

// RuntimeService is the read-only aggregate service for durable runs.
type RuntimeService struct {
	Store *workflow.GORMStore
	Gates *airuntime.GateEvaluator
}

func NewRuntimeService(store *workflow.GORMStore) *RuntimeService {
	return &RuntimeService{Store: store}
}

func NewRuntimeServiceWithEvaluator(store *workflow.GORMStore, gates *airuntime.GateEvaluator) *RuntimeService {
	return &RuntimeService{Store: store, Gates: gates}
}

func BuildBudgetDTO(limitsJSON, usageJSON, reservationsJSON *string) v1.RuntimeBudgetDTO {
	var l workflow.BaseBudgetLimits
	var u workflow.BaseBudgetUsage
	var rs workflow.BaseBudgetReservations
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	valid := true
	if limitsJSON == nil || json.Unmarshal([]byte(*limitsJSON), &l) != nil || l.MaxModelCalls <= 0 || l.MaxL0ToolCalls <= 0 || l.MaxDurationMS <= 0 {
		valid = false
	}
	if usageJSON == nil || json.Unmarshal([]byte(*usageJSON), &u) != nil || u.Schema != workflow.BaseBudgetSchema || u.ModelCalls < 0 || u.L0ToolCalls < 0 {
		valid = false
	}
	if reservationsJSON == nil || json.Unmarshal([]byte(*reservationsJSON), &rs) != nil || rs.Schema != workflow.BaseBudgetSchema || rs.Items == nil {
		valid = false
	}
	if !valid {
		meta.Availability = v1.AvailabilityPartial
		meta.DataQuality = v1.DataQualityUnknown
		meta.ReasonCode = "invalid_budget_json"
	}
	toInt := func(v int64) *int { x := int(v); return &x }
	toI64 := func(v int64) *int64 { return &v }
	toF := func(v float64) *float64 { return &v }
	reservedModel, reservedTool := 0, 0
	exhaustedReason := ""
	for _, x := range rs.Items {
		if x.State == workflow.BaseBudgetReservationPending {
			if x.Kind == workflow.BaseBudgetKindModelCall {
				reservedModel++
			}
			if x.Kind == workflow.BaseBudgetKindL0ToolCall {
				reservedTool++
			}
		}
		if x.State == workflow.BaseBudgetReservationExhausted && exhaustedReason == "" {
			exhaustedReason = x.ExhaustedReason
		}
	}
	r := v1.RuntimeBudgetDTO{MaxModelCalls: toInt(l.MaxModelCalls), MaxL0ToolCalls: toInt(l.MaxL0ToolCalls), MaxDurationMs: toI64(l.MaxDurationMS), ModelCalls: toInt(u.ModelCalls), ToolCalls: toInt(u.L0ToolCalls), Iterations: toInt(u.PlannerRounds + u.ExecutorRounds + u.ReplannerRounds), InputTokens: toI64(u.InputTokens), OutputTokens: toI64(u.OutputTokens), CostCNY: toF(u.CostCNY), MCPCalls: toInt(u.MCPCalls), RAGCalls: toInt(u.RAGDocuments), ReservedModelCalls: toInt(int64(reservedModel)), ReservedToolCalls: toInt(int64(reservedTool)), Exhausted: exhaustedReason != "", ExhaustedReason: exhaustedReason, ResourceMeta: meta}
	return r
}

func BuildContextDTO(s workflow.DurableContextSnapshot, revUsed, revCommitted uint64, includeHistory bool, owner string, ctx context.Context) (v1.ContextDTO, error) {
	if includeHistory {
		if err := AuthorizeRuntimeContent(ctx, owner, ContentKindHistory); err != nil {
			return v1.ContextDTO{}, err
		}
	}
	serverIdentity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return v1.ContextDTO{}, ErrRuntimeForbidden
	}
	scope := serverIdentity.Scope.UserID
	if serverIdentity.Scope.All {
		scope = "all"
	}
	identity := v1.IdentityDTO{UserID: serverIdentity.UserID, Username: serverIdentity.Username, Role: string(serverIdentity.Role), Scope: scope, AuthDisabled: serverIdentity.AuthDisabled}
	count := 0
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	if s.Schema != workflow.DurableContextSnapshotSchema || s.DeadlineAt.IsZero() {
		meta.Availability = v1.AvailabilityPartial
		meta.DataQuality = v1.DataQualityUnknown
		meta.ReasonCode = "invalid_context_snapshot"
	}
	if len(s.History) > 0 {
		var h []any
		if json.Unmarshal(s.History, &h) == nil {
			count = len(h)
		} else {
			meta.Availability, meta.DataQuality, meta.ReasonCode = v1.AvailabilityPartial, v1.DataQualityUnknown, "invalid_context_history"
		}
	}
	hash := sha256.Sum256(s.History)
	bh := sha256.Sum256(s.BudgetLimits)
	return v1.ContextDTO{Identity: identity, SessionRevisionUsed: revUsed, SessionRevisionCommitted: revCommitted, SummaryHash: hex.EncodeToString(hash[:]), HistoryCount: count, BudgetLimitsHash: hex.EncodeToString(bh[:]), DeadlineAt: &s.DeadlineAt, RuntimeCompatibilityHash: "", ResourceMeta: meta}, nil
}

func BuildCompatibilityDTO(run mysql.WorkflowRun, attempt *mysql.WorkflowAttempt, worker *mysql.RuntimeWorkerSnapshot, checkpoints ...*mysql.WorkflowCheckpoint) v1.RuntimeCompatibilityDTO {
	r := v1.RuntimeCompatibilityDTO{ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityPartial, DataQuality: v1.DataQualityPartial}}
	if run.RuntimeCompatibilityHash != nil && *run.RuntimeCompatibilityHash != "" {
		r.RunFingerprint = *run.RuntimeCompatibilityHash
	}
	if attempt != nil && attempt.CheckpointCompatibilityHash != nil && *attempt.CheckpointCompatibilityHash != "" {
		r.CheckpointFingerprint = *attempt.CheckpointCompatibilityHash
	}
	if attempt != nil && attempt.ExecutingWorkerFingerprint != nil && *attempt.ExecutingWorkerFingerprint != "" {
		r.AttemptFingerprint = *attempt.ExecutingWorkerFingerprint
	}
	if worker != nil && worker.RuntimeCompatibilityHash != nil && *worker.RuntimeCompatibilityHash != "" {
		r.ExecutingWorkerFingerprint = *worker.RuntimeCompatibilityHash
		r.WorkerMatch = r.AttemptFingerprint != "" && r.AttemptFingerprint == r.ExecutingWorkerFingerprint
	} else {
		r.ReasonCode = "not_observed"
	}
	if r.RunFingerprint != "" && attempt != nil && attempt.RunCompatibilityHash != nil {
		r.RunMatch = *attempt.RunCompatibilityHash == r.RunFingerprint
	}
	// Checkpoint compatibility is compared against the persisted checkpoint
	// snapshot, never against the run hash (these are independent dimensions).
	if attempt != nil && attempt.CheckpointCompatibilityHash != nil {
		if len(checkpoints) > 0 && checkpoints[0] != nil && checkpoints[0].RuntimeCompatibilityHash != nil {
			r.CheckpointMatch = *attempt.CheckpointCompatibilityHash == *checkpoints[0].RuntimeCompatibilityHash
		} else {
			r.CheckpointMatch = false
			if r.ReasonCode == "" {
				r.ReasonCode = "checkpoint_not_observed"
			}
		}
	}
	r.ExactRestoreAllowed = r.RunMatch && r.CheckpointMatch && r.WorkerMatch
	return r
}

func BuildRunSummary(run mysql.WorkflowRun) v1.RunSummaryDTO {
	p := MapCanonicalStatus(run.Status, run.RuntimeMode)
	phase := CurrentPhaseFromFacts(RunFacts{Status: run.Status, RuntimeMode: run.RuntimeMode}, AttemptFacts{}, EventFacts{})
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: p.DataQuality}
	if !p.Known {
		meta.Availability = v1.AvailabilityPartial
		meta.DataQuality = v1.DataQualityUnknown
		meta.ReasonCode = "unknown_status"
	}
	return v1.RunSummaryDTO{RunID: run.ID, SessionID: run.SessionID, WorkflowKey: run.WorkflowKey, RuntimeMode: run.RuntimeMode, Status: p.Status, CurrentPhase: phase, Agent: run.RuntimeAgent, Attempt: int(run.Attempt), WorkerID: value(run.LeaseOwner), LeaseGeneration: run.LeaseGeneration, LeaseState: leaseState(run), HeartbeatAt: run.HeartbeatAt, ParkReason: value(run.ParkReason), RuntimeVersion: value(run.RuntimeVersion), RuntimeCompatibilityHash: value(run.RuntimeCompatibilityHash), QueryHash: hashText(run.QueryText), Budget: BuildBudgetDTO(run.BudgetLimitsJSON, run.BudgetUsageJSON, run.BudgetReservationsJSON), UsageQuality: v1.DataQuality(run.UsageQuality), TraceQuality: v1.DataQuality(run.TraceQuality), StartedAt: &run.StartedAt, FinishedAt: run.FinishedAt, DurationMs: run.DurationMs, ResourceMeta: meta}
}

func AllowedRecoveryActions(status string, compatibility v1.RuntimeCompatibilityDTO) []v1.RecoveryAction {
	switch status {
	case string(v1.RuntimeStatusParked):
		if compatibility.ExactRestoreAllowed {
			return []v1.RecoveryAction{v1.RecoveryActionRestore, v1.RecoveryActionResume}
		}
		return nil
	case string(v1.RuntimeStatusRetryableFailed):
		actions := []v1.RecoveryAction{v1.RecoveryActionReplay, v1.RecoveryActionCancel}
		if compatibility.ExactRestoreAllowed {
			actions = append([]v1.RecoveryAction{v1.RecoveryActionResume}, actions...)
		}
		return actions
	case string(v1.RuntimeStatusWaitingApproval):
		return []v1.RecoveryAction{v1.RecoveryActionCancel}
	case string(v1.RuntimeStatusRunning), string(v1.RuntimeStatusPending):
		return []v1.RecoveryAction{v1.RecoveryActionCancel}
	default:
		return nil
	}
}

func (s *RuntimeService) GetRun(ctx context.Context, runID string) (v1.RunDetailDTO, error) {
	if s == nil || s.Store == nil {
		return v1.RunDetailDTO{}, ErrRuntimeNotFound
	}
	run, err := s.Store.GetRuntimeRun(ctx, runID)
	if err != nil {
		return v1.RunDetailDTO{}, MapRecoveryError(err)
	}
	if err = AuthorizeRun(ctx, run.UserID); err != nil {
		return v1.RunDetailDTO{}, err
	}
	sum := BuildRunSummary(*run)
	db := s.Store.DB()
	var attempt mysql.WorkflowAttempt
	var worker mysql.RuntimeWorkerSnapshot
	var event mysql.WorkflowEvent
	var checkpoint mysql.WorkflowCheckpoint
	_ = db.WithContext(ctx).Where("run_id = ?", runID).Order("attempt DESC").First(&attempt).Error
	_ = db.WithContext(ctx).Where("run_id = ?", runID).Order("seq DESC").First(&event).Error
	_ = db.WithContext(ctx).Where("run_id = ?", runID).Order("committed_at DESC, created_at DESC").First(&checkpoint).Error
	if run.LeaseOwner != nil {
		_ = db.WithContext(ctx).Where("worker_id = ?", *run.LeaseOwner).First(&worker).Error
	}
	sum.CurrentPhase = CurrentPhaseFromFacts(RunFacts{Status: run.Status, RuntimeMode: run.RuntimeMode}, AttemptFacts{Active: attempt.Status != nil && *attempt.Status == workflow.RunStatusRunning, Recovery: attempt.Mode != nil && isRecoveryMode(*attempt.Mode), Mode: value(attempt.Mode), OperationActive: attempt.OperationID != nil}, EventFacts{Type: event.EventType})
	var snap workflow.DurableContextSnapshot
	if run.ContextSnapshotJSON != nil {
		_ = json.Unmarshal([]byte(*run.ContextSnapshotJSON), &snap)
	}
	c, _ := BuildContextDTO(snap, valueU64(run.SessionRevision), valueU64(run.SessionRevision), false, run.UserID, ctx)
	var checkpointPtr *mysql.WorkflowCheckpoint
	if checkpoint.ID != "" {
		checkpointPtr = &checkpoint
	}
	comp := BuildCompatibilityDTO(*run, &attempt, &worker, checkpointPtr)
	if attempt.ID == "" {
		comp.ResourceMeta = v1.ResourceMeta{Availability: v1.AvailabilityPartial, DataQuality: v1.DataQualityUnknown, ReasonCode: "attempt_not_observed"}
	}
	gate := v1.RuntimeGateSummaryDTO{ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: "not_observed"}}
	if s.Gates != nil {
		if gs, e := s.Gates.CurrentState(ctx); e == nil {
			keys := airuntime.CanonicalGateKeys()
			for _, k := range keys {
				if gs.StaticCaps.Enabled(k) {
					gate.StaticCaps = append(gate.StaticCaps, k)
				}
				if gs.DynamicCaps.Enabled(k) {
					gate.DynamicCaps = append(gate.DynamicCaps, k)
				}
				if gs.CurrentEffective.Enabled(k) {
					gate.EffectiveCaps = append(gate.EffectiveCaps, k)
				}
			}
			gate.ShadowMode = gs.CurrentEffective.Enabled(airuntime.GateAgentRuntimeShadowMode)
			gate.L1WriteAllowed = gs.CurrentEffective.Enabled(airuntime.GateAgentRuntimeL1Writes)
			gate.L2WriteAllowed = gs.CurrentEffective.Enabled(airuntime.GateAgentRuntimeL2Writes)
			gate.ResourceMeta = v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
		}
	}
	var currentAttempt *v1.AttemptDTO
	if attempt.ID != "" {
		status := v1.RuntimeStatus(value(attempt.Status))
		currentAttempt = &v1.AttemptDTO{AttemptID: attempt.ID, RunID: attempt.RunID, Attempt: int(attempt.Attempt), Mode: value(attempt.Mode), Status: status, CurrentPhase: sum.CurrentPhase, WorkerID: value(attempt.WorkerID), LeaseGeneration: valueU64(attempt.LeaseGeneration), RuntimeVersion: value(attempt.RuntimeVersion), RunCompatibilityHash: value(attempt.RunCompatibilityHash), CheckpointCompatibilityHash: value(attempt.CheckpointCompatibilityHash), ExecutingWorkerFingerprint: value(attempt.ExecutingWorkerFingerprint), TraceID: value(attempt.TraceID), OperationID: value(attempt.OperationID), RetryCount: int(valueU(attempt.RetryCount)), FailoverCount: int(valueU(attempt.FailoverCount)), FailureCode: value(attempt.FailureCode), FailureMessage: value(attempt.FailureMessageRedacted), UsageQuality: v1.DataQuality(value(attempt.UsageQuality)), TraceQuality: v1.DataQuality(value(attempt.TraceQuality)), StartedAt: attempt.StartedAt, FinishedAt: attempt.FinishedAt, ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}}
	}
	return v1.RunDetailDTO{Summary: sum, Overview: v1.RunOverviewDTO{Status: sum.Status, CurrentPhase: sum.CurrentPhase, ResourceMeta: sum.ResourceMeta}, CurrentAttempt: currentAttempt, Budget: sum.Budget, Compatibility: comp, ContextSummary: v1.RuntimeContextSummaryDTO{Identity: c.Identity, SessionRevisionUsed: c.SessionRevisionUsed, SessionRevisionCommitted: c.SessionRevisionCommitted, SummaryHash: c.SummaryHash, HistoryCount: c.HistoryCount, ResourceMeta: c.ResourceMeta}, GateSummary: gate, AllowedRecoveryActions: AllowedRecoveryActions(string(sum.Status), comp), ResourceMeta: sum.ResourceMeta}, nil
}

func (s *RuntimeService) ListRuns(ctx context.Context, f mysql.RuntimeRunFilter) (v1.ListRunsRes, error) {
	rows, total, err := s.Store.ListRuntimeRuns(ctx, f)
	if err != nil {
		return v1.ListRunsRes{}, err
	}
	out := make([]v1.RunSummaryDTO, len(rows))
	for i := range rows {
		out[i] = BuildRunSummary(rows[i])
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	size := f.PageSize
	if size < 1 {
		size = 50
	}
	return v1.ListRunsRes{Items: out, Page: v1.PageMeta{Page: page, PageSize: size, Total: total, HasNext: int64(page*size) < total}, ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}}, nil
}

func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func valueU64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}
func valueU(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}
func hashText(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func leaseState(r mysql.WorkflowRun) string {
	if r.LeaseOwner == nil {
		return "unassigned"
	}
	if r.LeaseUntil != nil && r.LeaseUntil.Before(time.Now()) {
		return "expired"
	}
	return "leased"
}
