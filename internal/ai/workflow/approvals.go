package workflow

import (
	"context"
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

const (
	ApprovalStatusPreparing   = "preparing"
	ApprovalStatusPending     = "pending"
	ApprovalStatusApproved    = "approved"
	ApprovalStatusRejected    = "rejected"
	ApprovalStatusExpired     = "expired"
	ApprovalStatusInvalidated = "invalidated"
)

var (
	// ErrApprovalNotFound 对不存在或不可对外观察的 Approval 返回同一结果。
	ErrApprovalNotFound = errors.New("APPROVAL_NOT_FOUND")
	// ErrApprovalAlreadyDecided 表示终态 Approval 收到了冲突决定。
	ErrApprovalAlreadyDecided = errors.New("APPROVAL_ALREADY_DECIDED")
	// ErrApprovalExpired 表示 Approval 已过期，不能批准或拒绝。
	ErrApprovalExpired = errors.New("APPROVAL_EXPIRED")
	// ErrApprovalVersionConflict 表示客户端 version 不再是 pending Approval 的当前版本。
	ErrApprovalVersionConflict = errors.New("APPROVAL_VERSION_CONFLICT")
	// ErrApprovalProposalMismatch 表示客户端回传的 proposal_hash 与持久化事实不一致。
	ErrApprovalProposalMismatch = errors.New("APPROVAL_PROPOSAL_MISMATCH")
	// ErrApprovalNotExpired 表示 expiry 决策早于数据库过期时间。
	ErrApprovalNotExpired = errors.New("APPROVAL_NOT_EXPIRED")
	// ErrApprovalIdentityMismatch 表示持久化 ID 不符合稳定 Approval identity。
	ErrApprovalIdentityMismatch = errors.New("APPROVAL_IDENTITY_MISMATCH")
)

// DecideApprovalInput 是独立于 Worker lease 的 Approval version CAS 输入。
type DecideApprovalInput struct {
	ApprovalID      string
	ProposalHash    string
	ExpectedVersion uint64
	Decision        string
	Reason          string
}

// ListPendingApprovals 只返回当前身份可决策的他人 pending Approval。
func (s *GORMStore) ListPendingApprovals(ctx context.Context, limit int) ([]mysql.AgentApproval, error) {
	identity, err := authorizeApprovalActor(ctx, "")
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("approval list limit must be between 1 and 100")
	}
	var approvals []mysql.AgentApproval
	if err := s.db.WithContext(ctx).
		Where("status = ? AND requested_by <> ?", ApprovalStatusPending, identity.UserID).
		Order("published_at ASC, id ASC").
		Limit(limit).
		Find(&approvals).Error; err != nil {
		return nil, fmt.Errorf("查询 pending Approval: %w", err)
	}
	for index := range approvals {
		if err := validateStoredApprovalIdentity(&approvals[index]); err != nil {
			return nil, err
		}
	}
	return approvals, nil
}

// GetPendingApproval 只读取可决策的他人 pending Approval；preparing 与不存在统一不可见。
func (s *GORMStore) GetPendingApproval(ctx context.Context, approvalID string) (*mysql.AgentApproval, error) {
	if strings.TrimSpace(approvalID) == "" {
		return nil, ErrApprovalNotFound
	}
	var approval mysql.AgentApproval
	result := s.db.WithContext(ctx).
		Where("id = ? AND status = ?", approvalID, ApprovalStatusPending).
		First(&approval)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrApprovalNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("查询 pending Approval: %w", result.Error)
	}
	if err := validateStoredApprovalIdentity(&approval); err != nil {
		return nil, err
	}
	if _, err := authorizeApprovalActor(ctx, approval.RequestedBy); err != nil {
		return nil, err
	}
	return &approval, nil
}

// DecideApprovalAndWakeRun 原子提交 Approval CAS、Run 唤醒和唯一 Event。
// 它不接受 Worker lease，也不调用 Tool 或 Effect。
func (s *GORMStore) DecideApprovalAndWakeRun(ctx context.Context, input DecideApprovalInput) (*mysql.AgentApproval, error) {
	if err := validateApprovalDecisionInput(input); err != nil {
		return nil, err
	}
	var decided *mysql.AgentApproval
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var approval mysql.AgentApproval
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status <> ?", input.ApprovalID, ApprovalStatusPreparing).
			First(&approval)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrApprovalNotFound
		}
		if result.Error != nil {
			return fmt.Errorf("锁定 Approval: %w", result.Error)
		}
		if err := validateStoredApprovalIdentity(&approval); err != nil {
			return err
		}
		identity, err := authorizeApprovalActor(ctx, approval.RequestedBy)
		if err != nil {
			return err
		}
		if input.ProposalHash != approval.ProposalHash {
			return ErrApprovalProposalMismatch
		}
		if approval.Status != ApprovalStatusPending {
			switch {
			case approval.Status == input.Decision:
				copy := approval
				decided = &copy
				return nil
			case approval.Status == ApprovalStatusExpired:
				return ErrApprovalExpired
			default:
				return ErrApprovalAlreadyDecided
			}
		}
		if approval.Version != input.ExpectedVersion {
			return ErrApprovalVersionConflict
		}

		var databaseNow time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&databaseNow).Error; err != nil {
			return fmt.Errorf("读取 Approval 决策时间: %w", err)
		}
		due := approval.ExpiresAt != nil && !approval.ExpiresAt.After(databaseNow)
		if input.Decision == ApprovalStatusExpired && !due {
			return ErrApprovalNotExpired
		}
		if input.Decision != ApprovalStatusExpired && due {
			return ErrApprovalExpired
		}

		var run mysql.WorkflowRun
		runResult := applyDurableRuntimeContract(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&mysql.WorkflowRun{})).
			Where("id = ?", approval.RunID).
			First(&run)
		if errors.Is(runResult.Error, gorm.ErrRecordNotFound) {
			return ErrDurablePrimitiveRequired
		}
		if runResult.Error != nil {
			return fmt.Errorf("锁定 Approval Run: %w", runResult.Error)
		}
		if err := ValidateRunTransition(run.Status, RunStatusPending, TransitionIntentApprovalDecided, ""); err != nil {
			return err
		}

		eventType := EventApprovalDecided
		if input.Decision == ApprovalStatusExpired {
			eventType = EventApprovalExpired
		}
		event := WorkflowEventInput{
			Type: eventType,
			Payload: EventPayload{Reference: approval.ID, Attributes: map[string]any{
				"approval_id": approval.ID,
				"decision":    input.Decision,
			}},
		}
		eventPayload, err := marshalDurableEvent(event)
		if err != nil {
			return err
		}
		reason := policy.NewRedactor().RedactText(input.Reason)
		updates := map[string]any{
			"status":          input.Decision,
			"version":         gorm.Expr("version + 1"),
			"decided_by":      identity.UserID,
			"decision_reason": nullableApprovalReason(reason),
			"decided_at":      databaseNow,
		}
		approvalResult := tx.Model(&mysql.AgentApproval{}).
			Where("id = ? AND status = ? AND version = ?", approval.ID, ApprovalStatusPending, input.ExpectedVersion).
			Updates(updates)
		if approvalResult.Error != nil {
			return fmt.Errorf("提交 Approval CAS: %w", approvalResult.Error)
		}
		if approvalResult.RowsAffected != 1 {
			return ErrApprovalVersionConflict
		}

		runUpdate := applyDurableRuntimeContract(tx.Model(&mysql.WorkflowRun{})).
			Where("id = ? AND status = ?", run.ID, RunStatusWaitingApproval).
			Updates(map[string]any{
				"status":         RunStatusPending,
				"available_at":   databaseNow,
				"lease_owner":    nil,
				"lease_until":    nil,
				"heartbeat_at":   nil,
				"last_event_seq": gorm.Expr("last_event_seq + 1"),
			})
		if runUpdate.Error != nil {
			return fmt.Errorf("唤醒 Approval Run: %w", runUpdate.Error)
		}
		if runUpdate.RowsAffected != 1 {
			return ErrRunCASConflict
		}
		var seq uint64
		if err := tx.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Pluck("last_event_seq", &seq).Error; err != nil {
			return fmt.Errorf("读取 Approval Event seq: %w", err)
		}
		if err := insertDurableEvent(tx, run.ID, seq, event, eventPayload); err != nil {
			return err
		}
		if err := tx.First(&approval, "id = ?", approval.ID).Error; err != nil {
			return fmt.Errorf("读取已决策 Approval: %w", err)
		}
		copy := approval
		decided = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return decided, nil
}

func authorizeApprovalActor(ctx context.Context, requestedBy string) (policy.Identity, error) {
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return policy.Identity{}, err
	}
	if err := policy.Authorize(ctx, policy.PermissionDecideProposal, policy.Resource{OwnerID: requestedBy}); err != nil {
		return policy.Identity{}, err
	}
	return identity, nil
}

func validateApprovalDecisionInput(input DecideApprovalInput) error {
	if strings.TrimSpace(input.ApprovalID) == "" || len(input.ApprovalID) > 128 {
		return ErrApprovalNotFound
	}
	if len(input.ProposalHash) != 64 || strings.ToLower(input.ProposalHash) != input.ProposalHash {
		return ErrApprovalProposalMismatch
	}
	if _, err := hex.DecodeString(input.ProposalHash); err != nil {
		return ErrApprovalProposalMismatch
	}
	if input.ExpectedVersion == 0 {
		return ErrApprovalVersionConflict
	}
	switch input.Decision {
	case ApprovalStatusApproved, ApprovalStatusRejected, ApprovalStatusExpired:
		return nil
	default:
		return fmt.Errorf("invalid Approval decision %q", input.Decision)
	}
}

func validateStoredApprovalIdentity(approval *mysql.AgentApproval) error {
	expected, err := policy.ApprovalID(approval.RunID, approval.ProposalHash)
	if err != nil || expected != approval.ID {
		return ErrApprovalIdentityMismatch
	}
	return nil
}

func nullableApprovalReason(reason string) any {
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	return reason
}
