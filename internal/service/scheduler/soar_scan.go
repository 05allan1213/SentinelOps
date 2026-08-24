// soar_scan.go AI 运维补偿扫描：定时扫描历史高危/严重事件，对未触发过 AI 运维的事件补发触发。
//
// 设计思想（行业最佳实践）：
//
//	实时触发（ingest 入库时）只能覆盖新事件，历史存量高危事件（如系统上线前已入库、
//	或新建前已存在的事件）无法被触发。补偿扫描填补这一缺口，
//	确保所有高危/严重事件都经过 AI 运维处置，符合 Splunk SOAR / IBM QRadar SOAR 的
//	"retroactive playbook execution"（追溯执行）最佳实践。
//
// 触发条件：
//   - severity IN (high, critical)
//   - status = new（未处理）
//   - 该事件从未触发过任何运维任务（soar_runs 表中无记录）
//
// 执行频率：每 30 分钟扫描一次，每次最多处理 20 条，避免批量触发压垮系统。
package scheduler

import (
	"context"
	"time"

	"SentinelOps/internal/ai/ops/engine"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	soarScanInterval = 30 * time.Minute // 补偿扫描间隔
	soarScanBatch    = 20               // 每次最多处理条数，防止批量触发压垮系统
)

// RunOpsCompensationScan 启动 AI 运维补偿扫描后台任务，应在 main 中与 scheduler.Run 一起调用。
func RunOpsCompensationScan(ctx context.Context) {
	go func() {
		// 启动时立即执行一次，覆盖系统上线前的存量事件
		doOpsScan(ctx)
		ticker := time.NewTicker(soarScanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				doOpsScan(ctx)
			}
		}
	}()
}

// doOpsScan 执行单次补偿扫描：查询未触发 AI 运维的高危事件，逐一触发响应。
func doOpsScan(ctx context.Context) {
	events, err := dao.ListUnhandledHighSeverityEvents(ctx, soarScanBatch)
	if err != nil {
		g.Log().Warningf(ctx, "[ops-scan] 查询未处理高危事件失败: %v", err)
		return
	}
	if len(events) == 0 {
		return
	}
	g.Log().Infof(ctx, "[ops-scan] 发现 %d 条未触发 AI 运维的高危事件，开始补偿触发", len(events))
	for i := range events {
		engine.TriggerForEvent(ctx, &events[i])
	}
}
