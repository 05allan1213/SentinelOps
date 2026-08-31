package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxLeaseDuration = 24 * time.Hour

// ClaimInput 描述一次 Worker 原子认领请求；租约时间统一由 MySQL 计算。
type ClaimInput struct {
	Owner          string
	LeaseDuration  time.Duration
	RuntimeVersion string
}

// ClaimedRun 返回被认领的 Run 快照和唯一 fenced write token。
type ClaimedRun struct {
	Run   mysql.WorkflowRun
	Token LeaseToken
}

// ClaimNextRun 使用 MySQL 行锁原子认领一个 eligible Run 并写 run.claimed。
func (s *GORMStore) ClaimNextRun(ctx context.Context, input ClaimInput) (*ClaimedRun, bool, error) {
	if err := validateLeaseOwner(input.Owner); err != nil {
		return nil, false, err
	}
	if input.LeaseDuration < time.Millisecond || input.LeaseDuration > maxLeaseDuration {
		return nil, false, fmt.Errorf("lease duration must be between 1ms and %s", maxLeaseDuration)
	}

	var claimed *ClaimedRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		query := applyDurableRuntimeContract(tx.Model(&mysql.WorkflowRun{})).
			Where("attempt < max_attempts").
			Where(`(
				(status = ? AND available_at <= CURRENT_TIMESTAMP(3) AND (lease_until IS NULL OR lease_until <= CURRENT_TIMESTAMP(3)))
				OR
				(status = ? AND available_at <= CURRENT_TIMESTAMP(3) AND lease_owner IS NULL)
				OR
				(status = ? AND (lease_owner IS NULL OR lease_until <= CURRENT_TIMESTAMP(3)))
			)`, RunStatusPending, RunStatusRetryableFailed, RunStatusRunning).
			Order("priority DESC, available_at ASC, id ASC").
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Limit(1)
		if strings.TrimSpace(input.RuntimeVersion) != "" {
			query = query.Where("runtime_version = ?", input.RuntimeVersion)
		}
		result := query.Take(&run)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return fmt.Errorf("锁定可认领 workflow Run: %w", result.Error)
		}

		if run.Status == RunStatusRetryableFailed {
			if err := ValidateRunTransition(run.Status, RunStatusPending, TransitionIntentRetryReady, ""); err != nil {
				return err
			}
			ready := tx.Model(&mysql.WorkflowRun{}).
				Where("id = ? AND runtime_mode = ? AND status = ? AND lease_generation = ?", run.ID, RuntimeModeDurableV1, RunStatusRetryableFailed, run.LeaseGeneration).
				Update("status", RunStatusPending)
			if ready.Error != nil {
				return fmt.Errorf("恢复可重试 workflow Run 为 pending: %w", ready.Error)
			}
			if ready.RowsAffected != 1 {
				return ErrLeaseLost
			}
			run.Status = RunStatusPending
		}
		updates := map[string]any{
			"status":           RunStatusRunning,
			"lease_owner":      input.Owner,
			"lease_until":      gorm.Expr("DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL ? MICROSECOND)", input.LeaseDuration.Microseconds()),
			"heartbeat_at":     gorm.Expr("CURRENT_TIMESTAMP(3)"),
			"lease_generation": gorm.Expr("lease_generation + 1"),
			"attempt":          gorm.Expr("attempt + 1"),
			"last_event_seq":   gorm.Expr("last_event_seq + 1"),
		}
		updateResult := tx.Model(&mysql.WorkflowRun{}).
			Where("id = ? AND runtime_mode = ? AND status = ? AND lease_generation = ?", run.ID, RuntimeModeDurableV1, run.Status, run.LeaseGeneration).
			Updates(updates)
		if updateResult.Error != nil {
			return fmt.Errorf("认领 workflow Run: %w", updateResult.Error)
		}
		if updateResult.RowsAffected != 1 {
			return ErrLeaseLost
		}
		if err := tx.Where("id = ?", run.ID).First(&run).Error; err != nil {
			return fmt.Errorf("读取已认领 workflow Run: %w", err)
		}
		event := WorkflowEventInput{
			Type: EventRunClaimed,
			Payload: EventPayload{Attributes: map[string]any{
				"owner":                      input.Owner,
				"lease_generation":           run.LeaseGeneration,
				"attempt":                    run.Attempt,
				"runtime_version":            valueOrEmpty(run.RuntimeVersion),
				"runtime_compatibility_hash": valueOrEmpty(run.RuntimeCompatibilityHash),
			}},
		}
		payload, err := marshalDurableEvent(event)
		if err != nil {
			return err
		}
		if err := insertDurableEvent(tx, run.ID, run.LastEventSeq, event, payload); err != nil {
			return err
		}
		if err := beginAttemptTx(tx, &run, LeaseToken{RunID: run.ID, Owner: input.Owner, Generation: run.LeaseGeneration}, input.Owner); err != nil {
			return err
		}
		claimed = &ClaimedRun{
			Run: run,
			Token: LeaseToken{
				RunID: run.ID, Owner: input.Owner, Generation: run.LeaseGeneration,
			},
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return claimed, claimed != nil, nil
}

func validateLeaseOwner(owner string) error {
	if strings.TrimSpace(owner) == "" || len(owner) > 128 {
		return fmt.Errorf("lease owner must contain 1 to 128 bytes")
	}
	return nil
}
