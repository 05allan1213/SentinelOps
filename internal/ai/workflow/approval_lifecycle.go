package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrApprovalCheckpointMismatch 表示发布或恢复所用 Checkpoint fingerprint 不再精确一致。
	ErrApprovalCheckpointMismatch = errors.New("APPROVAL_CHECKPOINT_MISMATCH")
	// ErrApprovalResumeTargetRequired 表示 Mutation Tool 不是官方 ResumeWithParams 的显式 target。
	ErrApprovalResumeTargetRequired = errors.New("APPROVAL_RESUME_TARGET_REQUIRED")
	// ErrApprovalRejected 表示持久化 Approval 已拒绝，禁止相同 Proposal 再次中断。
	ErrApprovalRejected = errors.New("APPROVAL_REJECTED")
	// ErrApprovalInvalidated 表示恢复依赖已失效，禁止相同 Proposal 再次中断。
	ErrApprovalInvalidated = errors.New("APPROVAL_INVALIDATED")
)

// CheckpointFingerprint 是项目允许观察的 Eino opaque Checkpoint 元数据。
type CheckpointFingerprint struct {
	ID              string
	PayloadSHA256   string
	LeaseGeneration uint64
}

// PrepareApprovalInput 是 StatefulInterrupt 前 generation-fenced preparing upsert 的稳定事实。
type PrepareApprovalInput struct {
	Lease                    LeaseToken
	ToolCallIDObserved       string
	ToolName                 string
	ToolRevision             string
	ToolSchemaHash           string
	RiskLevel                policy.RiskLevel
	ProposalJSONRedacted     string
	ProposalHash             string
	PolicyHash               string
	RuntimeCompatibilityHash string
	ExpiresAt                time.Time
}

// PublishApprovalInput 是 Runner Interrupt Event 与已提交 Checkpoint 的精确绑定。
type PublishApprovalInput struct {
	Lease                     LeaseToken
	ApprovalID                string
	ProposalHash              string
	InterruptID               string
	InterruptAddress          string
	CheckpointID              string
	CheckpointPayloadSHA256   string
	CheckpointLeaseGeneration uint64
	TraceID                   string
}

// ApprovalResumeTarget 是数据库决定映射到官方 ResumeWithParams 的最小值。
type ApprovalResumeTarget struct {
	ApprovalID   string
	ProposalHash string
	InterruptID  string
	Decision     string
}

// AuthorizeApprovalResumeInput 是 Tool endpoint 前重新读取 MySQL 真值的精确输入。
type AuthorizeApprovalResumeInput struct {
	Lease                    LeaseToken
	ApprovalID               string
	ProposalHash             string
	ToolName                 string
	ToolRevision             string
	ToolSchemaHash           string
	PolicyHash               string
	RuntimeCompatibilityHash string
	ExplicitTarget           bool
	GateAllowed              bool
}

// PrepareApproval 只插入或刷新同一 generation 可见的 preparing；任何后续状态只读返回。
func (s *GORMStore) PrepareApproval(ctx context.Context, input PrepareApprovalInput) (*mysql.AgentApproval, error) {
	if err := validatePrepareApprovalInput(input); err != nil {
		return nil, err
	}
	if err := policy.Authorize(ctx, policy.PermissionProposeMutation, policy.Resource{}); err != nil {
		return nil, err
	}
	requestedBy, err := policy.UserID(ctx)
	if err != nil {
		return nil, err
	}
	approvalID, err := policy.ApprovalID(input.Lease.RunID, input.ProposalHash)
	if err != nil {
		return nil, err
	}
	var result *mysql.AgentApproval
	var resultErr error
	err = s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if run.UserID != requestedBy || run.RuntimeCompatibilityHash == nil || *run.RuntimeCompatibilityHash != input.RuntimeCompatibilityHash {
			return policy.ErrForbidden
		}
		var existing mysql.AgentApproval
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, "id = ?", approvalID)
		if lookup.Error == nil {
			if err := validateSamePreparedApproval(&existing, input, requestedBy); err != nil {
				if parkErr := invalidateApprovalAndParkTx(tx, run, input.Lease, &existing, "preparing_identity_mismatch"); parkErr != nil {
					return parkErr
				}
				resultErr = ErrApprovalInvalidated
				return nil
			}
			if existing.Status == ApprovalStatusPreparing {
				updates := map[string]any{
					"tool_call_id_observed": nullableApprovalText(input.ToolCallIDObserved),
					"preparing_at":          gorm.Expr("CURRENT_TIMESTAMP(3)"), "expires_at": input.ExpiresAt,
				}
				if err := tx.Model(&mysql.AgentApproval{}).Where("id = ? AND status = ?", existing.ID, ApprovalStatusPreparing).Updates(updates).Error; err != nil {
					return fmt.Errorf("刷新 preparing Approval: %w", err)
				}
				if err := tx.First(&existing, "id = ?", existing.ID).Error; err != nil {
					return fmt.Errorf("读取已刷新 preparing Approval: %w", err)
				}
			}
			copy := existing
			result = &copy
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return fmt.Errorf("锁定 preparing Approval: %w", lookup.Error)
		}

		var databaseNow time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&databaseNow).Error; err != nil {
			return fmt.Errorf("读取 preparing Approval 时间: %w", err)
		}
		row := mysql.AgentApproval{
			ID: approvalID, RunID: run.ID, ToolName: input.ToolName, ToolRevision: input.ToolRevision,
			ToolSchemaHash: input.ToolSchemaHash, RiskLevel: string(input.RiskLevel),
			ProposalJSONRedacted: input.ProposalJSONRedacted, ProposalHash: input.ProposalHash,
			PolicyHash: input.PolicyHash, RuntimeCompatibilityHash: input.RuntimeCompatibilityHash,
			RequestedBy: requestedBy, Status: ApprovalStatusPreparing, Version: 1,
			PreparingAt: databaseNow, CreatedAt: databaseNow, ExpiresAt: &input.ExpiresAt,
		}
		if strings.TrimSpace(input.ToolCallIDObserved) != "" {
			row.ToolCallIDObserved = &input.ToolCallIDObserved
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("创建 preparing Approval: %w", err)
		}
		event := WorkflowEventInput{Type: EventApprovalPreparing, Payload: EventPayload{
			Reference: row.ID, Attributes: map[string]any{"approval_id": row.ID, "proposal_hash": row.ProposalHash, "risk_level": row.RiskLevel},
		}}
		payload, err := marshalDurableEvent(event)
		if err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusRunning, input.Lease, map[string]any{})
		if err != nil {
			return err
		}
		if err := insertDurableEvent(tx, run.ID, seq, event, payload); err != nil {
			return err
		}
		copy := row
		result = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, resultErr
}

// LoadCheckpointFingerprint 只读取当前 generation 刚提交的 Checkpoint 元数据，不解析 blob。
func (s *GORMStore) LoadCheckpointFingerprint(ctx context.Context, lease LeaseToken, checkpointID string) (CheckpointFingerprint, error) {
	var fingerprint CheckpointFingerprint
	err := s.withFencedRunTransaction(ctx, lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		row, err := lockCheckpoint(tx, run, checkpointID)
		if err != nil {
			return err
		}
		if row.LeaseGeneration == nil || *row.LeaseGeneration != lease.Generation {
			return ErrApprovalCheckpointMismatch
		}
		fingerprint = CheckpointFingerprint{ID: checkpointID, PayloadSHA256: *row.PayloadSHA256, LeaseGeneration: *row.LeaseGeneration}
		return nil
	})
	return fingerprint, err
}

// PublishApprovalAndWait 原子提交 preparing→pending、Run→waiting_approval 与 Interrupt/Approval Events。
func (s *GORMStore) PublishApprovalAndWait(ctx context.Context, input PublishApprovalInput) (*mysql.AgentApproval, error) {
	if err := validatePublishApprovalInput(input); err != nil {
		return nil, err
	}
	var published *mysql.AgentApproval
	err := s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		var approval mysql.AgentApproval
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&approval, "id = ?", input.ApprovalID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrApprovalNotFound
			}
			return fmt.Errorf("锁定待发布 Approval: %w", err)
		}
		if approval.Status != ApprovalStatusPreparing || approval.ProposalHash != input.ProposalHash || approval.RunID != run.ID {
			return ErrApprovalCheckpointMismatch
		}
		row, err := lockCheckpoint(tx, run, input.CheckpointID)
		if err != nil {
			return err
		}
		if *row.PayloadSHA256 != input.CheckpointPayloadSHA256 || row.LeaseGeneration == nil ||
			*row.LeaseGeneration != input.CheckpointLeaseGeneration || input.CheckpointLeaseGeneration != input.Lease.Generation {
			return ErrApprovalCheckpointMismatch
		}
		var databaseNow time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&databaseNow).Error; err != nil {
			return fmt.Errorf("读取 Approval 发布时间: %w", err)
		}
		updates := map[string]any{
			"status": ApprovalStatusPending, "interrupt_id": input.InterruptID,
			"interrupt_address": input.InterruptAddress, "checkpoint_id": input.CheckpointID,
			"checkpoint_payload_sha256":   input.CheckpointPayloadSHA256,
			"checkpoint_lease_generation": input.CheckpointLeaseGeneration, "published_at": databaseNow,
		}
		approvalUpdate := tx.Model(&mysql.AgentApproval{}).
			Where("id = ? AND status = ? AND proposal_hash = ?", approval.ID, ApprovalStatusPreparing, input.ProposalHash).
			Updates(updates)
		if approvalUpdate.Error != nil {
			return fmt.Errorf("发布 pending Approval: %w", approvalUpdate.Error)
		}
		if approvalUpdate.RowsAffected != 1 {
			return ErrApprovalCheckpointMismatch
		}

		interrupted := WorkflowEventInput{Type: EventAgentInterrupted, TraceID: input.TraceID, Payload: EventPayload{
			Reference: approval.ID, Attributes: map[string]any{"approval_id": approval.ID, "interrupt_id": input.InterruptID},
		}}
		requested := WorkflowEventInput{Type: EventApprovalRequested, TraceID: input.TraceID, Payload: EventPayload{
			Reference: approval.ID, Attributes: map[string]any{
				"approval_id": approval.ID, "proposal_hash": approval.ProposalHash,
				"checkpoint_id": input.CheckpointID, "checkpoint_payload_sha256": input.CheckpointPayloadSHA256,
				"checkpoint_lease_generation": input.CheckpointLeaseGeneration,
			},
		}}
		interruptedPayload, err := marshalDurableEvent(interrupted)
		if err != nil {
			return err
		}
		requestedPayload, err := marshalDurableEvent(requested)
		if err != nil {
			return err
		}
		firstSeq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusRunning, input.Lease, map[string]any{})
		if err != nil {
			return err
		}
		if err := insertDurableEvent(tx, run.ID, firstSeq, interrupted, interruptedPayload); err != nil {
			return err
		}
		secondSeq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusRunning, input.Lease, map[string]any{
			"status": RunStatusWaitingApproval, "lease_owner": nil, "lease_until": nil, "heartbeat_at": nil,
		})
		if err != nil {
			return err
		}
		if err := insertDurableEvent(tx, run.ID, secondSeq, requested, requestedPayload); err != nil {
			return err
		}
		if err := tx.First(&approval, "id = ?", approval.ID).Error; err != nil {
			return fmt.Errorf("读取已发布 Approval: %w", err)
		}
		copy := approval
		published = &copy
		return nil
	})
	return published, err
}

// LoadApprovalResumeTarget 在 Runner Resume 前验证持久化 fingerprint，并构造显式 target。
// preparing orphan 返回空 InterruptID，调用方必须使用空 Targets 让叶子 Tool 重新 Interrupt。
func (s *GORMStore) LoadApprovalResumeTarget(ctx context.Context, lease LeaseToken) (*ApprovalResumeTarget, error) {
	var target *ApprovalResumeTarget
	var resultErr error
	err := s.withFencedRunTransaction(ctx, lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		var approval mysql.AgentApproval
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("run_id = ?", run.ID).Order("created_at DESC, id DESC").First(&approval)
		if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if lookup.Error != nil {
			return fmt.Errorf("读取 Approval Resume target: %w", lookup.Error)
		}
		if approval.Status == ApprovalStatusPreparing {
			checkpointID, err := EinoCheckpointID(run.ID)
			if err != nil {
				return err
			}
			var checkpointCount int64
			if err := tx.Model(&mysql.WorkflowCheckpoint{}).
				Where("run_id = ? AND eino_checkpoint_id = ?", run.ID, checkpointID).
				Count(&checkpointCount).Error; err != nil {
				return fmt.Errorf("检查 preparing orphan Checkpoint: %w", err)
			}
			if checkpointCount == 0 {
				return nil
			}
			if _, err := lockCheckpoint(tx, run, checkpointID); err != nil {
				if parkErr := invalidateApprovalAndParkTx(tx, run, lease, &approval, "preparing_checkpoint_invalid"); parkErr != nil {
					return parkErr
				}
				resultErr = ErrApprovalCheckpointMismatch
				return nil
			}
			target = &ApprovalResumeTarget{ApprovalID: approval.ID, ProposalHash: approval.ProposalHash, Decision: approval.Status}
			return nil
		}
		if approval.Status == ApprovalStatusPending || approval.InterruptID == nil || strings.TrimSpace(*approval.InterruptID) == "" {
			if parkErr := invalidateApprovalAndParkTx(tx, run, lease, &approval, "approval_resume_binding_missing"); parkErr != nil {
				return parkErr
			}
			resultErr = ErrApprovalCheckpointMismatch
			return nil
		}
		if err := validateApprovalCheckpointBinding(tx, run, &approval); err != nil {
			if parkErr := invalidateApprovalAndParkTx(tx, run, lease, &approval, "checkpoint_fingerprint_mismatch"); parkErr != nil {
				return parkErr
			}
			resultErr = ErrApprovalCheckpointMismatch
			return nil
		}
		target = &ApprovalResumeTarget{
			ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
			InterruptID: *approval.InterruptID, Decision: approval.Status,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return target, resultErr
}

// AuthorizeApprovalResume 忽略客户端决定数据并重新读取 exact MySQL Approval/Checkpoint 真值。
func (s *GORMStore) AuthorizeApprovalResume(ctx context.Context, input AuthorizeApprovalResumeInput) (*mysql.AgentApproval, error) {
	if !input.ExplicitTarget {
		return nil, ErrApprovalResumeTargetRequired
	}
	var authorized *mysql.AgentApproval
	var resultErr error
	err := s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		var approval mysql.AgentApproval
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&approval, "id = ? AND run_id = ?", input.ApprovalID, run.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrApprovalNotFound
			}
			return fmt.Errorf("锁定 Resume Approval: %w", err)
		}
		if approval.ProposalHash != input.ProposalHash || approval.ToolName != input.ToolName || approval.ToolRevision != input.ToolRevision ||
			approval.ToolSchemaHash != input.ToolSchemaHash || approval.PolicyHash != input.PolicyHash ||
			approval.RuntimeCompatibilityHash != input.RuntimeCompatibilityHash || run.RuntimeCompatibilityHash == nil ||
			*run.RuntimeCompatibilityHash != input.RuntimeCompatibilityHash {
			if err := invalidateApprovalAndParkTx(tx, run, input.Lease, &approval, "proposal_policy_or_runtime_mismatch"); err != nil {
				return err
			}
			resultErr = ErrApprovalInvalidated
			return nil
		}
		if err := validateApprovalCheckpointBinding(tx, run, &approval); err != nil {
			if parkErr := invalidateApprovalAndParkTx(tx, run, input.Lease, &approval, "checkpoint_fingerprint_mismatch"); parkErr != nil {
				return parkErr
			}
			resultErr = ErrApprovalCheckpointMismatch
			return nil
		}
		if !input.GateAllowed {
			if err := invalidateApprovalAndParkTx(tx, run, input.Lease, &approval, "write_gate_closed"); err != nil {
				return err
			}
			resultErr = ErrApprovalInvalidated
			return nil
		}
		switch approval.Status {
		case ApprovalStatusApproved:
			copy := approval
			authorized = &copy
			return nil
		case ApprovalStatusRejected:
			return ErrApprovalRejected
		case ApprovalStatusExpired:
			return ErrApprovalExpired
		case ApprovalStatusInvalidated:
			return ErrApprovalInvalidated
		default:
			return ErrApprovalResumeTargetRequired
		}
	})
	if err != nil {
		return nil, err
	}
	return authorized, resultErr
}

// ExpireDueApprovals 在既有 Worker loop 中有界扫描，并复用 phase21 决策 primitive 唤醒 Run。
func (s *GORMStore) ExpireDueApprovals(ctx context.Context, workerID string, limit int) (int, error) {
	if strings.TrimSpace(workerID) == "" || limit <= 0 || limit > 1000 {
		return 0, fmt.Errorf("approval expiry worker and limit are invalid")
	}
	var due []mysql.AgentApproval
	if err := s.db.WithContext(ctx).Where("status = ? AND expires_at IS NOT NULL AND expires_at <= CURRENT_TIMESTAMP(3)", ApprovalStatusPending).
		Order("expires_at ASC, id ASC").Limit(limit).Find(&due).Error; err != nil {
		return 0, fmt.Errorf("扫描到期 Approval: %w", err)
	}
	const expiryActorID = "approval-expiry-worker"
	workerContext := policy.WithIdentity(ctx, policy.Identity{
		UserID: expiryActorID, Role: policy.RoleAdmin,
		Scope: policy.Scope{UserID: expiryActorID, All: true},
	})
	expired := 0
	for index := range due {
		approval := &due[index]
		_, err := s.DecideApprovalAndWakeRun(workerContext, DecideApprovalInput{
			ApprovalID: approval.ID, ProposalHash: approval.ProposalHash, ExpectedVersion: approval.Version,
			Decision: ApprovalStatusExpired, Reason: "expired by durable Worker",
		})
		if errors.Is(err, ErrApprovalVersionConflict) || errors.Is(err, ErrApprovalAlreadyDecided) || errors.Is(err, ErrApprovalExpired) {
			continue
		}
		if err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

func validatePrepareApprovalInput(input PrepareApprovalInput) error {
	if err := validateLeaseToken(input.Lease); err != nil {
		return err
	}
	if strings.TrimSpace(input.ToolName) == "" || strings.TrimSpace(input.ToolRevision) == "" || input.ExpiresAt.IsZero() {
		return fmt.Errorf("preparing Approval Tool identity and expiry are required")
	}
	if input.RiskLevel != policy.RiskL1 && input.RiskLevel != policy.RiskL2 {
		return fmt.Errorf("preparing Approval requires L1 or L2 risk")
	}
	if !jsonObject(input.ProposalJSONRedacted) {
		return fmt.Errorf("preparing Approval requires redacted Proposal JSON object")
	}
	for name, value := range map[string]string{
		"tool_schema_hash": input.ToolSchemaHash, "proposal_hash": input.ProposalHash,
		"policy_hash": input.PolicyHash, "runtime_compatibility_hash": input.RuntimeCompatibilityHash,
	} {
		if !validLowerSHA256(value) {
			return fmt.Errorf("%s must be a lowercase SHA-256", name)
		}
	}
	return nil
}

func validatePublishApprovalInput(input PublishApprovalInput) error {
	if err := validateLeaseToken(input.Lease); err != nil {
		return err
	}
	if strings.TrimSpace(input.ApprovalID) == "" || strings.TrimSpace(input.InterruptID) == "" || len(input.InterruptID) > 128 ||
		strings.TrimSpace(input.InterruptAddress) == "" || len(input.InterruptAddress) > 512 || strings.TrimSpace(input.CheckpointID) == "" ||
		!validLowerSHA256(input.ProposalHash) || !validLowerSHA256(input.CheckpointPayloadSHA256) || input.CheckpointLeaseGeneration == 0 {
		return ErrApprovalCheckpointMismatch
	}
	return nil
}

func validateSamePreparedApproval(existing *mysql.AgentApproval, input PrepareApprovalInput, requestedBy string) error {
	if existing == nil || existing.RunID != input.Lease.RunID || existing.ProposalHash != input.ProposalHash ||
		existing.ToolName != input.ToolName || existing.ToolRevision != input.ToolRevision || existing.ToolSchemaHash != input.ToolSchemaHash ||
		existing.RiskLevel != string(input.RiskLevel) || existing.PolicyHash != input.PolicyHash ||
		existing.RuntimeCompatibilityHash != input.RuntimeCompatibilityHash || existing.RequestedBy != requestedBy {
		return ErrApprovalIdentityMismatch
	}
	return nil
}

func lockCheckpoint(tx *gorm.DB, run *mysql.WorkflowRun, checkpointID string) (*mysql.WorkflowCheckpoint, error) {
	var row mysql.WorkflowCheckpoint
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("run_id = ? AND eino_checkpoint_id = ?", run.ID, checkpointID).First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrApprovalCheckpointMismatch
	}
	if result.Error != nil {
		return nil, fmt.Errorf("锁定 Approval Checkpoint: %w", result.Error)
	}
	if row.CheckpointBlob == nil || row.PayloadSHA256 == nil || row.LeaseGeneration == nil || row.CommittedAt == nil ||
		row.RuntimeVersion == nil || row.RuntimeCompatibilityHash == nil || run.RuntimeVersion == nil || run.RuntimeCompatibilityHash == nil ||
		*row.RuntimeVersion != *run.RuntimeVersion || *row.RuntimeCompatibilityHash != *run.RuntimeCompatibilityHash {
		return nil, ErrApprovalCheckpointMismatch
	}
	digest := sha256.Sum256(row.CheckpointBlob)
	if *row.PayloadSHA256 != hex.EncodeToString(digest[:]) {
		return nil, ErrApprovalCheckpointMismatch
	}
	return &row, nil
}

func validateApprovalCheckpointBinding(tx *gorm.DB, run *mysql.WorkflowRun, approval *mysql.AgentApproval) error {
	if approval.CheckpointID == nil || approval.CheckpointPayloadSHA256 == nil || approval.CheckpointLeaseGeneration == nil {
		return ErrApprovalCheckpointMismatch
	}
	row, err := lockCheckpoint(tx, run, *approval.CheckpointID)
	if err != nil {
		return err
	}
	if *row.PayloadSHA256 != *approval.CheckpointPayloadSHA256 || row.LeaseGeneration == nil || *row.LeaseGeneration != *approval.CheckpointLeaseGeneration {
		return ErrApprovalCheckpointMismatch
	}
	return nil
}

func invalidateApprovalAndParkTx(tx *gorm.DB, run *mysql.WorkflowRun, lease LeaseToken, approval *mysql.AgentApproval, reason string) error {
	if approval.Status == ApprovalStatusPreparing || approval.Status == ApprovalStatusPending {
		result := tx.Model(&mysql.AgentApproval{}).Where("id = ? AND status IN ?", approval.ID, []string{ApprovalStatusPreparing, ApprovalStatusPending}).Updates(map[string]any{
			"status": ApprovalStatusInvalidated, "version": gorm.Expr("version + 1"),
			"decision_reason": reason, "decided_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		})
		if result.Error != nil {
			return fmt.Errorf("失效不兼容 Approval: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrApprovalInvalidated
		}
	}
	event := WorkflowEventInput{Type: EventApprovalInvalidated, Payload: EventPayload{
		Reference: approval.ID, Attributes: map[string]any{"approval_id": approval.ID, "reason": reason},
	}}
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	firstSeq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusRunning, lease, map[string]any{})
	if err != nil {
		return err
	}
	if err := insertDurableEvent(tx, run.ID, firstSeq, event, payload); err != nil {
		return err
	}
	parkedEvent := WorkflowEventInput{Type: EventRunParked, Payload: EventPayload{
		Reference: approval.ID, Attributes: map[string]any{"park_reason": ParkReasonApprovalInvalidated, "approval_id": approval.ID},
	}}
	parkedPayload, err := marshalDurableEvent(parkedEvent)
	if err != nil {
		return err
	}
	if err := ValidateRunTransition(run.Status, RunStatusParked, TransitionIntentDefault, ParkReasonApprovalInvalidated); err != nil {
		return err
	}
	secondSeq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, RunStatusRunning, lease, map[string]any{
		"status": RunStatusParked, "park_reason": ParkReasonApprovalInvalidated,
		"lease_owner": nil, "lease_until": nil, "heartbeat_at": nil,
	})
	if err != nil {
		return err
	}
	return insertDurableEvent(tx, run.ID, secondSeq, parkedEvent, parkedPayload)
}

func validLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func jsonObject(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}

func nullableApprovalText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
