package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	EffectResolutionExecuted        = "executed"
	EffectResolutionNotExecuted     = "not_executed"
	EffectResolutionStillUnknown    = "still_unknown"
	EffectResolutionAcceptedUnknown = "accepted_unknown"
)

var (
	ErrEffectReconciliationDenied = errors.New("effect reconciliation denied")
	ErrEffectResolutionInvalid    = errors.New("invalid effect resolution")
)

// ReconciliationClaimInput 是复用现有 Run lease 的对账认领请求。
type ReconciliationClaimInput struct {
	Owner          string
	LeaseDuration  time.Duration
	RuntimeVersion string
}

// ReconciliationClaim 返回对账 Run、Effect 和新的 generation token。
type ReconciliationClaim struct {
	Run             mysql.WorkflowRun
	Effect          mysql.AgentEffect
	Token           LeaseToken
	PrimaryResponse string
}

// ResolveEffectInput 描述一次 generation/CAS 保护的 Effect 对账结论。
type ResolveEffectInput struct {
	Token             LeaseToken
	EffectID          string
	ExpectedVersion   uint64
	Resolution        string
	ResponseRedacted  string
	ExternalReference string
	EvidenceRedacted  string
	ResolvedBy        string
}

// AdminAcceptUnknownInput 是 admin 明确接受不确定性并取消 Run 的请求。
type AdminAcceptUnknownInput struct {
	RunID            string
	EffectID         string
	Owner            string
	LeaseDuration    time.Duration
	EvidenceRedacted string
	Reason           string
}

// AdminResolveEffectInput 是 admin 对 unknown Effect 提交的明确结论。
type AdminResolveEffectInput struct {
	EffectID          string
	Resolution        string
	ResponseRedacted  string
	ExternalReference string
	EvidenceRedacted  string
	Owner             string
	LeaseDuration     time.Duration
}

// ListUnknownEffects 返回当前身份可见的 unknown Effect；不暴露 request 明文。
func (s *GORMStore) ListUnknownEffects(ctx context.Context, limit int) ([]mysql.AgentEffect, error) {
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("effect list limit must be between 1 and 1000")
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&mysql.AgentEffect{}).
		Joins("JOIN workflow_runs ON workflow_runs.id = agent_effects.run_id").
		Where("agent_effects.status = ? AND workflow_runs.runtime_mode = ? AND workflow_runs.park_reason = ?", EffectStatusUnknown, RuntimeModeDurableV1, ParkReasonEffectUnknown)
	if !identity.Scope.All {
		query = query.Where("workflow_runs.user_id = ?", identity.Scope.UserID)
	}
	var effects []mysql.AgentEffect
	if err := query.Order("agent_effects.created_at ASC, agent_effects.id ASC").Limit(limit).Find(&effects).Error; err != nil {
		return nil, err
	}
	return effects, nil
}

// AdminResolveEffect 复用同一 fenced claim，再提交确定的人工结论。
func (s *GORMStore) AdminResolveEffect(ctx context.Context, input AdminResolveEffectInput) error {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return err
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return err
	}
	if input.EffectID == "" || input.Owner == "" {
		return ErrEffectResolutionInvalid
	}
	if input.Resolution != EffectResolutionExecuted && input.Resolution != EffectResolutionNotExecuted && input.Resolution != EffectResolutionStillUnknown {
		return ErrEffectResolutionInvalid
	}
	var current mysql.AgentEffect
	if err := s.db.WithContext(ctx).Where("id = ? AND status = ?", input.EffectID, EffectStatusUnknown).First(&current).Error; err != nil {
		return ErrEffectStateConflict
	}
	if current.EffectType == string(policy.EffectNonReconciliableExternal) && input.Resolution == EffectResolutionStillUnknown {
		return ErrEffectReconciliationDenied
	}
	claimed, err := s.claimSpecificEffect(ctx, input.EffectID, ReconciliationClaimInput{Owner: input.Owner, LeaseDuration: input.LeaseDuration})
	if err != nil {
		return err
	}
	return s.ResolveEffect(ctx, ResolveEffectInput{
		Token: claimed.Token, EffectID: claimed.Effect.ID, ExpectedVersion: claimed.Effect.Version,
		Resolution: input.Resolution, ResponseRedacted: input.ResponseRedacted,
		ExternalReference: input.ExternalReference, EvidenceRedacted: input.EvidenceRedacted,
		ResolvedBy: identity.UserID,
	})
}

// ReconcileExpiredRunningEffects 将 lease 已过期的 running Effect 原子收敛为
// unknown + parked。它绝不把外部调用重置为 pending。
func (s *GORMStore) ReconcileExpiredRunningEffects(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 || limit > 1000 {
		return 0, fmt.Errorf("reconciliation limit must be between 1 and 1000")
	}
	var reconciled int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		reconciled, err = reconcileExpiredRunningEffectsTx(tx, limit)
		return err
	})
	return reconciled, err
}

func reconcileExpiredRunningEffectsTx(tx *gorm.DB, limit int) (int64, error) {
	var reconciled int64
	var effects []mysql.AgentEffect
	result := tx.Model(&mysql.AgentEffect{}).
		Joins("JOIN workflow_runs ON workflow_runs.id = agent_effects.run_id").
		Where("agent_effects.status = ? AND agent_effects.effect_type <> ? AND workflow_runs.runtime_mode = ? AND workflow_runs.status = ? AND workflow_runs.lease_until IS NOT NULL AND workflow_runs.lease_until <= CURRENT_TIMESTAMP(3) AND agent_effects.lease_generation = workflow_runs.lease_generation", EffectStatusRunning, string(policy.EffectTransactionalDB), RuntimeModeDurableV1, RunStatusRunning).
		Order("workflow_runs.lease_until ASC, agent_effects.id ASC").
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Limit(limit).Find(&effects)
	if result.Error != nil {
		return 0, fmt.Errorf("锁定过期 external Effect: %w", result.Error)
	}
	for index := range effects {
		effect := &effects[index]
		var run mysql.WorkflowRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", effect.RunID).Error; err != nil {
			return 0, err
		}
		if run.Status != RunStatusRunning || run.LeaseUntil == nil || !run.LeaseUntil.Before(time.Now().Add(time.Millisecond)) || run.LeaseGeneration != effect.LeaseGeneration {
			continue
		}
		newGeneration := run.LeaseGeneration + 1
		result := tx.Model(&mysql.AgentEffect{}).
			Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", effect.ID, EffectStatusRunning, effect.Version, run.LeaseGeneration).
			Updates(map[string]any{
				"status": EffectStatusUnknown, "version": gorm.Expr("version + 1"),
				"lease_generation": newGeneration, "finished_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
				"last_error":                   "external Effect owner lease expired before outcome was recorded",
				"resolution_evidence_redacted": `{"classification":"unknown","cause":"lease_expired"}`,
			})
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected != 1 {
			return 0, ErrEffectStateConflict
		}
		if err := finishAttemptAfterLeaseExpiryTx(tx, &run, run.LeaseGeneration, RunStatusParked, "unknown", ParkReasonEffectUnknown, "external Effect owner lease expired before outcome was recorded"); err != nil {
			return 0, err
		}
		if err := updateRunWithoutLease(tx, &run, RunStatusParked, map[string]any{
			"park_reason": ParkReasonEffectUnknown, "lease_owner": nil, "lease_until": nil,
			"heartbeat_at": nil, "lease_generation": newGeneration,
		}); err != nil {
			return 0, err
		}
		if err := insertReconciliationEvent(tx, &run, WorkflowEventInput{Type: EventEffectUnknown, Payload: EventPayload{
			Reference: effect.ID, Attributes: map[string]any{"effect_id": effect.ID, "status": EffectStatusUnknown, "cause": "lease_expired"},
		}}); err != nil {
			return 0, err
		}
		if err := insertReconciliationEvent(tx, &run, WorkflowEventInput{Type: EventRunParked, Payload: EventPayload{
			Reference: effect.ID, Attributes: map[string]any{"park_reason": ParkReasonEffectUnknown, "effect_id": effect.ID},
		}}); err != nil {
			return 0, err
		}
		reconciled++
	}
	return reconciled, nil
}

// ClaimEffectReconciliation 只认领 effect_unknown 且类型可对账的 parked Run。
func (s *GORMStore) ClaimEffectReconciliation(ctx context.Context, input ReconciliationClaimInput) (*ReconciliationClaim, bool, error) {
	return s.claimEffectReconciliation(ctx, input, false)
}

func (s *GORMStore) claimSpecificEffect(ctx context.Context, effectID string, input ReconciliationClaimInput) (*ReconciliationClaim, error) {
	claimed, ok, err := s.claimEffectReconciliation(ctx, input, true, effectID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrEffectStateConflict
	}
	return claimed, nil
}

func (s *GORMStore) claimEffectReconciliation(ctx context.Context, input ReconciliationClaimInput, allowNonReconcilable bool, effectIDs ...string) (*ReconciliationClaim, bool, error) {
	if err := validateLeaseOwner(input.Owner); err != nil {
		return nil, false, err
	}
	if input.LeaseDuration < time.Millisecond || input.LeaseDuration > maxLeaseDuration {
		return nil, false, fmt.Errorf("lease duration must be between 1ms and %s", maxLeaseDuration)
	}
	var claimed *ReconciliationClaim
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		oldGeneration := uint64(0)
		query := applyDurableRuntimeContract(tx.Model(&mysql.WorkflowRun{})).
			Joins("JOIN agent_effects ON agent_effects.run_id = workflow_runs.id").
			Where("workflow_runs.status = ? AND workflow_runs.park_reason = ? AND agent_effects.status = ? AND agent_effects.effect_type <> ? AND agent_effects.lease_generation = workflow_runs.lease_generation", RunStatusParked, ParkReasonEffectUnknown, EffectStatusUnknown, string(policy.EffectTransactionalDB))
		if len(effectIDs) == 0 {
			query = query.Where("agent_effects.next_reconcile_at IS NULL OR agent_effects.next_reconcile_at <= CURRENT_TIMESTAMP(3)")
		}
		if strings.TrimSpace(input.RuntimeVersion) != "" {
			query = query.Where("workflow_runs.runtime_version = ?", input.RuntimeVersion)
		}
		if !allowNonReconcilable {
			query = query.Where("agent_effects.effect_type IN ?", []string{string(policy.EffectReconcilable), string(policy.EffectProviderIdempotent)})
		}
		if len(effectIDs) > 0 {
			query = query.Where("agent_effects.id = ?", effectIDs[0])
		}
		result := query.Order("workflow_runs.available_at ASC, workflow_runs.id ASC, agent_effects.id ASC").Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Limit(1).First(&run)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return result.Error
		}
		oldGeneration = run.LeaseGeneration
		var effect mysql.AgentEffect
		effectQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND status = ? AND lease_generation = ?", run.ID, EffectStatusUnknown, oldGeneration)
		if len(effectIDs) == 0 {
			effectQuery = effectQuery.Where("next_reconcile_at IS NULL OR next_reconcile_at <= CURRENT_TIMESTAMP(3)")
		}
		if !allowNonReconcilable {
			effectQuery = effectQuery.Where("effect_type IN ?", []string{string(policy.EffectReconcilable), string(policy.EffectProviderIdempotent)})
		}
		if len(effectIDs) > 0 {
			effectQuery = effectQuery.Where("id = ?", effectIDs[0])
		}
		if err := effectQuery.Order("created_at ASC, id ASC").First(&effect).Error; err != nil {
			return err
		}
		primaryResponse := ""
		if effect.ParentEffectID != nil {
			var primary mysql.AgentEffect
			if err := tx.Where("id = ? AND run_id = ?", *effect.ParentEffectID, run.ID).First(&primary).Error; err != nil {
				return err
			}
			if primary.Status != EffectStatusSucceeded {
				return ErrEffectStateConflict
			}
			decoded, decodeErr := decodeEffectResponse(primary.ResponseRedacted)
			if decodeErr != nil {
				return decodeErr
			}
			primaryResponse = decoded
		}
		newGeneration := oldGeneration + 1
		result = tx.Model(&mysql.WorkflowRun{}).
			Where("id = ? AND runtime_mode = ? AND status = ? AND park_reason = ? AND lease_generation = ? AND lease_owner IS NULL", run.ID, RuntimeModeDurableV1, RunStatusParked, ParkReasonEffectUnknown, oldGeneration).
			Updates(map[string]any{
				"status": RunStatusReconciling, "lease_owner": input.Owner,
				"lease_until":  gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL ? MICROSECOND)", input.LeaseDuration.Microseconds()),
				"heartbeat_at": gorm.Expr("CURRENT_TIMESTAMP(3)"), "lease_generation": newGeneration,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLeaseLost
		}
		if err := tx.First(&run, "id = ?", run.ID).Error; err != nil {
			return err
		}
		result = tx.Model(&mysql.AgentEffect{}).
			Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", effect.ID, EffectStatusUnknown, effect.Version, oldGeneration).
			Updates(map[string]any{"status": EffectStatusReconciling, "version": gorm.Expr("version + 1"), "lease_generation": newGeneration, "reconciliation_attempts": gorm.Expr("reconciliation_attempts + 1"), "next_reconcile_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		if err := insertReconciliationEvent(tx, &run, WorkflowEventInput{Type: EventRunReconciling, Payload: EventPayload{Reference: effect.ID, Attributes: map[string]any{"effect_id": effect.ID, "park_reason": ParkReasonEffectUnknown}}}); err != nil {
			return err
		}
		if err := insertReconciliationEvent(tx, &run, WorkflowEventInput{Type: EventEffectReconciling, Payload: EventPayload{Reference: effect.ID, Attributes: map[string]any{"effect_id": effect.ID, "status": EffectStatusReconciling}}}); err != nil {
			return err
		}
		effect.Version++
		effect.Status = EffectStatusReconciling
		effect.LeaseGeneration = newGeneration
		claimed = &ReconciliationClaim{
			Run: run, Effect: effect, Token: LeaseToken{RunID: run.ID, Owner: input.Owner, Generation: newGeneration},
			PrimaryResponse: primaryResponse,
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return claimed, claimed != nil, nil
}

// ResolveEffect 将 reconciling Effect 按明确证据收敛，并以同一 lease 更新 Run。
func (s *GORMStore) ResolveEffect(ctx context.Context, input ResolveEffectInput) error {
	if input.Resolution != EffectResolutionExecuted && input.Resolution != EffectResolutionNotExecuted && input.Resolution != EffectResolutionStillUnknown {
		return ErrEffectResolutionInvalid
	}
	if strings.TrimSpace(input.EffectID) == "" || strings.TrimSpace(input.ResolvedBy) == "" {
		return ErrEffectResolutionInvalid
	}
	evidence, err := normalizeExternalEvidence(input.EvidenceRedacted)
	if err != nil {
		return err
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return err
	}
	if err := s.authorizeRunScope(ctx, input.Token.RunID); err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, input.Token, RunStatusReconciling, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		var effect mysql.AgentEffect
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.EffectID).First(&effect).Error; err != nil {
			return err
		}
		if effect.Status != EffectStatusReconciling || effect.Version != input.ExpectedVersion || effect.RunID != run.ID || effect.LeaseGeneration != input.Token.Generation {
			return ErrEffectStateConflict
		}
		if effect.EffectType == string(policy.EffectNonReconciliableExternal) && identity.Role != policy.RoleAdmin {
			return policy.ErrForbidden
		}
		status := EffectStatusUnknown
		if input.Resolution == EffectResolutionExecuted {
			status = EffectStatusSucceeded
		} else if input.Resolution == EffectResolutionNotExecuted {
			status = EffectStatusPending
		}
		updates := map[string]any{"status": status, "version": gorm.Expr("version + 1"), "resolution": input.Resolution, "resolution_evidence_redacted": nullableEffectText(evidence), "resolved_by": input.ResolvedBy, "resolved_at": gorm.Expr("CURRENT_TIMESTAMP(3)"), "last_error": nil}
		if input.Resolution == EffectResolutionExecuted {
			response, err := json.Marshal(policy.NewRedactor().RedactText(input.ResponseRedacted))
			if err != nil {
				return err
			}
			updates["response_redacted"] = string(response)
		}
		if input.ExternalReference != "" {
			updates["external_reference"] = policy.NewRedactor().RedactText(input.ExternalReference)
		}
		if input.Resolution == EffectResolutionStillUnknown {
			updates["next_reconcile_at"] = gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 1 MINUTE)")
		}
		if result := tx.Model(&mysql.AgentEffect{}).Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", effect.ID, EffectStatusReconciling, input.ExpectedVersion, input.Token.Generation).Updates(updates); result.Error != nil {
			return result.Error
		} else if result.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		transitionTarget := map[string]string{EffectResolutionExecuted: RunStatusPending, EffectResolutionNotExecuted: RunStatusPending, EffectResolutionStillUnknown: RunStatusParked}[input.Resolution]
		transitionIntent := TransitionIntentEffectResolved
		if input.Resolution == EffectResolutionStillUnknown {
			transitionIntent = TransitionIntentEffectStillUnknown
		}
		if err := ValidateRunTransition(run.Status, transitionTarget, transitionIntent, ParkReasonEffectUnknown); err != nil {
			return err
		}
		runTarget := RunStatusPending
		intent := TransitionIntentEffectResolved
		if input.Resolution == EffectResolutionStillUnknown {
			runTarget, intent = RunStatusParked, TransitionIntentEffectStillUnknown
		}
		if err := appendFencedReconciliationEvent(tx, run, input.Token, WorkflowEventInput{Type: EventEffectResolved, Payload: EventPayload{Reference: effect.ID, Attributes: map[string]any{"effect_id": effect.ID, "resolution": input.Resolution, "status": status, "resolved_by": input.ResolvedBy}}}, runTarget, intent); err != nil {
			return err
		}
		return nil
	})
}

// AdminAcceptUnknownAndCancel 只记录 admin 证据；Run 终态始终委托 phase08 primitive。
func (s *GORMStore) AdminAcceptUnknownAndCancel(ctx context.Context, input AdminAcceptUnknownInput) error {
	if err := policy.Authorize(ctx, policy.PermissionManageUsersPolicyGates, policy.Resource{}); err != nil {
		return err
	}
	if input.RunID == "" || input.EffectID == "" || input.Owner == "" {
		return ErrEffectResolutionInvalid
	}
	if strings.TrimSpace(input.Reason) == "" {
		return ErrEffectResolutionInvalid
	}
	evidence, err := normalizeExternalEvidence(input.EvidenceRedacted)
	if err != nil {
		return err
	}
	var current mysql.AgentEffect
	if err := s.db.WithContext(ctx).Where("id = ? AND run_id = ? AND status = ?", input.EffectID, input.RunID, EffectStatusUnknown).First(&current).Error; err != nil {
		return ErrEffectStateConflict
	}
	claimed, err := s.claimSpecificEffect(ctx, input.EffectID, ReconciliationClaimInput{Owner: input.Owner, LeaseDuration: input.LeaseDuration})
	if err != nil {
		return err
	}
	if claimed.Run.ID != input.RunID || claimed.Effect.ID != input.EffectID {
		return ErrEffectStateConflict
	}
	if err := s.withFencedRunTransaction(ctx, claimed.Token, RunStatusReconciling, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		result := tx.Model(&mysql.AgentEffect{}).Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", input.EffectID, EffectStatusReconciling, claimed.Effect.Version, claimed.Token.Generation).Updates(map[string]any{"status": EffectStatusUnknown, "version": gorm.Expr("version + 1"), "resolution": EffectResolutionAcceptedUnknown, "resolution_evidence_redacted": nullableEffectText(evidence), "resolved_by": input.Owner, "resolved_at": gorm.Expr("CURRENT_TIMESTAMP(3)"), "last_error": nullableEffectText(policy.NewRedactor().RedactText(input.Reason))})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		event := WorkflowEventInput{Type: EventEffectResolved, Payload: EventPayload{Reference: input.EffectID, Attributes: map[string]any{"effect_id": input.EffectID, "resolution": EffectResolutionAcceptedUnknown, "status": EffectStatusUnknown, "resolved_by": input.Owner}}}
		payload, err := marshalDurableEvent(event)
		if err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusReconciling, claimed.Token, map[string]any{})
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, event, payload)
	}); err != nil {
		return err
	}
	return s.CompleteRunAndCommitSession(ctx, CompleteRunInput{RunID: claimed.Run.ID, ExpectedStatus: RunStatusReconciling, TargetStatus: RunStatusCanceled, Lease: claimed.Token, ErrorMessage: input.Reason, TraceQuality: "complete"})
}

func updateRunWithoutLease(tx *gorm.DB, run *mysql.WorkflowRun, target string, updates map[string]any) error {
	updates["status"] = target
	updates["last_event_seq"] = gorm.Expr("last_event_seq + 1")
	result := tx.Model(&mysql.WorkflowRun{}).Where("id = ? AND runtime_mode = ? AND status = ? AND lease_generation = ?", run.ID, RuntimeModeDurableV1, run.Status, run.LeaseGeneration).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrRunCASConflict
	}
	return tx.First(run, "id = ?", run.ID).Error
}

func insertReconciliationEvent(tx *gorm.DB, run *mysql.WorkflowRun, event WorkflowEventInput) error {
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	result := tx.Model(&mysql.WorkflowRun{}).
		Where("id = ? AND runtime_mode = ? AND status = ? AND lease_generation = ? AND last_event_seq = ?", run.ID, RuntimeModeDurableV1, run.Status, run.LeaseGeneration, run.LastEventSeq).
		Update("last_event_seq", gorm.Expr("last_event_seq + 1"))
	if result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			return result.Error
		}
		return ErrRunCASConflict
	}
	run.LastEventSeq++
	return insertDurableEvent(tx, run.ID, run.LastEventSeq, event, payload)
}

func appendFencedReconciliationEvent(tx *gorm.DB, run *mysql.WorkflowRun, token LeaseToken, event WorkflowEventInput, target, intent string) error {
	parkReason := ParkReasonEffectUnknown
	if err := ValidateRunTransition(run.Status, target, intent, parkReason); err != nil {
		return err
	}
	updates := map[string]any{"status": target}
	if target == RunStatusPending || target == RunStatusParked {
		updates["lease_owner"], updates["lease_until"], updates["heartbeat_at"] = nil, nil, nil
	}
	if target == RunStatusPending {
		updates["park_reason"] = nil
	} else {
		updates["park_reason"] = ParkReasonEffectUnknown
	}
	seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusReconciling, token, updates)
	if err != nil {
		return err
	}
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	return insertDurableEvent(tx, run.ID, seq, event, payload)
}
