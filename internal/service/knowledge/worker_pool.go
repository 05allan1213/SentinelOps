package knowledge

import (
	"context"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	// maxRetries 索引失败后的最大重试次数（不含首次执行）。
	// 重试间隔按指数退避：retry1=30s, retry2=60s, retry3=90s。
	maxRetries = 3
)

// IndexTask 异步索引任务。
type IndexTask struct {
	DocID      string // 文档 ID
	BaseID     string // 所属知识库 ID
	RetryCount int    // 当前已重试次数（0 = 首次执行）
}

// WorkerPool 异步文档索引队列（固定线程池）。
type WorkerPool struct {
	queue chan IndexTask
	wg    sync.WaitGroup
}

var (
	globalPool     *WorkerPool
	globalPoolOnce sync.Once
)

// GetWorkerPool 返回全局单例 Worker Pool（懒初始化）。
func GetWorkerPool() *WorkerPool {
	globalPoolOnce.Do(func() {
		globalPool = &WorkerPool{
			queue: make(chan IndexTask, 1000),
		}
	})
	return globalPool
}

// Start 启动 n 个 worker goroutine，监听队列并执行索引任务。
// 应在应用启动时（main.go）调用一次。
func (p *WorkerPool) Start(ctx context.Context, workers int) {
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go func(workerID int) {
			defer p.wg.Done()
			for {
				select {
				case task, ok := <-p.queue:
					if !ok {
						return
					}
					g.Log().Infof(ctx, "[knowledge:worker%d] 开始索引文档 %s（retry=%d）", workerID, task.DocID, task.RetryCount)
					if err := buildDocIndex(ctx, task); err != nil {
						g.Log().Errorf(ctx, "[knowledge:worker%d] 索引文档 %s 失败: %v", workerID, task.DocID, err)
						// 自动重试：未超过最大重试次数时，按指数退避重新入队
						if task.RetryCount < maxRetries {
							task.RetryCount++
							delay := time.Duration(task.RetryCount) * 30 * time.Second
							g.Log().Infof(ctx, "[knowledge:worker%d] 文档 %s 将在 %s 后重试（第%d次）",
								workerID, task.DocID, delay, task.RetryCount)
							go func(t IndexTask) {
								time.Sleep(delay)
								p.Submit(t)
							}(task)
						} else {
							g.Log().Warningf(ctx, "[knowledge:worker%d] 文档 %s 已达最大重试次数(%d)，不再重试",
								workerID, task.DocID, maxRetries)
						}
					}
				case <-ctx.Done():
					return
				}
			}
		}(i + 1)
	}
	g.Log().Infof(ctx, "[knowledge] Worker Pool 已启动，workers=%d，队列容量=%d", workers, cap(p.queue))
}

// Submit 将索引任务投入队列（非阻塞）。
// 若队列已满（达到 1000 容量上限），任务被丢弃并记录 Warning 日志。
// 文档状态将停留在 pending，用户可通过 RebuildDoc 手动重试。
func (p *WorkerPool) Submit(task IndexTask) {
	select {
	case p.queue <- task:
	default:
		g.Log().Warningf(context.Background(),
			"[knowledge] 索引队列已满（容量=%d），文档 %s 任务丢弃，请手动触发重建",
			cap(p.queue), task.DocID)
	}
}

// GetQueue 返回任务通道（用于获取队列长度等状态信息）。
func (p *WorkerPool) GetQueue() chan IndexTask {
	return p.queue
}

// StartWorkerPool 便捷函数：初始化并启动全局 Worker Pool。
// workers 从配置读取，默认 3。
func StartWorkerPool(ctx context.Context) {
	workers := 3
	if v, err := g.Cfg().Get(ctx, "knowledge.index_workers"); err == nil && v.Int() > 0 {
		workers = v.Int()
	}
	GetWorkerPool().Start(ctx, workers)
}
