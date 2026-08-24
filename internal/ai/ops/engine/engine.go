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

func init() {
	actions.SetAnalyzeFunc(ops_pipeline.RunEventAnalysis)
}

// TriggerForEvent 对事件触发 AI 自动运维（异步，仅 new 状态可触发）。
func TriggerForEvent(ctx context.Context, event *dao.Event) {
	AutoRunForEvent(ctx, event)
}

// AutoRunForEvent 自动触发 AI 运维，返回 RunID；事件已处理或已被接管时返回空字符串。
func AutoRunForEvent(ctx context.Context, event *dao.Event) string {
	if !canAutoStartOpsForStatus(event.Status) {
		return ""
	}
	claimed, err := dao.ClaimEventForOps(ctx, event.ID)
	if err != nil {
		g.Log().Warningf(ctx, "[ops] 抢占事件运维处理权失败 | eventID=%s | err=%v", event.ID, err)
		return ""
	}
	if !claimed {
		g.Log().Infof(ctx, "[ops] 事件已被其他运维任务接管，跳过重复自动触发 | eventID=%s", event.ID)
		return ""
	}
	event.Status = "processing"
	return startRun(event)
}

// DirectRunForEvent 手动对事件执行 AI 运维，返回 RunID。
func DirectRunForEvent(ctx context.Context, event *dao.Event) string {
	if !canManualStartOpsForStatus(event.Status) {
		return ""
	}
	return startRun(event)
}

func startRun(event *dao.Event) string {
	runID := uuid.New().String()
	go func() {
		if err := ops_pipeline.ExecuteRun(context.Background(), runID, event); err != nil {
			g.Log().Warningf(context.Background(), "[ops] 运维执行失败: %v", err)
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
