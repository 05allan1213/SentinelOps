// Package engine AI 运维对外入口，触发两阶段 AI 运维流水线。
package engine

import (
	"context"

	"SentinelOps/internal/ai/agent/ops_pipeline"
	"SentinelOps/internal/ai/ops/actions"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// LegacyWriteGate 是 P42 发布期兼容 Gate evaluator 的接口别名。
type LegacyWriteGate = ops_pipeline.LegacyWriteGate

// ErrLegacyOpsWritesDisabled 表示旧 Ops 直写入口默认关闭或收到 durable_v1 Context。
var ErrLegacyOpsWritesDisabled = ops_pipeline.ErrLegacyOpsWritesDisabled

func legacyOpsWritesAllowed(ctx context.Context, gate LegacyWriteGate) bool {
	return requireLegacyOpsWrites(ctx, gate) == nil
}

func requireLegacyOpsWrites(ctx context.Context, gate LegacyWriteGate) error {
	return ops_pipeline.RequireLegacyOpsWrites(ctx, gate)
}

func init() {
	actions.SetAnalyzeFunc(ops_pipeline.RunEventAnalysis)
}

// TriggerForEvent 对事件触发 AI 自动运维（异步，仅 new 状态可触发）。
func TriggerForEvent(ctx context.Context, gate LegacyWriteGate, event *dao.Event) (string, error) {
	return AutoRunForEvent(ctx, gate, event)
}

// AutoRunForEvent 自动触发 AI 运维，返回 RunID；事件已处理或已被接管时返回空字符串。
func AutoRunForEvent(ctx context.Context, gate LegacyWriteGate, event *dao.Event) (string, error) {
	if err := requireLegacyOpsWrites(ctx, gate); err != nil {
		return "", err
	}
	if !canAutoStartOpsForStatus(event.Status) {
		return "", nil
	}
	claimed, err := dao.ClaimEventForOps(ctx, event.ID)
	if err != nil {
		g.Log().Warningf(ctx, "[ops] 抢占事件运维处理权失败 | eventID=%s | err=%v", event.ID, err)
		return "", err
	}
	if !claimed {
		g.Log().Infof(ctx, "[ops] 事件已被其他运维任务接管，跳过重复自动触发 | eventID=%s", event.ID)
		return "", nil
	}
	event.Status = "processing"
	return startRun(ctx, gate, event), nil
}

// DirectRunForEvent 手动对事件执行 AI 运维，返回 RunID。
func DirectRunForEvent(ctx context.Context, gate LegacyWriteGate, event *dao.Event) (string, error) {
	if err := requireLegacyOpsWrites(ctx, gate); err != nil {
		return "", err
	}
	if !canManualStartOpsForStatus(event.Status) {
		return "", nil
	}
	return startRun(ctx, gate, event), nil
}

func startRun(ctx context.Context, gate LegacyWriteGate, event *dao.Event) string {
	runID := uuid.New().String()
	legacyCtx := context.WithoutCancel(ctx)
	go func() {
		if err := ops_pipeline.ExecuteRun(legacyCtx, gate, runID, event); err != nil {
			g.Log().Warningf(legacyCtx, "[ops] 运维执行失败: %v", err)
		}
	}()
	return runID
}

func canAutoStartOpsForStatus(status string) bool {
	return status == "new"
}

func canManualStartOpsForStatus(status string) bool {
	return status != ""
}
