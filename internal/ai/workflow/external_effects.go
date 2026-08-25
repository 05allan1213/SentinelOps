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
)

var (
	// ErrExternalDeadlineUnsafe 表示租约安全余量内已没有外部调用窗口。
	ErrExternalDeadlineUnsafe = errors.New("external Effect deadline is outside lease safety margin")
)

// ExternalEffectExecutionInput 保存外部 Effect DAG 每一步共享的冻结授权事实。
type ExternalEffectExecutionInput struct {
	Lease                    LeaseToken
	ApprovalID               string
	ProposalHash             string
	ToolCallIDObserved       string
	ToolName                 string
	ToolRevision             string
	ToolSchemaHash           string
	TargetHash               string
	RequestRedacted          string
	PolicyHash               string
	RuntimeCompatibilityHash string
	EffectType               string
	Attempt                  uint
	TraceID                  string
	GateAllowed              bool
	Derived                  []DerivedEffectInput
}

// StartExternalEffectInput 描述即将调用原 endpoint 的稳定 Effect step 与时间边界。
type StartExternalEffectInput struct {
	Execution         ExternalEffectExecutionInput
	EffectStep        string
	AttemptDeadline   time.Time
	LeaseSafetyMargin time.Duration
}

// StartExternalEffectResult 返回 running Effect、严格小于 lease 的调用 deadline 与复用结果。
type StartExternalEffectResult struct {
	Effect       mysql.AgentEffect
	CallDeadline time.Time
	Response     string
	Reused       bool
}

// FinishExternalEffectInput 保存外部 endpoint 返回后的 fenced 状态转换内容。
type FinishExternalEffectInput struct {
	Execution         ExternalEffectExecutionInput
	EffectID          string
	ExpectedVersion   uint64
	ResponseRedacted  string
	ExternalReference string
	LastErrorRedacted string
	EvidenceRedacted  string
}

// EnsureExternalEffectDAG 原子创建或验证一个 external Primary 与 Catalog 固定 derived 行。
func (s *GORMStore) EnsureExternalEffectDAG(ctx context.Context, input ExternalEffectExecutionInput) ([]mysql.AgentEffect, error) {
	normalizedRequest, err := normalizeRedactedRequest(input.RequestRedacted)
	if err != nil {
		return nil, err
	}
	input.RequestRedacted = normalizedRequest
	transition := externalTransitionInput(input)
	if err := validateExternalExecutionInput(input); err != nil {
		return nil, err
	}
	if input.EffectType == string(policy.EffectTransactionalDB) {
		return nil, fmt.Errorf("transactional Primary DAG must be created by TransitionEffectWithEvent")
	}
	if err := validateExternalAuthorization(ctx, s, input); err != nil {
		return nil, err
	}
	var rows []mysql.AgentEffect
	err = s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if err := validateExternalRunAndApproval(tx, run, input); err != nil {
			return err
		}
		primaryKey, err := policy.EffectKey(run.ID, input.ProposalHash, EffectStepPrimary)
		if err != nil {
			return err
		}
		primary, found, err := lockEffect(tx, primaryKey)
		if err != nil {
			return err
		}
		if found {
			if err := validatePrimaryEffectIdentity(&primary, transition, primaryKey); err != nil {
				return err
			}
			if err := validateDerivedEffects(tx, &primary, transition); err != nil {
				return err
			}
		} else {
			primary = newPrimaryEffect(run, transition, primaryKey)
			if err := tx.Create(&primary).Error; err != nil {
				return fmt.Errorf("创建 external Primary Effect: %w", err)
			}
			if err := createDerivedEffects(tx, run, &primary, transition); err != nil {
				return err
			}
		}
		rows = append(rows, primary)
		for _, derived := range input.Derived {
			key, err := policy.EffectKey(run.ID, input.ProposalHash, derived.Step)
			if err != nil {
				return err
			}
			var row mysql.AgentEffect
			if err := tx.First(&row, "id = ?", key).Error; err != nil {
				return fmt.Errorf("读取 derived Effect %q: %w", derived.Step, err)
			}
			rows = append(rows, row)
		}
		return nil
	})
	return rows, err
}

// StartExternalEffect 在调用前最后一次重验 lease、Approval、Policy、Gate、父 Effect 与 deadline。
func (s *GORMStore) StartExternalEffect(ctx context.Context, input StartExternalEffectInput) (StartExternalEffectResult, error) {
	execution := input.Execution
	normalizedRequest, err := normalizeRedactedRequest(execution.RequestRedacted)
	if err != nil {
		return StartExternalEffectResult{}, err
	}
	execution.RequestRedacted = normalizedRequest
	input.Execution = execution
	if err := validateExternalExecutionInput(execution); err != nil {
		return StartExternalEffectResult{}, err
	}
	if strings.TrimSpace(input.EffectStep) == "" || input.LeaseSafetyMargin <= 0 {
		return StartExternalEffectResult{}, ErrExternalDeadlineUnsafe
	}
	if err := validateExternalAuthorization(ctx, s, execution); err != nil {
		return StartExternalEffectResult{}, err
	}
	var started StartExternalEffectResult
	err = s.withFencedRunTransaction(ctx, execution.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if err := validateExternalRunAndApproval(tx, run, execution); err != nil {
			return err
		}
		key, err := policy.EffectKey(run.ID, execution.ProposalHash, input.EffectStep)
		if err != nil {
			return err
		}
		effect, found, err := lockEffect(tx, key)
		if err != nil {
			return err
		}
		if !found || effect.ID != key || effect.IdempotencyKey != key || effect.RunID != run.ID ||
			effect.ProposalHash != execution.ProposalHash || effect.ToolName != execution.ToolName ||
			effect.ToolRevision != execution.ToolRevision || effect.ToolSchemaHash != execution.ToolSchemaHash ||
			effect.TargetHash != execution.TargetHash || effect.EffectStep != input.EffectStep ||
			effect.EffectType != externalStepType(execution, input.EffectStep) {
			return ErrEffectIdentityMismatch
		}
		if effect.EffectRole == EffectRolePrimary && (effect.ParentEffectID != nil || input.EffectStep != EffectStepPrimary) {
			return ErrEffectIdentityMismatch
		}
		if effect.EffectRole == EffectRoleDerived {
			primaryKey, err := policy.EffectKey(run.ID, execution.ProposalHash, EffectStepPrimary)
			if err != nil || effect.ParentEffectID == nil || *effect.ParentEffectID != primaryKey || input.EffectStep == EffectStepPrimary {
				return ErrEffectIdentityMismatch
			}
		} else if effect.EffectRole != EffectRolePrimary {
			return ErrEffectIdentityMismatch
		}
		if effect.Status == EffectStatusSucceeded {
			response, err := decodeEffectResponse(effect.ResponseRedacted)
			if err != nil {
				return err
			}
			started = StartExternalEffectResult{Effect: effect, Response: response, Reused: true}
			return nil
		}
		if effect.Status != EffectStatusPending {
			return fmt.Errorf("%w: status=%s", ErrEffectStateConflict, effect.Status)
		}
		if effect.EffectRole == EffectRoleDerived {
			if effect.ParentEffectID == nil {
				return ErrEffectIdentityMismatch
			}
			var parent mysql.AgentEffect
			if err := tx.First(&parent, "id = ?", *effect.ParentEffectID).Error; err != nil {
				return fmt.Errorf("读取 derived Effect 父节点: %w", err)
			}
			if parent.Status != EffectStatusSucceeded || parent.ProposalHash != effect.ProposalHash {
				return ErrEffectStateConflict
			}
		}
		var databaseNow time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&databaseNow).Error; err != nil {
			return fmt.Errorf("读取 external Effect 开始时间: %w", err)
		}
		deadline, err := BoundExternalEffectDeadline(databaseNow, input.AttemptDeadline, *run.LeaseUntil, input.LeaseSafetyMargin)
		if err != nil {
			return err
		}
		result := tx.Model(&mysql.AgentEffect{}).
			Where("id = ? AND status = ? AND version = ?", effect.ID, EffectStatusPending, effect.Version).
			Updates(map[string]any{
				"status": EffectStatusRunning, "version": gorm.Expr("version + 1"),
				"lease_generation": execution.Lease.Generation, "attempt": execution.Attempt,
				"tool_call_id_observed": nullableEffectText(execution.ToolCallIDObserved), "started_at": databaseNow,
			})
		if result.Error != nil {
			return fmt.Errorf("认领 external Effect: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		if err := appendExternalEffectEvent(tx, run, execution, effect, EventEffectStarted, EffectStatusRunning, RunStatusRunning); err != nil {
			return err
		}
		if err := tx.First(&effect, "id = ?", effect.ID).Error; err != nil {
			return err
		}
		started = StartExternalEffectResult{Effect: effect, CallDeadline: deadline}
		return nil
	})
	return started, err
}

// FinishExternalEffectSucceeded 持久化可安全复用的脱敏结果；CAS 失败绝不返回成功。
func (s *GORMStore) FinishExternalEffectSucceeded(ctx context.Context, input FinishExternalEffectInput) (TransitionEffectResult, error) {
	var committed TransitionEffectResult
	err := s.finishExternalEffect(ctx, input, EffectStatusSucceeded, EventEffectSucceeded, func(tx *gorm.DB, effect *mysql.AgentEffect) error {
		response := policy.NewRedactor().RedactText(input.ResponseRedacted)
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"response_redacted": string(encoded), "external_reference": nullableEffectText(policy.NewRedactor().RedactText(input.ExternalReference)),
			"last_error": nil, "finished_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		}
		if evidence, err := normalizeExternalEvidence(input.EvidenceRedacted); err != nil {
			return err
		} else if evidence != "" {
			updates["resolution_evidence_redacted"] = evidence
		}
		return tx.Model(&mysql.AgentEffect{}).Where("id = ?", effect.ID).Updates(updates).Error
	}, &committed)
	return committed, err
}

// FinishExternalEffectFailed 保存确定失败或可证明未发送的失败，不释放 Run lease。
func (s *GORMStore) FinishExternalEffectFailed(ctx context.Context, input FinishExternalEffectInput) error {
	return s.finishExternalEffect(ctx, input, EffectStatusFailed, EventEffectFailed, func(tx *gorm.DB, effect *mysql.AgentEffect) error {
		updates := map[string]any{
			"last_error":  policy.NewRedactor().RedactText(input.LastErrorRedacted),
			"finished_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		}
		if evidence, err := normalizeExternalEvidence(input.EvidenceRedacted); err != nil {
			return err
		} else if evidence != "" {
			updates["resolution_evidence_redacted"] = evidence
		}
		return tx.Model(&mysql.AgentEffect{}).Where("id = ?", effect.ID).Updates(updates).Error
	}, nil)
}

// MarkExternalEffectUnknownAndPark 将 ambiguous Effect、Run parked、Session 占用和两个 Event 同事务提交。
func (s *GORMStore) MarkExternalEffectUnknownAndPark(ctx context.Context, input FinishExternalEffectInput) error {
	execution := input.Execution
	if err := validateExternalExecutionInput(execution); err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, execution.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if err := validateExternalRunAndApproval(tx, run, execution); err != nil {
			return err
		}
		effect, err := lockRunningExternalEffect(tx, input)
		if err != nil {
			return err
		}
		evidence, err := normalizeExternalEvidence(input.EvidenceRedacted)
		if err != nil {
			return err
		}
		result := tx.Model(&mysql.AgentEffect{}).
			Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", effect.ID, EffectStatusRunning, input.ExpectedVersion, execution.Lease.Generation).
			Updates(map[string]any{
				"status": EffectStatusUnknown, "version": gorm.Expr("version + 1"),
				"last_error":                   policy.NewRedactor().RedactText(input.LastErrorRedacted),
				"resolution_evidence_redacted": nullableEffectText(evidence), "finished_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
			})
		if result.Error != nil {
			return fmt.Errorf("提交 unknown Effect: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		if err := appendExternalEffectEvent(tx, run, execution, effect, EventEffectUnknown, EffectStatusUnknown, RunStatusRunning); err != nil {
			return err
		}
		parkEvent := WorkflowEventInput{Type: EventRunParked, TraceID: execution.TraceID, Payload: EventPayload{
			Reference: effect.ID, Attributes: map[string]any{"park_reason": ParkReasonEffectUnknown, "effect_id": effect.ID},
		}}
		payload, err := marshalDurableEvent(parkEvent)
		if err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusRunning, execution.Lease, map[string]any{
			"status": RunStatusParked, "park_reason": ParkReasonEffectUnknown,
			"lease_owner": nil, "lease_until": nil, "heartbeat_at": nil,
		})
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, parkEvent, payload)
	})
}

// BoundExternalEffectDeadline 选择 attempt deadline 与 lease 安全边界中的较早者。
func BoundExternalEffectDeadline(now, attemptDeadline, leaseUntil time.Time, safetyMargin time.Duration) (time.Time, error) {
	if safetyMargin <= 0 || leaseUntil.IsZero() {
		return time.Time{}, ErrExternalDeadlineUnsafe
	}
	deadline := leaseUntil.Add(-safetyMargin)
	if !attemptDeadline.IsZero() && attemptDeadline.Before(deadline) {
		deadline = attemptDeadline
	}
	if !deadline.After(now) {
		return time.Time{}, ErrExternalDeadlineUnsafe
	}
	return deadline, nil
}

func (s *GORMStore) finishExternalEffect(ctx context.Context, input FinishExternalEffectInput, status, eventType string, update func(*gorm.DB, *mysql.AgentEffect) error, committed *TransitionEffectResult) error {
	execution := input.Execution
	if err := validateExternalExecutionInput(execution); err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, execution.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if err := validateExternalRunAndApproval(tx, run, execution); err != nil {
			return err
		}
		effect, err := lockRunningExternalEffect(tx, input)
		if err != nil {
			return err
		}
		result := tx.Model(&mysql.AgentEffect{}).
			Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", effect.ID, EffectStatusRunning, input.ExpectedVersion, execution.Lease.Generation).
			Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		if err := update(tx, &effect); err != nil {
			return err
		}
		if err := appendExternalEffectEvent(tx, run, execution, effect, eventType, status, RunStatusRunning); err != nil {
			return err
		}
		if committed != nil {
			if err := tx.First(&effect, "id = ?", effect.ID).Error; err != nil {
				return err
			}
			response, err := decodeEffectResponse(effect.ResponseRedacted)
			if err != nil {
				return err
			}
			*committed = TransitionEffectResult{Effect: effect, Response: response}
		}
		return nil
	})
}

func validateExternalExecutionInput(input ExternalEffectExecutionInput) error {
	transition := externalTransitionInput(input)
	if err := validateLeaseToken(input.Lease); err != nil {
		return err
	}
	if input.EffectType != string(policy.EffectTransactionalDB) && input.EffectType != string(policy.EffectReconcilable) &&
		input.EffectType != string(policy.EffectProviderIdempotent) && input.EffectType != string(policy.EffectNonReconciliableExternal) {
		return fmt.Errorf("external Effect type is invalid")
	}
	transition.EffectType = string(policy.EffectTransactionalDB)
	if err := validateTransitionEffectInput(transition, func(context.Context) (string, error) { return "", nil }); err != nil {
		return err
	}
	return nil
}

func externalStepType(input ExternalEffectExecutionInput, step string) string {
	if step == EffectStepPrimary {
		return input.EffectType
	}
	for _, derived := range input.Derived {
		if derived.Step == step {
			return derived.EffectType
		}
	}
	return ""
}

func validateExternalAuthorization(ctx context.Context, store *GORMStore, input ExternalEffectExecutionInput) error {
	if !input.GateAllowed {
		return policy.ErrForbidden
	}
	if err := store.authorizeRunScope(ctx, input.Lease.RunID); err != nil {
		return err
	}
	return policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{})
}

func validateExternalRunAndApproval(tx *gorm.DB, run *mysql.WorkflowRun, input ExternalEffectExecutionInput) error {
	if run.RuntimeCompatibilityHash == nil || run.PolicyHash == nil ||
		*run.RuntimeCompatibilityHash != input.RuntimeCompatibilityHash || *run.PolicyHash != input.PolicyHash {
		return policy.ErrForbidden
	}
	approval, err := lockApprovedEffectApproval(tx, run, externalTransitionInput(input))
	if err != nil {
		return err
	}
	return validateApprovalCheckpointBinding(tx, run, approval)
}

func externalTransitionInput(input ExternalEffectExecutionInput) TransitionEffectInput {
	return TransitionEffectInput{
		Lease: input.Lease, ApprovalID: input.ApprovalID, ProposalHash: input.ProposalHash,
		ToolCallIDObserved: input.ToolCallIDObserved, ToolName: input.ToolName, ToolRevision: input.ToolRevision,
		ToolSchemaHash: input.ToolSchemaHash, TargetHash: input.TargetHash, RequestRedacted: input.RequestRedacted,
		PolicyHash: input.PolicyHash, RuntimeCompatibilityHash: input.RuntimeCompatibilityHash,
		EffectType: input.EffectType, Attempt: input.Attempt, TraceID: input.TraceID,
		GateAllowed: input.GateAllowed, Derived: input.Derived,
	}
}

func lockRunningExternalEffect(tx *gorm.DB, input FinishExternalEffectInput) (mysql.AgentEffect, error) {
	effect, found, err := lockEffect(tx, input.EffectID)
	if err != nil {
		return mysql.AgentEffect{}, err
	}
	if !found || effect.Status != EffectStatusRunning || effect.Version != input.ExpectedVersion ||
		effect.LeaseGeneration != input.Execution.Lease.Generation || effect.RunID != input.Execution.Lease.RunID ||
		effect.ProposalHash != input.Execution.ProposalHash {
		return mysql.AgentEffect{}, ErrEffectStateConflict
	}
	return effect, nil
}

func appendExternalEffectEvent(tx *gorm.DB, run *mysql.WorkflowRun, input ExternalEffectExecutionInput, effect mysql.AgentEffect, eventType, status, expectedRunStatus string) error {
	event := WorkflowEventInput{Type: eventType, TraceID: input.TraceID, Payload: EventPayload{
		Reference: effect.ID, Attributes: map[string]any{
			"effect_id": effect.ID, "effect_role": effect.EffectRole, "effect_step": effect.EffectStep,
			"effect_type": effect.EffectType, "proposal_hash": input.ProposalHash, "status": status,
		},
	}}
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, expectedRunStatus, input.Lease, map[string]any{})
	if err != nil {
		return err
	}
	return insertDurableEvent(tx, run.ID, seq, event, payload)
}

func normalizeExternalEvidence(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	canonical, err := policy.CanonicalToolArgumentsJSON([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("external Effect evidence must be a JSON object: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return "", err
	}
	redacted, err := policy.NewRedactor().RedactJSON(decoded)
	if err != nil {
		return "", err
	}
	return string(redacted), nil
}
