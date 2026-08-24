package limiter

import (
	"context"

	"golang.org/x/time/rate"
)

// globalLimiter 全局限流器：每秒10个请求，突发20个
// 防止大量并发 Agent 执行导致 LLM API 限流（429错误）
var globalLimiter = rate.NewLimiter(rate.Limit(10), 20)

// Wait 等待获取令牌（阻塞式），超时由 ctx 控制。
// 用于需要保证执行的场景（如用户交互式请求）。
func Wait(ctx context.Context) error {
	return globalLimiter.Wait(ctx)
}

// Allow 尝试获取令牌（非阻塞），立即返回是否成功。
// 用于可选执行的场景（如后台任务）。
func Allow() bool {
	return globalLimiter.Allow()
}

// SetRate 动态调整限流速率（运行时可通过配置热更新）
func SetRate(requestsPerSecond float64, burst int) {
	globalLimiter.SetLimit(rate.Limit(requestsPerSecond))
	globalLimiter.SetBurst(burst)
}
