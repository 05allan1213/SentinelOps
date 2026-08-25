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
	EffectRolePrimary = "primary"
	EffectRoleDerived = "derived"
	EffectStepPrimary = "primary"

	EffectStatusPending     = "pending"
	EffectStatusRunning     = "running"
	EffectStatusSucceeded   = "succeeded"
	EffectStatusFailed      = "failed"
	EffectStatusUnknown     = "unknown"
	EffectStatusReconciling = "reconciling"
)

var (
	// ErrEffectIdentityMismatch 表示唯一键命中的既有 Effect 与当前冻结事实不一致。
	ErrEffectIdentityMismatch = errors.New("effect identity mismatch")
	// ErrEffectStateConflict 表示 transactional_db 发现不可能安全普通重放的持久化状态。
	ErrEffectStateConflict = errors.New("transactional effect state conflict")
)

// DerivedEffectInput 描述 Primary 提交时必须确定性创建的 pending 后继步骤。
// 后继执行与 reconciliation 属于 P24/P25，本单元只提交稳定 ledger 行。
type DerivedEffectInput struct {
	Step       string
	EffectType string
}

// TransitionEffectInput 是 transactional_db Primary 原子执行所需的冻结事实。
type TransitionEffectInput struct {
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

// TransitionEffectResult 返回已提交的 Primary 与可安全复用的脱敏 Tool 结果。
type TransitionEffectResult struct {
	Effect   mysql.AgentEffect
	Response string
	Reused   bool
}

// TransactionalEffectCallback 是 RuntimeHandler 传入的原 Tool endpoint 调用。
// callback Context 已绑定当前 GORM Transaction，不得启动 goroutine 或执行外部副作用。
type TransactionalEffectCallback func(context.Context) (string, error)

// TransitionEffectWithEvent 原子完成 Primary pending→running→succeeded、领域写、
// Effect Events 与确定性 derived pending 行；任一步失败均整体回滚且永不写 unknown。
func (s *GORMStore) TransitionEffectWithEvent(
	ctx context.Context,
	input TransitionEffectInput,
	callback TransactionalEffectCallback,
) (TransitionEffectResult, error) {
	normalizedRequest, err := normalizeRedactedRequest(input.RequestRedacted)
	if err != nil {
		return TransitionEffectResult{}, err
	}
	input.RequestRedacted = normalizedRequest
	if err := validateTransitionEffectInput(input, callback); err != nil {
		return TransitionEffectResult{}, err
	}
	if !input.GateAllowed {
		return TransitionEffectResult{}, policy.ErrForbidden
	}
	if err := s.authorizeRunScope(ctx, input.Lease.RunID); err != nil {
		return TransitionEffectResult{}, err
	}
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return TransitionEffectResult{}, err
	}

	var committed TransitionEffectResult
	err = s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if run.RuntimeCompatibilityHash == nil || run.PolicyHash == nil ||
			*run.RuntimeCompatibilityHash != input.RuntimeCompatibilityHash || *run.PolicyHash != input.PolicyHash {
			return policy.ErrForbidden
		}
		approval, err := lockApprovedEffectApproval(tx, run, input)
		if err != nil {
			return err
		}
		if err := validateApprovalCheckpointBinding(tx, run, approval); err != nil {
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
			if err := validatePrimaryEffectIdentity(&primary, input, primaryKey); err != nil {
				return err
			}
			if primary.Status == EffectStatusSucceeded {
				if err := validateDerivedEffects(tx, &primary, input); err != nil {
					return err
				}
				response, err := decodeEffectResponse(primary.ResponseRedacted)
				if err != nil {
					return err
				}
				committed = TransitionEffectResult{Effect: primary, Response: response, Reused: true}
				return nil
			}
			if primary.Status != EffectStatusPending {
				return fmt.Errorf("%w: status=%s", ErrEffectStateConflict, primary.Status)
			}
		} else {
			primary = newPrimaryEffect(run, input, primaryKey)
			if err := tx.Create(&primary).Error; err != nil {
				return fmt.Errorf("创建 Primary Effect: %w", err)
			}
		}

		var databaseNow time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&databaseNow).Error; err != nil {
			return fmt.Errorf("读取 Effect 开始时间: %w", err)
		}
		started := tx.Model(&mysql.AgentEffect{}).
			Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", primary.ID, EffectStatusPending, primary.Version, primary.LeaseGeneration).
			Updates(map[string]any{
				"status": EffectStatusRunning, "version": gorm.Expr("version + 1"),
				"lease_generation": input.Lease.Generation, "attempt": input.Attempt,
				"tool_call_id_observed": nullableEffectText(input.ToolCallIDObserved), "started_at": databaseNow,
			})
		if started.Error != nil {
			return fmt.Errorf("认领 transactional Primary Effect: %w", started.Error)
		}
		if started.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		if err := appendEffectEvent(tx, run, input, primary.ID, EventEffectStarted, EffectStatusRunning); err != nil {
			return err
		}

		callbackCtx, err := mysql.ContextWithTransaction(ctx, tx)
		if err != nil {
			return err
		}
		response, err := callback(callbackCtx)
		if err != nil {
			return err
		}
		redactedResponse := policy.NewRedactor().RedactText(response)
		encodedResponse, err := json.Marshal(redactedResponse)
		if err != nil {
			return fmt.Errorf("编码脱敏 Effect 结果: %w", err)
		}
		if err := createDerivedEffects(tx, run, &primary, input); err != nil {
			return err
		}

		finished := tx.Model(&mysql.AgentEffect{}).
			Where("id = ? AND status = ? AND version = ? AND lease_generation = ?", primary.ID, EffectStatusRunning, primary.Version+1, input.Lease.Generation).
			Updates(map[string]any{
				"status": EffectStatusSucceeded, "version": gorm.Expr("version + 1"),
				"response_redacted": string(encodedResponse), "finished_at": gorm.Expr("CURRENT_TIMESTAMP(3)"), "last_error": nil,
			})
		if finished.Error != nil {
			return fmt.Errorf("提交 transactional Primary Effect: %w", finished.Error)
		}
		if finished.RowsAffected != 1 {
			return ErrEffectStateConflict
		}
		if err := appendEffectEvent(tx, run, input, primary.ID, EventEffectSucceeded, EffectStatusSucceeded); err != nil {
			return err
		}
		if err := tx.First(&primary, "id = ?", primary.ID).Error; err != nil {
			return fmt.Errorf("读取已提交 Primary Effect: %w", err)
		}
		committed = TransitionEffectResult{Effect: primary, Response: redactedResponse}
		return nil
	})
	if err != nil {
		return TransitionEffectResult{}, err
	}
	return committed, nil
}

func validateTransitionEffectInput(input TransitionEffectInput, callback TransactionalEffectCallback) error {
	if err := validateLeaseToken(input.Lease); err != nil {
		return err
	}
	if callback == nil {
		return fmt.Errorf("transactional Effect callback is required")
	}
	if input.EffectType != string(policy.EffectTransactionalDB) {
		return fmt.Errorf("P23 requires transactional_db Primary Effect")
	}
	if input.Attempt == 0 || strings.TrimSpace(input.ApprovalID) == "" || strings.TrimSpace(input.ToolName) == "" ||
		strings.TrimSpace(input.ToolRevision) == "" || strings.TrimSpace(input.TraceID) == "" {
		return fmt.Errorf("transactional Effect execution identity is incomplete")
	}
	if err := validateRedactedRequest(input.RequestRedacted); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"proposal_hash": input.ProposalHash, "tool_schema_hash": input.ToolSchemaHash,
		"target_hash": input.TargetHash, "policy_hash": input.PolicyHash,
		"runtime_compatibility_hash": input.RuntimeCompatibilityHash,
	} {
		if !validLowerSHA256(value) {
			return fmt.Errorf("%s must be a lowercase SHA-256", name)
		}
	}
	seen := map[string]struct{}{EffectStepPrimary: {}}
	for _, derived := range input.Derived {
		if strings.TrimSpace(derived.Step) == "" || derived.EffectType == "" || derived.EffectType == string(policy.EffectTransactionalDB) {
			return fmt.Errorf("derived Effect step/type is invalid")
		}
		if _, duplicate := seen[derived.Step]; duplicate {
			return fmt.Errorf("duplicate Effect step %q", derived.Step)
		}
		seen[derived.Step] = struct{}{}
	}
	return nil
}

func validateRedactedRequest(raw string) error {
	_, err := normalizeRedactedRequest(raw)
	return err
}

func normalizeRedactedRequest(raw string) (string, error) {
	canonical, err := policy.CanonicalToolArgumentsJSON([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("transactional Effect request_redacted must be a JSON object: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return "", fmt.Errorf("decode transactional Effect request_redacted: %w", err)
	}
	redacted, err := policy.NewRedactor().RedactJSON(decoded)
	if err != nil {
		return "", fmt.Errorf("validate transactional Effect request_redacted redaction: %w", err)
	}
	if string(redacted) != string(canonical) {
		return "", fmt.Errorf("transactional Effect request_redacted contains unredacted sensitive material")
	}
	return string(canonical), nil
}

func lockApprovedEffectApproval(tx *gorm.DB, run *mysql.WorkflowRun, input TransitionEffectInput) (*mysql.AgentApproval, error) {
	var approval mysql.AgentApproval
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&approval, "id = ?", input.ApprovalID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrApprovalNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("锁定 Effect Approval: %w", result.Error)
	}
	if err := validateStoredApprovalIdentity(&approval); err != nil {
		return nil, err
	}
	if approval.Status != ApprovalStatusApproved || approval.RunID != run.ID || approval.ProposalHash != input.ProposalHash ||
		approval.ToolName != input.ToolName || approval.ToolRevision != input.ToolRevision || approval.ToolSchemaHash != input.ToolSchemaHash ||
		approval.PolicyHash != input.PolicyHash || approval.RuntimeCompatibilityHash != input.RuntimeCompatibilityHash {
		return nil, policy.ErrForbidden
	}
	return &approval, nil
}

func lockEffect(tx *gorm.DB, id string) (mysql.AgentEffect, bool, error) {
	var effect mysql.AgentEffect
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&effect, "id = ?", id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return mysql.AgentEffect{}, false, nil
	}
	if result.Error != nil {
		return mysql.AgentEffect{}, false, fmt.Errorf("锁定 Effect: %w", result.Error)
	}
	return effect, true, nil
}

func newPrimaryEffect(run *mysql.WorkflowRun, input TransitionEffectInput, key string) mysql.AgentEffect {
	now := time.Now()
	request := input.RequestRedacted
	return mysql.AgentEffect{
		ID: key, RunID: run.ID, IdempotencyKey: key, EffectRole: EffectRolePrimary, EffectStep: EffectStepPrimary,
		ProposalHash: input.ProposalHash, ToolName: input.ToolName, ToolRevision: input.ToolRevision,
		ToolSchemaHash: input.ToolSchemaHash, TargetHash: input.TargetHash, RequestRedacted: &request,
		EffectType: input.EffectType, Status: EffectStatusPending, Version: 1,
		LeaseGeneration: input.Lease.Generation, Attempt: input.Attempt, CreatedAt: now, UpdatedAt: now,
	}
}

func validatePrimaryEffectIdentity(effect *mysql.AgentEffect, input TransitionEffectInput, key string) error {
	if effect.ID != key || effect.IdempotencyKey != key || effect.RunID != input.Lease.RunID ||
		effect.EffectRole != EffectRolePrimary || effect.EffectStep != EffectStepPrimary || effect.ParentEffectID != nil ||
		effect.ProposalHash != input.ProposalHash || effect.ToolName != input.ToolName || effect.ToolRevision != input.ToolRevision ||
		effect.ToolSchemaHash != input.ToolSchemaHash || effect.TargetHash != input.TargetHash || effect.EffectType != input.EffectType {
		return ErrEffectIdentityMismatch
	}
	return nil
}

func createDerivedEffects(tx *gorm.DB, run *mysql.WorkflowRun, primary *mysql.AgentEffect, input TransitionEffectInput) error {
	for _, derived := range input.Derived {
		key, err := policy.EffectKey(run.ID, input.ProposalHash, derived.Step)
		if err != nil {
			return err
		}
		request := input.RequestRedacted
		parentID := primary.ID
		now := time.Now()
		row := mysql.AgentEffect{
			ID: key, RunID: run.ID, IdempotencyKey: key, EffectRole: EffectRoleDerived, EffectStep: derived.Step,
			ParentEffectID: &parentID, ProposalHash: input.ProposalHash, ToolName: input.ToolName,
			ToolRevision: input.ToolRevision, ToolSchemaHash: input.ToolSchemaHash, TargetHash: input.TargetHash,
			RequestRedacted: &request, EffectType: derived.EffectType, Status: EffectStatusPending, Version: 1,
			LeaseGeneration: input.Lease.Generation, Attempt: input.Attempt, CreatedAt: now, UpdatedAt: now,
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return fmt.Errorf("创建 derived Effect %q: %w", derived.Step, result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrEffectIdentityMismatch
		}
	}
	return nil
}

func validateDerivedEffects(tx *gorm.DB, primary *mysql.AgentEffect, input TransitionEffectInput) error {
	var stored []mysql.AgentEffect
	if err := tx.Where("parent_effect_id = ?", primary.ID).Order("effect_step").Find(&stored).Error; err != nil {
		return fmt.Errorf("读取 derived Effect: %w", err)
	}
	if len(stored) != len(input.Derived) {
		return ErrEffectIdentityMismatch
	}
	want := make(map[string]string, len(input.Derived))
	for _, derived := range input.Derived {
		want[derived.Step] = derived.EffectType
	}
	for index := range stored {
		effect := &stored[index]
		key, err := policy.EffectKey(input.Lease.RunID, input.ProposalHash, effect.EffectStep)
		if err != nil {
			return err
		}
		if want[effect.EffectStep] != effect.EffectType || effect.ID != key || effect.IdempotencyKey != key ||
			effect.EffectRole != EffectRoleDerived || effect.ParentEffectID == nil || *effect.ParentEffectID != primary.ID {
			return ErrEffectIdentityMismatch
		}
	}
	return nil
}

func appendEffectEvent(tx *gorm.DB, run *mysql.WorkflowRun, input TransitionEffectInput, effectID, eventType, status string) error {
	event := WorkflowEventInput{Type: eventType, TraceID: input.TraceID, Payload: EventPayload{
		Reference: effectID, Attributes: map[string]any{
			"effect_id": effectID, "effect_role": EffectRolePrimary, "effect_step": EffectStepPrimary,
			"effect_type": input.EffectType, "proposal_hash": input.ProposalHash, "status": status,
		},
	}}
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusRunning, input.Lease, map[string]any{})
	if err != nil {
		return err
	}
	return insertDurableEvent(tx, run.ID, seq, event, payload)
}

func decodeEffectResponse(encoded *string) (string, error) {
	if encoded == nil {
		return "", ErrEffectIdentityMismatch
	}
	var response string
	if err := json.Unmarshal([]byte(*encoded), &response); err != nil {
		return "", fmt.Errorf("解码持久化 Effect 结果: %w", err)
	}
	return response, nil
}

func nullableEffectText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
