package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// RuntimeModeLegacy 标记缺少 durable runtime contract 的旧运行。
	RuntimeModeLegacy = "legacy"
	// RuntimeModeDurableV1 标记由新 API 按完整契约创建的运行。
	RuntimeModeDurableV1 = "durable_v1"
	// LegacyCutoverParkReason 记录旧运行无法由新 Worker 恢复的审计原因。
	LegacyCutoverParkReason = "legacy_cutover_not_resumable"
	// phase07 只消费 Spec 已锁定的名称，不在 phase08 前建立第二份 Event catalog。
	legacyCutoverAuditEventType = "run.failed"
	legacyCutoverMaxRuns        = 1000
)

// ErrLegacyCutoverSelectionMismatch 表示显式 allowlist 含非 legacy、已终态或不存在的 Run。
var ErrLegacyCutoverSelectionMismatch = errors.New("legacy cutover selection mismatch")

var terminalRunStatuses = []string{
	RunStatusSuccess,
	RunStatusSucceeded,
	RunStatusFailed,
	RunStatusCanceled,
}

// LegacyCutoverStats 是一次 cutover 前后都可复核的分类统计。
// LegacyTerminal 包含 CutoverTerminalized；后者作为可审计子集单独报告。
type LegacyCutoverStats struct {
	LegacyTerminal      int64 `json:"legacy_terminal"`
	LegacyNonTerminal   int64 `json:"legacy_non_terminal"`
	CutoverTerminalized int64 `json:"cutover_terminalized"`
	DurableEligible     int64 `json:"durable_eligible"`
}

// LegacyCutoverResult 记录本次明确 allowlist 的终态化数量与完成后的分类统计。
type LegacyCutoverResult struct {
	Terminalized int64              `json:"terminalized"`
	Stats        LegacyCutoverStats `json:"stats"`
}

// applyDurableRuntimeContract 添加新 Worker 必须复用的 runtime contract 防线。
// phase09 只在此基础上追加状态、可用时间和 lease 条件，不得放宽本谓词。
func applyDurableRuntimeContract(query *gorm.DB) *gorm.DB {
	return query.Where(
		"runtime_mode = ? AND immutable_input_json IS NOT NULL AND runtime_version IS NOT NULL AND TRIM(runtime_version) <> '' AND runtime_compatibility_hash IS NOT NULL AND TRIM(runtime_compatibility_hash) <> ''",
		RuntimeModeDurableV1,
	)
}

// LegacyCutoverStats 返回 legacy terminal、待 drain、已 cutover 与 durable eligible 数量。
func (s *GORMStore) LegacyCutoverStats(ctx context.Context) (LegacyCutoverStats, error) {
	return queryLegacyCutoverStats(ctx, s.db)
}

func queryLegacyCutoverStats(ctx context.Context, db *gorm.DB) (LegacyCutoverStats, error) {
	var stats LegacyCutoverStats
	if err := db.WithContext(ctx).Model(&mysql.WorkflowRun{}).
		Where("runtime_mode = ? AND status IN ?", RuntimeModeLegacy, terminalRunStatuses).
		Count(&stats.LegacyTerminal).Error; err != nil {
		return stats, fmt.Errorf("统计 legacy terminal: %w", err)
	}
	if err := db.WithContext(ctx).Model(&mysql.WorkflowRun{}).
		Where("runtime_mode = ? AND (status IS NULL OR status NOT IN ?)", RuntimeModeLegacy, terminalRunStatuses).
		Count(&stats.LegacyNonTerminal).Error; err != nil {
		return stats, fmt.Errorf("统计 legacy non-terminal: %w", err)
	}
	if err := db.WithContext(ctx).Model(&mysql.WorkflowRun{}).
		Where("runtime_mode = ? AND park_reason = ? AND status IN ?", RuntimeModeLegacy, LegacyCutoverParkReason, terminalRunStatuses).
		Count(&stats.CutoverTerminalized).Error; err != nil {
		return stats, fmt.Errorf("统计 cutover terminalized: %w", err)
	}
	if err := applyDurableRuntimeContract(db.WithContext(ctx).Model(&mysql.WorkflowRun{})).
		Count(&stats.DurableEligible).Error; err != nil {
		return stats, fmt.Errorf("统计 durable eligible: %w", err)
	}
	return stats, nil
}

// ListLegacyNonTerminalRunIDs 列出等待业务确认的 legacy 非终态 Run；不会修改数据。
func (s *GORMStore) ListLegacyNonTerminalRunIDs(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > legacyCutoverMaxRuns {
		return nil, fmt.Errorf("legacy cutover preview limit must be between 1 and %d", legacyCutoverMaxRuns)
	}
	var ids []string
	err := s.db.WithContext(ctx).Model(&mysql.WorkflowRun{}).
		Where("runtime_mode = ? AND (status IS NULL OR status NOT IN ?)", RuntimeModeLegacy, terminalRunStatuses).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("查询 legacy non-terminal: %w", err)
	}
	return ids, nil
}

// TerminalizeLegacyRuns 将明确 allowlist 中的 legacy 非终态 Run 原子标记为失败。
// 未被显式选择的旧 Run 保持原状，可继续由旧 Runtime drain。
func (s *GORMStore) TerminalizeLegacyRuns(ctx context.Context, runIDs []string) (LegacyCutoverResult, error) {
	ids, err := normalizeLegacyRunIDs(runIDs)
	if err != nil {
		return LegacyCutoverResult{}, err
	}

	var cutoverResult LegacyCutoverResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runs []mysql.WorkflowRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ? AND runtime_mode = ? AND (status IS NULL OR status NOT IN ?)", ids, RuntimeModeLegacy, terminalRunStatuses).
			Order("id ASC").Find(&runs).Error; err != nil {
			return fmt.Errorf("锁定 legacy cutover allowlist: %w", err)
		}
		if len(runs) != len(ids) {
			return fmt.Errorf("%w: requested=%d eligible=%d", ErrLegacyCutoverSelectionMismatch, len(ids), len(runs))
		}

		now := time.Now()
		for _, run := range runs {
			eventPayload, err := policy.CanonicalJSON(map[string]any{
				"action":      "legacy_cutover_terminalize",
				"from_status": run.Status,
				"park_reason": LegacyCutoverParkReason,
				"schema":      "fo/legacy-cutover/v1",
				"to_status":   RunStatusFailed,
			})
			if err != nil {
				return fmt.Errorf("构造 legacy cutover 审计 payload: %w", err)
			}
			var persistedMaxSeq uint64
			if err := tx.Model(&mysql.WorkflowEvent{}).
				Where("run_id = ?", run.ID).
				Select("COALESCE(MAX(seq), 0)").
				Scan(&persistedMaxSeq).Error; err != nil {
				return fmt.Errorf("读取 legacy run %q 最大事件序号: %w", run.ID, err)
			}
			if persistedMaxSeq < run.LastEventSeq {
				persistedMaxSeq = run.LastEventSeq
			}
			nextSeq := persistedMaxSeq + 1
			duration := int64(0)
			if !run.StartedAt.IsZero() {
				duration = now.Sub(run.StartedAt).Milliseconds()
				if duration < 0 {
					duration = 0
				}
			}
			updates := map[string]any{
				"status":             RunStatusFailed,
				"park_reason":        LegacyCutoverParkReason,
				"active_session_key": nil,
				"finished_at":        now,
				"duration_ms":        duration,
				"error_message":      "legacy run terminalized by audited cutover",
				"last_event_seq":     nextSeq,
			}
			updateResult := tx.Model(&mysql.WorkflowRun{}).Where("id = ?", run.ID).Updates(updates)
			if updateResult.Error != nil {
				return fmt.Errorf("终态化 legacy run %q: %w", run.ID, updateResult.Error)
			}
			if updateResult.RowsAffected != 1 {
				return fmt.Errorf("终态化 legacy run %q affected %d rows", run.ID, updateResult.RowsAffected)
			}
			if err := tx.Create(&mysql.WorkflowEvent{
				RunID:          run.ID,
				Seq:            nextSeq,
				EventType:      legacyCutoverAuditEventType,
				Payload:        string(eventPayload),
				PayloadVersion: 1,
				CreatedAt:      now,
			}).Error; err != nil {
				return fmt.Errorf("写入 legacy cutover 审计事件 %q: %w", run.ID, err)
			}
			cutoverResult.Terminalized++
		}
		stats, err := queryLegacyCutoverStats(ctx, tx)
		if err != nil {
			return err
		}
		cutoverResult.Stats = stats
		return nil
	})
	if err != nil {
		return LegacyCutoverResult{}, err
	}
	return cutoverResult, nil
}

func normalizeLegacyRunIDs(runIDs []string) ([]string, error) {
	if len(runIDs) == 0 {
		return nil, fmt.Errorf("legacy cutover requires an explicit non-empty run allowlist")
	}
	if len(runIDs) > legacyCutoverMaxRuns {
		return nil, fmt.Errorf("legacy cutover allowlist exceeds %d runs", legacyCutoverMaxRuns)
	}
	seen := make(map[string]struct{}, len(runIDs))
	ids := make([]string, 0, len(runIDs))
	for _, rawID := range runIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return nil, fmt.Errorf("legacy cutover run id must not be empty")
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("legacy cutover run id %q is duplicated", id)
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
