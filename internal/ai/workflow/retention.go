package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// RetentionLeaseRunID 是复用 workflow_runs lease 字段的唯一内部清理任务身份。
	RetentionLeaseRunID  = "system:retention"
	retentionWorkflowKey = "system.retention"
	retentionRuntimeMode = "system"
)

// RetentionClaimInput 描述清理任务对现有 generation-fenced lease 的认领。
type RetentionClaimInput struct {
	Owner         string
	LeaseDuration time.Duration
}

// RetentionLease 返回清理任务持有的现有 workflow lease token。
type RetentionLease struct {
	Token      LeaseToken
	LeaseUntil time.Time
}

// RetentionReleaseInput 以同一 generation 释放清理租约并设置下次有界扫描时间。
type RetentionReleaseInput struct {
	Token     LeaseToken
	NextRunAt time.Time
}

// RetentionCleanupCounts 汇总本批次真实物理清理结果。
type RetentionCleanupCounts struct {
	RunPayloads      int64
	EventPayloads    int64
	Checkpoints      int64
	ApprovalPayloads int64
	EffectPayloads   int64
	Runs             int64
	Events           int64
	Approvals        int64
	Effects          int64
}

// EnsureRetentionLeaseRun 幂等建立内部租约行；不创建第二套 lease 表或 Scheduler。
func (s *GORMStore) EnsureRetentionLeaseRun(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("workflow Store is required for retention")
	}
	now := time.Now()
	run := mysql.WorkflowRun{
		ID: RetentionLeaseRunID, WorkflowKey: retentionWorkflowKey,
		Status: RunStatusPending, AvailableAt: now, RuntimeMode: retentionRuntimeMode,
		MaxAttempts: 1, StartedAt: now,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&run).Error
}

// ClaimRetentionLease 复用 workflow_runs 的 owner/until/generation 原子认领清理任务。
func (s *GORMStore) ClaimRetentionLease(ctx context.Context, input RetentionClaimInput) (*RetentionLease, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("workflow Store is required for retention")
	}
	if err := validateLeaseOwner(input.Owner); err != nil {
		return nil, false, err
	}
	if input.LeaseDuration < time.Millisecond || input.LeaseDuration > maxLeaseDuration {
		return nil, false, fmt.Errorf("retention lease duration must be between 1ms and %s", maxLeaseDuration)
	}
	var lease *RetentionLease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		result := tx.Where("id = ? AND available_at <= CURRENT_TIMESTAMP(3) AND ((status = ? AND lease_owner IS NULL) OR (status = ? AND (lease_until IS NULL OR lease_until <= CURRENT_TIMESTAMP(3))))",
			RetentionLeaseRunID, RunStatusPending, RunStatusRunning).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Take(&run)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return fmt.Errorf("claim retention lease row: %w", result.Error)
		}
		update := tx.Model(&mysql.WorkflowRun{}).
			Where("id = ? AND status = ? AND lease_generation = ?", run.ID, run.Status, run.LeaseGeneration).
			Updates(map[string]any{
				"status": RunStatusRunning, "lease_owner": input.Owner,
				"lease_until":      gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL ? MICROSECOND)", input.LeaseDuration.Microseconds()),
				"heartbeat_at":     gorm.Expr("CURRENT_TIMESTAMP(3)"),
				"lease_generation": gorm.Expr("lease_generation + 1"),
			})
		if update.Error != nil {
			return fmt.Errorf("claim retention lease: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return ErrLeaseLost
		}
		if err := tx.Where("id = ?", run.ID).First(&run).Error; err != nil {
			return fmt.Errorf("load claimed retention lease: %w", err)
		}
		if run.LeaseUntil == nil {
			return fmt.Errorf("claimed retention lease has no deadline")
		}
		lease = &RetentionLease{
			Token:      LeaseToken{RunID: run.ID, Owner: input.Owner, Generation: run.LeaseGeneration},
			LeaseUntil: *run.LeaseUntil,
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return lease, lease != nil, nil
}

// ReleaseRetentionLease 只允许当前 owner/generation 释放并安排下一次扫描。
func (s *GORMStore) ReleaseRetentionLease(ctx context.Context, input RetentionReleaseInput) error {
	if s == nil || s.db == nil || input.Token.RunID != RetentionLeaseRunID || input.NextRunAt.IsZero() {
		return fmt.Errorf("valid retention lease and next run time are required")
	}
	result := s.db.WithContext(ctx).Model(&mysql.WorkflowRun{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ?",
			input.Token.RunID, RunStatusRunning, input.Token.Owner, input.Token.Generation).
		Updates(map[string]any{
			"status": RunStatusPending, "available_at": input.NextRunAt,
			"lease_owner": nil, "lease_until": nil, "heartbeat_at": nil,
		})
	if result.Error != nil {
		return fmt.Errorf("release retention lease: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// PhysicalDeleteSensitivePayloads 清空终态 Run 的敏感正文并硬删除其 Checkpoint。
func (s *GORMStore) PhysicalDeleteSensitivePayloads(ctx context.Context, cutoff time.Time, limit int) (RetentionCleanupCounts, error) {
	if s == nil || s.db == nil || cutoff.IsZero() || limit <= 0 || limit > 1000 {
		return RetentionCleanupCounts{}, fmt.Errorf("valid workflow Store, cutoff and batch limit are required")
	}
	var counts RetentionCleanupCounts
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		runIDs, err := retentionTerminalRunIDs(tx, cutoff, limit)
		if err != nil || len(runIDs) == 0 {
			return err
		}
		result := tx.Model(&mysql.WorkflowRun{}).Where("id IN ?", runIDs).Updates(map[string]any{
			"immutable_input_json": nil, "query_text": "", "context_snapshot_json": nil,
			"input_payload": "", "output_payload": "",
		})
		if result.Error != nil {
			return result.Error
		}
		counts.RunPayloads = result.RowsAffected
		result = tx.Model(&mysql.WorkflowEvent{}).Where("run_id IN ? AND payload IS NOT NULL AND payload <> ''", runIDs).Update("payload", nil)
		if result.Error != nil {
			return result.Error
		}
		counts.EventPayloads = result.RowsAffected
		result = tx.Unscoped().Where("run_id IN ?", runIDs).Delete(&mysql.WorkflowCheckpoint{})
		if result.Error != nil {
			return result.Error
		}
		counts.Checkpoints = result.RowsAffected
		result = tx.Model(&mysql.AgentApproval{}).Where("run_id IN ? AND proposal_json_redacted <> JSON_OBJECT()", runIDs).Update("proposal_json_redacted", gorm.Expr("JSON_OBJECT()"))
		if result.Error != nil {
			return result.Error
		}
		counts.ApprovalPayloads = result.RowsAffected
		result = tx.Model(&mysql.AgentEffect{}).Where("run_id IN ? AND (request_redacted IS NOT NULL OR response_redacted IS NOT NULL OR resolution_evidence_redacted IS NOT NULL)", runIDs).
			Updates(map[string]any{"request_redacted": nil, "response_redacted": nil, "resolution_evidence_redacted": nil})
		if result.Error != nil {
			return result.Error
		}
		counts.EffectPayloads = result.RowsAffected
		return nil
	})
	return counts, err
}

// PhysicalDeleteAuditMetadata 在 180 天窗口后硬删除终态 Run 及其审计明细。
func (s *GORMStore) PhysicalDeleteAuditMetadata(ctx context.Context, cutoff time.Time, limit int) (RetentionCleanupCounts, error) {
	if s == nil || s.db == nil || cutoff.IsZero() || limit <= 0 || limit > 1000 {
		return RetentionCleanupCounts{}, fmt.Errorf("valid workflow Store, cutoff and batch limit are required")
	}
	var counts RetentionCleanupCounts
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		runIDs, err := retentionTerminalRunIDs(tx, cutoff, limit)
		if err != nil || len(runIDs) == 0 {
			return err
		}
		result := tx.Unscoped().Where("run_id IN ?", runIDs).Delete(&mysql.WorkflowCheckpoint{})
		if result.Error != nil {
			return result.Error
		}
		counts.Checkpoints = result.RowsAffected
		result = tx.Where("run_id IN ?", runIDs).Delete(&mysql.WorkflowEvent{})
		if result.Error != nil {
			return result.Error
		}
		counts.Events = result.RowsAffected
		result = tx.Where("run_id IN ?", runIDs).Delete(&mysql.AgentApproval{})
		if result.Error != nil {
			return result.Error
		}
		counts.Approvals = result.RowsAffected
		result = tx.Where("run_id IN ?", runIDs).Delete(&mysql.AgentEffect{})
		if result.Error != nil {
			return result.Error
		}
		counts.Effects = result.RowsAffected
		result = tx.Unscoped().Where("id IN ?", runIDs).Delete(&mysql.WorkflowRun{})
		if result.Error != nil {
			return result.Error
		}
		counts.Runs = result.RowsAffected
		return nil
	})
	return counts, err
}

func retentionTerminalRunIDs(db *gorm.DB, cutoff time.Time, limit int) ([]string, error) {
	var ids []string
	err := db.Model(&mysql.WorkflowRun{}).
		Where("id <> ? AND status IN ? AND finished_at IS NOT NULL AND finished_at < ?", RetentionLeaseRunID, terminalRunStatuses, cutoff).
		Order("finished_at ASC, id ASC").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}
