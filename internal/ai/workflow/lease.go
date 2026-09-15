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

var (
	// ErrLeaseLost 表示调用方不再持有当前 Run 的有效 owner/generation 执行权。
	ErrLeaseLost = errors.New("workflow run lease lost")
)

// LeaseToken 是 Worker 写 primitive 唯一接受的最小 fenced write identity。
type LeaseToken struct {
	RunID      string
	Owner      string
	Generation uint64
}

// ReapInput 描述一次有界的过期租约失效扫描。
type ReapInput struct {
	Limit int
}

// HeartbeatLease 仅允许当前 owner/generation 延长仍有效的 running lease。
func (s *GORMStore) HeartbeatLease(ctx context.Context, token LeaseToken, leaseDuration time.Duration) error {
	if err := validateLeaseToken(token); err != nil {
		return err
	}
	if leaseDuration < time.Millisecond || leaseDuration > maxLeaseDuration {
		return fmt.Errorf("lease duration must be between 1ms and %s", maxLeaseDuration)
	}
	query := applyDurableRuntimeContract(s.db.WithContext(ctx).Model(&mysql.WorkflowRun{}))
	result := query.
		Where(`id = ? AND runtime_mode = ? AND status = ? AND lease_owner = ? AND lease_generation = ?
			AND lease_until IS NOT NULL AND lease_until > CURRENT_TIMESTAMP(3)`,
			token.RunID, RuntimeModeDurableV1, RunStatusRunning, token.Owner, token.Generation).
		Updates(map[string]any{
			"heartbeat_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
			"lease_until":  gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL ? MICROSECOND)", leaseDuration.Microseconds()),
		})
	if result.Error != nil {
		return fmt.Errorf("续期 workflow Run lease: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

// closeExpiredAttemptForTakeoverTx 在持有 Run 行锁时关闭过期租约留下的开放 Attempt，
// 让 claim 接管和显式回收具有一致的查询口径；运行中的恢复操作由恢复流程自己收口。
func closeExpiredAttemptForTakeoverTx(tx *gorm.DB, run *mysql.WorkflowRun) error {
	if run == nil {
		return ErrLeaseLost
	}
	activeOperations, err := listActiveOperationsForRunTx(tx, run.ID)
	if err != nil {
		return err
	}
	for _, operation := range activeOperations {
		if operation.Status == OperationStatusRunning {
			return nil
		}
	}
	return finishAttemptAfterLeaseExpiryTx(tx, run, run.LeaseGeneration, RunStatusFailed, "failed", "lease_expired", "worker lease expired before Attempt completion")
}

// ReapExpiredLeases 有界失效过期 running lease；generation 立即递增以拒绝旧 Worker 写入。
func (s *GORMStore) ReapExpiredLeases(ctx context.Context, input ReapInput) (int64, error) {
	if input.Limit <= 0 || input.Limit > 1000 {
		return 0, fmt.Errorf("reap limit must be between 1 and 1000")
	}

	var reaped int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reconciled, err := reconcileExpiredRunningEffectsTx(tx, input.Limit)
		if err != nil {
			return err
		}
		reaped = reconciled
		remaining := input.Limit - int(reconciled)
		if remaining == 0 {
			return nil
		}
		var runs []mysql.WorkflowRun
		result := applyDurableRuntimeContract(tx.Model(&mysql.WorkflowRun{})).
			Where("status = ? AND lease_owner IS NOT NULL AND lease_until IS NOT NULL AND lease_until <= CURRENT_TIMESTAMP(3)", RunStatusRunning).
			Order("lease_until ASC, id ASC").
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Limit(remaining).
			Find(&runs)
		if result.Error != nil {
			return fmt.Errorf("锁定过期 workflow Run lease: %w", result.Error)
		}
		for index := range runs {
			run := &runs[index]
			if err := closeExpiredAttemptForTakeoverTx(tx, run); err != nil {
				return err
			}
			updateResult := tx.Model(&mysql.WorkflowRun{}).
				Where("id = ? AND status = ? AND lease_owner = ? AND lease_generation = ? AND lease_until <= CURRENT_TIMESTAMP(3)",
					run.ID, RunStatusRunning, run.LeaseOwner, run.LeaseGeneration).
				Updates(map[string]any{
					"lease_owner":      nil,
					"lease_until":      nil,
					"heartbeat_at":     nil,
					"available_at":     gorm.Expr("CURRENT_TIMESTAMP(3)"),
					"lease_generation": gorm.Expr("lease_generation + 1"),
				})
			if updateResult.Error != nil {
				return fmt.Errorf("失效过期 workflow Run lease: %w", updateResult.Error)
			}
			if updateResult.RowsAffected != 1 {
				return ErrLeaseLost
			}
			reaped++
		}
		return nil
	})
	return reaped, err
}

// withFencedRunTransaction 在同一事务和行锁内验证 lease 后执行领域写，避免检查与写入间的接管窗口。
func (s *GORMStore) withFencedRunTransaction(
	ctx context.Context,
	token LeaseToken,
	expectedStatus string,
	write func(tx *gorm.DB, run *mysql.WorkflowRun) error,
) error {
	if err := validateLeaseToken(token); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		query := applyDurableRuntimeContract(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&mysql.WorkflowRun{}))
		result := query.
			Where("id = ?", token.RunID).
			First(&run)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrDurablePrimitiveRequired
		}
		if result.Error != nil {
			return fmt.Errorf("锁定 fenced workflow Run: %w", result.Error)
		}
		if run.Status != expectedStatus {
			return fmt.Errorf("%w: expected=%s actual=%s", ErrRunCASConflict, expectedStatus, run.Status)
		}
		var databaseNow time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&databaseNow).Error; err != nil {
			return fmt.Errorf("读取 MySQL lease 时间: %w", err)
		}
		if run.LeaseOwner == nil || *run.LeaseOwner != token.Owner || run.LeaseGeneration != token.Generation || run.LeaseUntil == nil || !run.LeaseUntil.After(databaseNow) {
			return ErrLeaseLost
		}
		return write(tx, &run)
	})
}

func updateFencedDurableRunAndAllocateSeq(
	tx *gorm.DB,
	runID, expectedStatus string,
	token LeaseToken,
	updates map[string]any,
) (uint64, error) {
	updates["last_event_seq"] = gorm.Expr("last_event_seq + 1")
	query := applyDurableRuntimeContract(tx.Model(&mysql.WorkflowRun{}))
	result := query.
		Where(`id = ? AND runtime_mode = ? AND status = ? AND lease_owner = ? AND lease_generation = ?
			AND lease_until IS NOT NULL AND lease_until > CURRENT_TIMESTAMP(3)`,
			runID, RuntimeModeDurableV1, expectedStatus, token.Owner, token.Generation).
		Updates(updates)
	if result.Error != nil {
		return 0, fmt.Errorf("更新 fenced durable Run 与 Event seq: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return 0, ErrLeaseLost
	}
	var seq uint64
	if err := tx.Model(&mysql.WorkflowRun{}).Where("id = ?", runID).Pluck("last_event_seq", &seq).Error; err != nil {
		return 0, fmt.Errorf("读取数据库分配的 Event seq: %w", err)
	}
	return seq, nil
}

func validateLeaseToken(token LeaseToken) error {
	if token.RunID == "" {
		return fmt.Errorf("lease token run id is required")
	}
	if err := validateLeaseOwner(token.Owner); err != nil {
		return err
	}
	if token.Generation == 0 {
		return fmt.Errorf("lease token generation must be positive")
	}
	return nil
}
