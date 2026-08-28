package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const einoCheckpointIDPrefix = "sentinelops/run/"

var (
	// ErrCheckpointContextMissing 表示 Eino Set 未携带 phase09 唯一租约身份。
	ErrCheckpointContextMissing = errors.New("workflow checkpoint lease context missing")
	// ErrCheckpointIDMismatch 表示 checkpoint ID 并非由 context 中的 Run 派生。
	ErrCheckpointIDMismatch = errors.New("workflow checkpoint id does not match lease run")
	// ErrCheckpointDeleteDenied 表示 Run 尚未进入允许清理 Checkpoint 的终态。
	ErrCheckpointDeleteDenied = errors.New("workflow checkpoint delete denied")
)

type leaseTokenContextKey struct{}

var (
	_ adk.CheckPointStore   = (*GORMStore)(nil)
	_ adk.CheckPointDeleter = (*GORMStore)(nil)
)

// ContextWithLeaseToken 将 phase09 唯一 fenced write identity 传给 Eino Store。
func ContextWithLeaseToken(ctx context.Context, token LeaseToken) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("checkpoint context is required")
	}
	if err := validateLeaseToken(token); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, leaseTokenContextKey{}, token), nil
}

// LeaseTokenFromContext 读取同一 phase09 token，供后续 typed RuntimeContext 复用。
func LeaseTokenFromContext(ctx context.Context) (LeaseToken, error) {
	if ctx == nil {
		return LeaseToken{}, ErrCheckpointContextMissing
	}
	token, ok := ctx.Value(leaseTokenContextKey{}).(LeaseToken)
	if !ok {
		return LeaseToken{}, ErrCheckpointContextMissing
	}
	if err := validateLeaseToken(token); err != nil {
		return LeaseToken{}, err
	}
	return token, nil
}

// EinoCheckpointID 从 durable Run ID 确定性派生唯一 Eino checkpoint ID。
func EinoCheckpointID(runID string) (string, error) {
	if strings.TrimSpace(runID) == "" {
		return "", fmt.Errorf("checkpoint run id is required")
	}
	if len(runID) > 64 {
		return "", fmt.Errorf("checkpoint run id exceeds schema limit")
	}
	return einoCheckpointIDPrefix + runID, nil
}

// EinoCheckpointOption 使用官方 WithCheckPointID 固化同一 Run 的恢复身份。
func EinoCheckpointOption(runID string) (adk.AgentRunOption, error) {
	checkpointID, err := EinoCheckpointID(runID)
	if err != nil {
		return adk.AgentRunOption{}, err
	}
	return adk.WithCheckPointID(checkpointID), nil
}

// Get 原样读取 Eino opaque bytes；legacy JSON 或 NULL blob 不构成 Eino Checkpoint。
func (s *GORMStore) Get(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	if _, err := runIDFromEinoCheckpointID(checkpointID); err != nil {
		return nil, false, err
	}
	var row mysql.WorkflowCheckpoint
	result := s.db.WithContext(ctx).
		Select("checkpoint_blob").
		Where("eino_checkpoint_id = ? AND checkpoint_blob IS NOT NULL", checkpointID).
		First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if result.Error != nil {
		return nil, false, fmt.Errorf("读取 Eino checkpoint: %w", result.Error)
	}
	payload := make([]byte, len(row.CheckpointBlob))
	copy(payload, row.CheckpointBlob)
	return payload, true, nil
}

// Set 在当前 running lease 的同一事务中幂等写入 Eino opaque bytes 与校验元数据。
func (s *GORMStore) Set(ctx context.Context, checkpointID string, checkpoint []byte) error {
	token, err := LeaseTokenFromContext(ctx)
	if err != nil {
		return err
	}
	expectedID, err := EinoCheckpointID(token.RunID)
	if err != nil {
		return err
	}
	if checkpointID != expectedID {
		return ErrCheckpointIDMismatch
	}

	payload := append([]byte{}, checkpoint...)
	payloadHash := sha256.Sum256(payload)
	payloadSHA256 := hex.EncodeToString(payloadHash[:])
	return s.withFencedRunTransaction(ctx, token, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if run.RuntimeVersion == nil || run.RuntimeCompatibilityHash == nil {
			return ErrDurablePrimitiveRequired
		}
		var committedAt time.Time
		if err := tx.Raw("SELECT CURRENT_TIMESTAMP(3)").Scan(&committedAt).Error; err != nil {
			return fmt.Errorf("读取 MySQL checkpoint 提交时间: %w", err)
		}
		runtimeVersion := *run.RuntimeVersion
		runtimeCompatibilityHash := *run.RuntimeCompatibilityHash
		generation := token.Generation
		row := mysql.WorkflowCheckpoint{
			ID:                       einoCheckpointRowID(checkpointID),
			RunID:                    token.RunID,
			CheckpointKey:            checkpointID,
			SnapshotJSON:             `{}`,
			EinoCheckpointID:         &checkpointID,
			CheckpointBlob:           payload,
			PayloadSHA256:            &payloadSHA256,
			RuntimeVersion:           &runtimeVersion,
			RuntimeCompatibilityHash: &runtimeCompatibilityHash,
			LeaseGeneration:          &generation,
			CommittedAt:              &committedAt,
		}
		updates := map[string]any{
			"run_id":                     token.RunID,
			"checkpoint_key":             checkpointID,
			"snapshot_json":              `{}`,
			"checkpoint_blob":            payload,
			"payload_sha256":             payloadSHA256,
			"runtime_version":            runtimeVersion,
			"runtime_compatibility_hash": runtimeCompatibilityHash,
			"lease_generation":           generation,
			"committed_at":               gorm.Expr("CURRENT_TIMESTAMP(3)"),
			"expires_at":                 nil,
			"deleted_at":                 nil,
			"updated_at":                 gorm.Expr("CURRENT_TIMESTAMP(3)"),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "eino_checkpoint_id"}},
			DoUpdates: clause.Assignments(updates),
		}).Create(&row).Error; err != nil {
			return fmt.Errorf("写入 fenced Eino checkpoint: %w", err)
		}
		return nil
	})
}

// Delete 只在 durable Run 进入终态后物理清理对应 Eino Checkpoint。
func (s *GORMStore) Delete(ctx context.Context, checkpointID string) error {
	runID, err := runIDFromEinoCheckpointID(checkpointID)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run mysql.WorkflowRun
		result := applyDurableRuntimeContract(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&mysql.WorkflowRun{})).
			Where("id = ?", runID).
			First(&run)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ErrDurablePrimitiveRequired
		}
		if result.Error != nil {
			return fmt.Errorf("锁定 checkpoint 所属 durable Run: %w", result.Error)
		}
		if RunOccupiesSession(run.Status) {
			return fmt.Errorf("%w: run status %s", ErrCheckpointDeleteDenied, run.Status)
		}
		if err := tx.Unscoped().
			Where("run_id = ? AND eino_checkpoint_id = ?", runID, checkpointID).
			Delete(&mysql.WorkflowCheckpoint{}).Error; err != nil {
			return fmt.Errorf("删除终态 Eino checkpoint: %w", err)
		}
		return nil
	})
}

func runIDFromEinoCheckpointID(checkpointID string) (string, error) {
	if !strings.HasPrefix(checkpointID, einoCheckpointIDPrefix) {
		return "", fmt.Errorf("invalid Eino checkpoint id")
	}
	runID := strings.TrimPrefix(checkpointID, einoCheckpointIDPrefix)
	expectedID, err := EinoCheckpointID(runID)
	if err != nil || expectedID != checkpointID {
		return "", fmt.Errorf("invalid Eino checkpoint id")
	}
	return runID, nil
}

func einoCheckpointRowID(checkpointID string) string {
	digest := sha256.Sum256([]byte(checkpointID))
	return hex.EncodeToString(digest[:])
}
