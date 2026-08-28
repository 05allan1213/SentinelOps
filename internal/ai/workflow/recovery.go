package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

// RecoveryMode 是 phase12 写入 workflow Event 的确定性恢复选择。
type RecoveryMode string

const (
	RecoveryModeResume RecoveryMode = "resume"
	RecoveryModeReplay RecoveryMode = "replay"
	RecoveryModeParked RecoveryMode = "parked"
)

// RecoveryCheckpointState 是 opaque checkpoint 仅基于持久化元数据得到的状态。
// Corrupt 不表示项目解析了 Eino bytes；Eino 反序列化失败由 Runner 返回并进入相同 fail-closed 路径。
type RecoveryCheckpointState string

const (
	RecoveryCheckpointMissing RecoveryCheckpointState = "missing"
	RecoveryCheckpointValid   RecoveryCheckpointState = "valid"
	RecoveryCheckpointCorrupt RecoveryCheckpointState = "corrupt"
)

var (
	// ErrRecoveryDependencyInvalid 表示恢复依赖不满足 Resume / Replay 的唯一安全条件。
	ErrRecoveryDependencyInvalid = errors.New("workflow recovery dependency is invalid")
	// ErrRecoveryHandoffDenied 表示 safe-point cancel 尚无当前 generation 的完整 checkpoint。
	ErrRecoveryHandoffDenied = errors.New("workflow recovery handoff denied")
)

// RecoveryCheckpoint 保存 selector 所需的 opaque checkpoint 元数据结论。
type RecoveryCheckpoint struct {
	State           RecoveryCheckpointState
	ID              string
	LeaseGeneration uint64
}

// RecoveryFacts 是数据库恢复真值的只读快照，不接收 Controller、SSE 或客户端参数。
type RecoveryFacts struct {
	RunID                    string
	Status                   string
	ParkReason               string
	ImmutableQuery           string
	RuntimeVersion           string
	RuntimeCompatibilityHash string
	Checkpoint               RecoveryCheckpoint
	HasPublishedApproval     bool
	HasEffect                bool
}

// RecoverySelectionRecord 描述 running Run 上的 fenced Resume / Replay Event。
type RecoverySelectionRecord struct {
	Lease          LeaseToken
	Mode           RecoveryMode
	Attempt        uint
	TraceID        string
	RuntimeVersion string
}

// RecoveryParkInput 描述恢复失败后的 fail-closed parked 转换。
type RecoveryParkInput struct {
	Lease          LeaseToken
	ExpectedStatus string
	Reason         string
	Attempt        uint
	TraceID        string
	RuntimeVersion string
}

// RecoveryRestoreInput 描述 runtime_incompatible 的唯一 fenced 解锁证明。
type RecoveryRestoreInput struct {
	Lease                    LeaseToken
	CurrentCompatibilityHash string
	Mode                     RecoveryMode
	Attempt                  uint
	TraceID                  string
	RuntimeVersion           string
}

// LoadRecoveryFacts 在当前 running lease 的行锁内读取 selector 的全部数据库真值。
func (s *GORMStore) LoadRecoveryFacts(ctx context.Context, token LeaseToken) (RecoveryFacts, error) {
	if err := s.authorizeRunScope(ctx, token.RunID); err != nil {
		return RecoveryFacts{}, err
	}
	var facts RecoveryFacts
	err := s.withFencedRunTransaction(ctx, token, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		loaded, err := loadRecoveryFacts(tx, run)
		if err != nil {
			return err
		}
		facts = loaded
		return nil
	})
	return facts, err
}

// RecordRecoverySelection 原子追加 run.resumed / run.replayed，并保持 Run 为 running。
func (s *GORMStore) RecordRecoverySelection(ctx context.Context, input RecoverySelectionRecord) error {
	if err := s.authorizeRunScope(ctx, input.Lease.RunID); err != nil {
		return err
	}
	eventType, err := recoveryEventType(input.Mode)
	if err != nil {
		return err
	}
	event := recoveryEvent(eventType, input.Mode, input.Attempt, input.Lease.Generation, input.RuntimeVersion, input.TraceID, "")
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, input.Lease, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if err := validateRecoveryAttempt(run, input.Attempt, input.RuntimeVersion); err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, run.Status, input.Lease, map[string]any{})
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, event, payload)
	})
}

// ParkRecovery 原子提交恢复失败原因和 run.parked Event。
func (s *GORMStore) ParkRecovery(ctx context.Context, input RecoveryParkInput) error {
	if err := s.authorizeRunScope(ctx, input.Lease.RunID); err != nil {
		return err
	}
	if input.ExpectedStatus == "" {
		input.ExpectedStatus = RunStatusRunning
	}
	switch input.Reason {
	case ParkReasonRuntimeIncompatible, ParkReasonCheckpointMissing, ParkReasonCheckpointCorrupt:
	default:
		return fmt.Errorf("unsupported phase12 park reason %q", input.Reason)
	}
	event := recoveryEvent(EventRunParked, RecoveryModeParked, input.Attempt, input.Lease.Generation, input.RuntimeVersion, input.TraceID, input.Reason)
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, input.Lease, input.ExpectedStatus, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if err := validateRecoveryAttempt(run, input.Attempt, input.RuntimeVersion); err != nil {
			return err
		}
		if err := ValidateRunTransition(run.Status, RunStatusParked, TransitionIntentDefault, input.Reason); err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, run.Status, input.Lease, map[string]any{
			"status": RunStatusParked, "park_reason": input.Reason,
		})
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, event, payload)
	})
}

// RestoreRuntimeCompatible 只允许 exact hash 且恢复依赖仍有效的 runtime_incompatible Run 回到 pending。
func (s *GORMStore) RestoreRuntimeCompatible(ctx context.Context, input RecoveryRestoreInput) error {
	if err := s.authorizeRunScope(ctx, input.Lease.RunID); err != nil {
		return err
	}
	if len(input.CurrentCompatibilityHash) != 64 || strings.ToLower(input.CurrentCompatibilityHash) != input.CurrentCompatibilityHash {
		return fmt.Errorf("current runtime compatibility hash must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(input.CurrentCompatibilityHash); err != nil {
		return fmt.Errorf("current runtime compatibility hash is not hexadecimal: %w", err)
	}
	eventType, err := recoveryEventType(input.Mode)
	if err != nil {
		return err
	}
	event := recoveryEvent(eventType, input.Mode, input.Attempt, input.Lease.Generation, input.RuntimeVersion, input.TraceID, ParkReasonRuntimeIncompatible)
	payload, err := marshalDurableEvent(event)
	if err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, input.Lease, RunStatusParked, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		if run.ParkReason == nil || *run.ParkReason != ParkReasonRuntimeIncompatible {
			return fmt.Errorf("%w: park_reason does not allow runtime restore", ErrRecoveryDependencyInvalid)
		}
		if err := validateRecoveryAttempt(run, input.Attempt, input.RuntimeVersion); err != nil {
			return err
		}
		if run.RuntimeCompatibilityHash == nil || *run.RuntimeCompatibilityHash != input.CurrentCompatibilityHash {
			return fmt.Errorf("%w: runtime compatibility hash is not exact", ErrRecoveryDependencyInvalid)
		}
		facts, err := loadRecoveryFacts(tx, run)
		if err != nil {
			return err
		}
		if err := validateRecoveryModeDependencies(input.Mode, facts); err != nil {
			return err
		}
		if err := ValidateRunTransition(run.Status, RunStatusPending, TransitionIntentRuntimeRestored, *run.ParkReason); err != nil {
			return err
		}
		seq, err := updateFencedDurableRunAndAllocateSeq(tx, run.ID, run.Status, input.Lease, map[string]any{
			"status": RunStatusPending, "park_reason": nil, "lease_owner": nil,
			"lease_until": nil, "heartbeat_at": nil, "available_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		})
		if err != nil {
			return err
		}
		return insertDurableEvent(tx, run.ID, seq, event, payload)
	})
}

// RequireCommittedCheckpoint 只确认当前有效 generation 已提交完整 checkpoint 后才允许安全交接。
func (s *GORMStore) RequireCommittedCheckpoint(ctx context.Context, token LeaseToken) error {
	if err := s.authorizeRunScope(ctx, token.RunID); err != nil {
		return err
	}
	return s.withFencedRunTransaction(ctx, token, RunStatusRunning, func(tx *gorm.DB, run *mysql.WorkflowRun) error {
		facts, err := loadRecoveryFacts(tx, run)
		if err != nil {
			return err
		}
		if facts.Checkpoint.State != RecoveryCheckpointValid || facts.Checkpoint.LeaseGeneration != token.Generation {
			return ErrRecoveryHandoffDenied
		}
		return nil
	})
}

func loadRecoveryFacts(tx *gorm.DB, run *mysql.WorkflowRun) (RecoveryFacts, error) {
	if run.RuntimeVersion == nil || run.RuntimeCompatibilityHash == nil || run.ImmutableInputJSON == nil {
		return RecoveryFacts{}, ErrDurablePrimitiveRequired
	}
	query, err := immutableRunQuery(*run.ImmutableInputJSON, run.QueryText)
	if err != nil {
		return RecoveryFacts{}, err
	}
	facts := RecoveryFacts{
		RunID: run.ID, Status: run.Status, ImmutableQuery: query,
		RuntimeVersion: *run.RuntimeVersion, RuntimeCompatibilityHash: *run.RuntimeCompatibilityHash,
		Checkpoint: RecoveryCheckpoint{State: RecoveryCheckpointMissing},
	}
	if run.ParkReason != nil {
		facts.ParkReason = *run.ParkReason
	}

	expectedCheckpointID, err := EinoCheckpointID(run.ID)
	if err != nil {
		return RecoveryFacts{}, err
	}
	var checkpoints []mysql.WorkflowCheckpoint
	if err := tx.Unscoped().Where("run_id = ?", run.ID).Order("created_at DESC").Find(&checkpoints).Error; err != nil {
		return RecoveryFacts{}, fmt.Errorf("读取 Run checkpoint 元数据: %w", err)
	}
	if len(checkpoints) > 0 {
		facts.Checkpoint = RecoveryCheckpoint{State: RecoveryCheckpointCorrupt, ID: expectedCheckpointID}
		for index := range checkpoints {
			row := &checkpoints[index]
			if row.EinoCheckpointID == nil || *row.EinoCheckpointID != expectedCheckpointID {
				continue
			}
			facts.Checkpoint.ID = expectedCheckpointID
			if validRecoveryCheckpoint(row, run, expectedCheckpointID) {
				facts.Checkpoint.State = RecoveryCheckpointValid
				facts.Checkpoint.LeaseGeneration = *row.LeaseGeneration
			}
			break
		}
	}

	if err := tx.Table("agent_approvals").Where("run_id = ? AND (published_at IS NOT NULL OR status <> ?)", run.ID, "preparing").
		Limit(1).Select("1").Scan(&facts.HasPublishedApproval).Error; err != nil {
		return RecoveryFacts{}, fmt.Errorf("读取已发布 Approval 恢复依赖: %w", err)
	}
	var effectCount int64
	if err := tx.Table("agent_effects").Where("run_id = ?", run.ID).Limit(1).Count(&effectCount).Error; err != nil {
		return RecoveryFacts{}, fmt.Errorf("读取 Effect 恢复依赖: %w", err)
	}
	facts.HasEffect = effectCount > 0
	return facts, nil
}

func validRecoveryCheckpoint(row *mysql.WorkflowCheckpoint, run *mysql.WorkflowRun, expectedID string) bool {
	if row == nil || row.ID != einoCheckpointRowID(expectedID) || row.RunID != run.ID || row.CheckpointKey != expectedID ||
		row.EinoCheckpointID == nil || *row.EinoCheckpointID != expectedID ||
		row.CheckpointBlob == nil ||
		row.PayloadSHA256 == nil || row.RuntimeVersion == nil || row.RuntimeCompatibilityHash == nil ||
		row.LeaseGeneration == nil || *row.LeaseGeneration == 0 || *row.LeaseGeneration > run.LeaseGeneration ||
		row.CommittedAt == nil || (row.ExpiresAt != nil && !row.ExpiresAt.After(time.Now())) {
		return false
	}
	if *row.RuntimeVersion != *run.RuntimeVersion || *row.RuntimeCompatibilityHash != *run.RuntimeCompatibilityHash {
		return false
	}
	digest := sha256.Sum256(row.CheckpointBlob)
	return *row.PayloadSHA256 == hex.EncodeToString(digest[:])
}

func immutableRunQuery(payload, queryText string) (string, error) {
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		return "", fmt.Errorf("解析 immutable Run input: %w", err)
	}
	if strings.TrimSpace(input.Query) == "" || input.Query != queryText {
		return "", fmt.Errorf("immutable Run query does not match frozen query_text")
	}
	return input.Query, nil
}

func validateRecoveryModeDependencies(mode RecoveryMode, facts RecoveryFacts) error {
	switch mode {
	case RecoveryModeResume:
		if facts.Checkpoint.State != RecoveryCheckpointValid {
			return fmt.Errorf("%w: Resume requires a valid checkpoint", ErrRecoveryDependencyInvalid)
		}
	case RecoveryModeReplay:
		if facts.Checkpoint.State != RecoveryCheckpointMissing || facts.HasPublishedApproval || facts.HasEffect || strings.TrimSpace(facts.ImmutableQuery) == "" {
			return fmt.Errorf("%w: Replay requires no checkpoint or published recovery dependency", ErrRecoveryDependencyInvalid)
		}
	default:
		return fmt.Errorf("unsupported recovery mode %q", mode)
	}
	return nil
}

func validateRecoveryAttempt(run *mysql.WorkflowRun, attempt uint, runtimeVersion string) error {
	if run.Attempt != attempt || run.RuntimeVersion == nil || *run.RuntimeVersion != runtimeVersion {
		return fmt.Errorf("%w: recovery attempt/runtime identity changed", ErrRunCASConflict)
	}
	return nil
}

func recoveryEventType(mode RecoveryMode) (string, error) {
	switch mode {
	case RecoveryModeResume:
		return EventRunResumed, nil
	case RecoveryModeReplay:
		return EventRunReplayed, nil
	default:
		return "", fmt.Errorf("unsupported active recovery mode %q", mode)
	}
}

func recoveryEvent(eventType string, mode RecoveryMode, attempt uint, generation uint64, runtimeVersion, traceID, parkReason string) WorkflowEventInput {
	attributes := map[string]any{
		"mode": mode, "attempt": attempt, "lease_generation": generation, "runtime_version": runtimeVersion,
	}
	if parkReason != "" {
		attributes["park_reason"] = parkReason
	}
	return WorkflowEventInput{Type: eventType, TraceID: traceID, Payload: EventPayload{Attributes: attributes}}
}
