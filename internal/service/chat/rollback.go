package chatsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	redisdao "SentinelOps/internal/dao/redis"

	"github.com/gogf/gf/v2/frame/g"
	"gorm.io/gorm"
)

// RollbackSession 回溯会话到指定消息索引
func RollbackWorkflowToCheckpoint(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("workflow run id is empty")
	}

	db, err := mysql.DB(ctx)
	if err != nil {
		return fmt.Errorf("init workflow store: %w", err)
	}
	store := workflow.NewGORMStore(db)

	checkpoint, err := store.LatestCheckpoint(ctx, runID, "")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("latest checkpoint not found for workflow run %s", runID)
		}
		return fmt.Errorf("latest checkpoint lookup failed for workflow run %s: %w", runID, err)
	}
	if checkpoint.RunID == "" {
		return fmt.Errorf("latest checkpoint not found for workflow run %s", runID)
	}

	if err := store.FinishRun(ctx, runID, "rolled_back", "", "rolled back to latest checkpoint"); err != nil {
		return fmt.Errorf("mark workflow run rolled back: %w", err)
	}

	g.Log().Infof(ctx, "[Rollback] Rollback workflow run %s to checkpoint %s step %s", runID, checkpoint.CheckpointID, checkpoint.Step)
	return nil
}

func RollbackSession(ctx context.Context, sessionID string, targetIndex int) (int, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return 0, err
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return 0, err
	}
	if !identity.Scope.All {
		db, dbErr := mysql.DB(ctx)
		if dbErr != nil {
			return 0, dbErr
		}
		var owned int64
		if dbErr = db.Model(&mysql.WorkflowRun{}).Where("session_id = ? AND user_id = ?", sessionID, identity.UserID).Count(&owned).Error; dbErr != nil {
			return 0, dbErr
		}
		if owned == 0 {
			return 0, policy.ErrForbidden
		}
	}
	recent, summary, err := redisdao.LoadSession(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("load session: %w", err)
	}

	if len(recent) == 0 {
		return 0, fmt.Errorf("session not found or empty")
	}

	if targetIndex < 0 || targetIndex >= len(recent) {
		return 0, fmt.Errorf("invalid targetIndex: %d (valid range: 0-%d)", targetIndex, len(recent)-1)
	}

	removedCount := len(recent) - targetIndex - 1
	recent = recent[:targetIndex+1]

	// 更新 redis 中对话历史内容
	if err := redisdao.SaveSession(ctx, sessionID, recent, summary); err != nil {
		return 0, fmt.Errorf("save session: %w", err)
	}

	// 同步更新内存中的 SessionMemory
	mem := cache.GetSessionMemory(sessionID)
	mem.SetState(recent, summary)

	g.Log().Infof(ctx, "[Rollback] Rollback session %s to index %d, removed %d messages", sessionID, targetIndex, removedCount)
	return removedCount, nil
}
