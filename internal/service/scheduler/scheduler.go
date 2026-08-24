// Package scheduler 订阅调度器：管理各订阅源的定时抓取任务。
//
// 整体架构：
//   - 每个订阅拥有独立的 goroutine（runFetchLoop），按自身 CronExpr 定时执行抓取
//   - 所有 goroutine 通过 context 级联取消，ctx.Done() 时统一优雅退出
//
// 数据流：
//
//	订阅数据（MySQL）
//	  → pipeline.Fetch（RSS/GitHub 抓取）
//	  → pipeline.Extract（提取 + 严重程度推断）
//	  → pipeline.DedupAndInsert（去重入库 MySQL，content 不写 DB）
//	  → pipeline.IndexDocuments（向量嵌入 → 存入 Milvus）
//
// 生命周期：
//   - Run()：启动时调用一次，加载所有已启用订阅并注册调度
//   - Register()：订阅 Create/Update/Resume 时热更新调度任务
//   - Unregister()：订阅 Delete/Pause 时停止对应 goroutine
package scheduler

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/pipeline"

	"github.com/gogf/gf/v2/frame/g"
)

// fetchInterval 订阅抓取的默认间隔（CronExpr 为空或无法解析时的兜底值）。
// 由 Run 从配置 scheduler.fetch_interval_minutes 读取覆盖，未配置时为 15 分钟。
var fetchInterval = 15 * time.Minute

var (
	// globalCtx 保存 Run 传入的根 context，供 Register 在 HTTP 请求生命周期外派生子 context 使用
	globalCtx context.Context
	// registry 全局订阅调度注册表，key 为订阅 ID，value 为对应 goroutine 的取消函数
	registry = &subRegistry{jobs: make(map[string]context.CancelFunc)}
)

// subRegistry 管理各订阅的调度任务，通过互斥锁保证并发安全。
type subRegistry struct {
	mu   sync.Mutex                    // 保护 jobs 的并发读写
	jobs map[string]context.CancelFunc // 订阅 ID → goroutine 取消函数
}

// Run 启动调度器，是本包的唯一初始化入口，应在 main 中调用一次。
//
// 启动顺序：
//  1. 从配置读取 fetchInterval（scheduler.fetch_interval_minutes，默认 15 分钟）
//  2. 从数据库加载所有已启用的订阅，逐一注册抓取 goroutine（不立即执行，等待第一个 tick）
//
// 参数：
//   - ctx: 根 context，ctx.Done() 时所有子 goroutine 优雅退出
func Run(ctx context.Context) {
	// 保存根 context，供后续 Register 调用时派生子 context
	globalCtx = ctx

	// 从配置读取默认抓取间隔，覆盖包级变量 fetchInterval（供 parseCronInterval 兜底使用）
	if m, err := g.Cfg().Get(ctx, "scheduler.fetch_interval_minutes"); err == nil && m.Int() > 0 {
		fetchInterval = time.Duration(m.Int()) * time.Minute
	}

	// 从数据库加载所有已启用的订阅，逐一注册独立的抓取 goroutine
	// 数据库不可用时仅打印警告，调度器仍可正常启动（新订阅通过 Register 热注册）
	enabled := true
	subs, err := dao.ListSubscriptions(ctx, &enabled)
	if err != nil {
		log.Printf("[scheduler] 加载订阅失败，跳过初始注册: %v", err)
	} else {
		for i := range subs {
			// 使用下标传地址，避免 for range 的 item 变量被循环复用导致地址相同
			registerLocked(&subs[i])
		}
	}

	log.Printf("[scheduler] 启动完成：已注册 %d 个订阅", len(registry.jobs))
}

// Register 为订阅注册（或热更新）调度任务，线程安全。
// 若同 ID 的 goroutine 已存在，先 cancel 旧任务再重新注册，实现 CronExpr 变更的无缝切换。
// 由 subscription controller 在 Create / Update / Resume 时调用。
func Register(sub *dao.Subscription) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registerLocked(sub)
}

// registerLocked 注册单个订阅的抓取 goroutine，调用方须持有 registry.mu 锁。
func registerLocked(sub *dao.Subscription) {
	// 若已有同 ID 的 goroutine 在运行，先取消再重建，防止重复抓取
	if cancel, ok := registry.jobs[sub.ID]; ok {
		cancel()
	}
	// 解析订阅的 CronExpr 为 time.Duration，无法解析时使用 fetchInterval 兜底
	interval := parseCronInterval(sub.CronExpr)
	// 派生独立的子 context，取消该订阅时只影响自身 goroutine
	subCtx, cancel := context.WithCancel(globalCtx)
	registry.jobs[sub.ID] = cancel
	go runFetchLoop(subCtx, sub.ID, interval)
	log.Printf("[scheduler] 注册订阅 %q (%s)，间隔=%v", sub.Name, sub.ID, interval)
}

// Unregister 停止指定订阅的调度任务并从注册表中移除，线程安全。
// 由 subscription controller 在 Delete / Pause 时调用。
func Unregister(subID string) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if cancel, ok := registry.jobs[subID]; ok {
		cancel() // 触发 subCtx.Done()，通知 runFetchLoop goroutine 退出
		delete(registry.jobs, subID)
		log.Printf("[scheduler] 注销订阅 %s", subID)
	}
}

// runFetchLoop 单个订阅的抓取主循环。
// 策略：启动时不立即执行，等待第一个 tick 后再开始抓取，之后按 interval 定时触发。
// ctx.Done() 时退出，对应订阅被暂停/删除时由 Unregister 触发取消。
func runFetchLoop(ctx context.Context, subID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// context 被取消（Unregister 或根 context 退出），退出循环
			return
		case <-ticker.C:
			doFetch(ctx, subID)
		}
	}
}

// doFetch 执行单次抓取，完整流程：加载订阅 → 抓取 → 提取 → 去重入库 → 向量索引 → 更新 last_fetch_at。
// 每次执行前从数据库重新加载订阅状态，确保 CronExpr、URL 等变更即时生效。
// 订阅已被暂停或删除时静默退出，避免已注销的 goroutine 写入脏数据。
func doFetch(ctx context.Context, subID string) {
	// 每次 tick 重新查询订阅状态，同时校验 enabled=true，防止已暂停订阅误抓
	sub, err := dao.FindEnabledByID(ctx, subID)
	if err != nil {
		return // 订阅已暂停或删除，本次跳过，goroutine 等待下次 tick 或 ctx.Done()
	}
	// 调用 pipeline.Fetch 按订阅类型（RSS/GitHub）抓取原始条目
	items, fetchErr := pipeline.Fetch(ctx, sub)
	if fetchErr != nil {
		g.Log().Warningf(ctx, "[scheduler] 抓取 %q 失败: %v", sub.Name, fetchErr)
		return
	}
	// 将原始条目转换为标准化 Event（推断 severity、提取 CVE 等）
	events := pipeline.Extract(items)
	// 去重后写入 MySQL，返回实际插入的事件列表（含完整 content）
	inserted, err := pipeline.DedupAndInsert(ctx, events)
	if err != nil {
		g.Log().Warningf(ctx, "[scheduler] 去重入库 %q 失败: %v", sub.Name, err)
		return
	}
	// 更新订阅的最后抓取时间，供前端展示
	if err = dao.UpdateLastFetchAt(ctx, subID, time.Now()); err != nil {
		g.Log().Warningf(ctx, "[scheduler] 更新 last_fetch_at 失败（%s）: %v", subID, err)
	}
	if len(inserted) > 0 {
		g.Log().Infof(ctx, "[scheduler] %q 新增 %d 条事件", sub.Name, len(inserted))
		// 立即对新增事件执行向量索引
		if err = pipeline.IndexDocuments(ctx, inserted); err != nil {
			g.Log().Warningf(ctx, "[scheduler] %q 向量索引失败: %v", sub.Name, err)
		}
	}
}

// parseCronInterval 将 cron 表达式解析为 time.Duration。
//
// 支持以下格式：
//   - "*/5 * * * *"：标准 5 段 cron，分钟字段步进（仅支持 */N 形式，N ≤ 1440）
//   - "0 */2 * * *"：标准 5 段 cron，小时字段步进（仅支持 0 */N 形式，N ≤ 24）
//
// 空值返回 fetchInterval（包级默认值）；无法识别的格式同样返回 fetchInterval 并打印警告日志。
func parseCronInterval(expr string) time.Duration {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fetchInterval // CronExpr 未设置，使用全局默认抓取间隔
	}
	// 解析标准 5 段 cron 表达式（仅支持简单步进形式）
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		// 分钟步进：*/N * * * * → N 分钟（N 上限 1440 防止溢出）
		if strings.HasPrefix(fields[0], "*/") {
			if n, err := strconv.Atoi(strings.TrimPrefix(fields[0], "*/")); err == nil && n > 0 && n <= 1440 {
				return time.Duration(n) * time.Minute
			}
		}
		// 小时步进：0 */N * * * → N 小时（N 上限 24 防止溢出）
		if fields[0] == "0" && strings.HasPrefix(fields[1], "*/") {
			if n, err := strconv.Atoi(strings.TrimPrefix(fields[1], "*/")); err == nil && n > 0 && n <= 24 {
				return time.Duration(n) * time.Hour
			}
		}
	}
	// 格式无法识别，退回全局默认抓取间隔并打印警告
	log.Printf("[scheduler] 无法解析 cron 表达式 %q，使用默认间隔 %v", expr, fetchInterval)
	return fetchInterval
}
