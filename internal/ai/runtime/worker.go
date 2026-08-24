package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/workflow"
)

// WorkerConfig 是 P09 Worker 雏形所需的 lease 与有界空轮询参数。
type WorkerConfig struct {
	Owner          string
	LeaseDuration  time.Duration
	MinPollBackoff time.Duration
	MaxPollBackoff time.Duration
}

// Worker 只委派唯一 workflow.GORMStore；P20 才接入 durable poll loop。
type Worker struct {
	store  *workflow.GORMStore
	config WorkerConfig
}

// NewWorker 创建无进程内调度真值的 Worker 雏形。
func NewWorker(store *workflow.GORMStore, config WorkerConfig) (*Worker, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow Store is required")
	}
	if strings.TrimSpace(config.Owner) == "" || len(config.Owner) > 128 {
		return nil, fmt.Errorf("worker owner must contain 1 to 128 bytes")
	}
	if config.LeaseDuration < time.Millisecond || config.LeaseDuration > 24*time.Hour {
		return nil, fmt.Errorf("worker lease duration must be between 1ms and 24h")
	}
	if config.MinPollBackoff <= 0 || config.MaxPollBackoff < config.MinPollBackoff {
		return nil, fmt.Errorf("worker poll backoff bounds are invalid")
	}
	return &Worker{store: store, config: config}, nil
}

// ClaimNext 尝试认领一个 Run；空结果由调用方使用 NextPollBackoff 退避。
func (w *Worker) ClaimNext(ctx context.Context) (*workflow.ClaimedRun, bool, error) {
	return w.store.ClaimNextRun(ctx, workflow.ClaimInput{
		Owner: w.config.Owner, LeaseDuration: w.config.LeaseDuration,
	})
}

// Heartbeat 延长当前 generation 的有效 lease。
func (w *Worker) Heartbeat(ctx context.Context, token workflow.LeaseToken) error {
	return w.store.HeartbeatLease(ctx, token, w.config.LeaseDuration)
}

// ReapExpired 有界失效过期 lease；不启动第二个 Scheduler 或 Queue。
func (w *Worker) ReapExpired(ctx context.Context, limit int) (int64, error) {
	return w.store.ReapExpiredLeases(ctx, workflow.ReapInput{Limit: limit})
}

// NextPollBackoff 返回指数增长且受配置上限约束的空轮询等待时间。
func (w *Worker) NextPollBackoff(consecutiveEmpty int) time.Duration {
	if consecutiveEmpty <= 0 {
		return w.config.MinPollBackoff
	}
	delay := w.config.MinPollBackoff
	for range consecutiveEmpty {
		if delay >= w.config.MaxPollBackoff/2 {
			return w.config.MaxPollBackoff
		}
		delay *= 2
	}
	if delay > w.config.MaxPollBackoff {
		return w.config.MaxPollBackoff
	}
	return delay
}
