// Package middleware 提供 HTTP 中间件集合。
//
// # 令牌桶原理
//
// 令牌桶（Token Bucket）以固定速率 QPS 向桶中投放令牌，桶容量为 burst。
// 每个请求到来时消耗一个令牌：桶中有令牌则立即放行，桶空则拒绝（返回 429）。
//
// # 限流策略
//
// 两层令牌桶：全局 QPS → 模块级用户 QPS
//
//   - 第一层（全局）：单个令牌桶 key="rl:global"，所有用户共享，超出 rate_global_qps 返回 429
//
//   - 第二层（模块级用户）：按路径归属模块 + 用户 ID 隔离，不同模块独立限速
//
//     模块        路径特征                          用户QPS  说明
//     chat        /chat/                            2       消耗 LLM，严格限制
//     event_agent /event/v1/stream, /analyze        5       AI 分析任务
//     knowledge   /knowledge/（写操作）              10      文档上传/索引
//     query       /trace/ /rageval/ /event/v1/list  30      只读查询，宽松
//     default     其他                              20      通用接口
package middleware

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcache"
	"golang.org/x/time/rate"
)

const rateLimiterTTL = 5 * time.Minute

var (
	rlOnce         sync.Once
	rlGlobalQPS    float64
	rlModuleQPS    map[string]float64
	rlLimiterCache = gcache.New()
)

func loadRLConfig() {
	rlOnce.Do(func() {
		ctx := context.Background()
		rlGlobalQPS = g.Cfg().MustGet(ctx, "limiter.rate_global_qps", 100).Float64()
		rlModuleQPS = map[string]float64{
			"chat":        g.Cfg().MustGet(ctx, "limiter.rate_chat_qps", 2).Float64(),
			"event_agent": g.Cfg().MustGet(ctx, "limiter.rate_event_qps", 5).Float64(),
			"knowledge":   g.Cfg().MustGet(ctx, "limiter.rate_knowledge_qps", 10).Float64(),
			"query":       g.Cfg().MustGet(ctx, "limiter.rate_query_qps", 30).Float64(),
			"default":     g.Cfg().MustGet(ctx, "limiter.rate_user_qps", 20).Float64(),
		}
	})
}

// resolveModule 根据路径返回模块名。
func resolveModule(path string) string {
	switch {
	case strings.Contains(path, "/chat/"):
		return "chat"
	case strings.Contains(path, "/event/v1/stream"), strings.Contains(path, "/event/v1/analyze"):
		return "event_agent"
	case strings.Contains(path, "/trace/"), strings.Contains(path, "/rageval/"),
		strings.Contains(path, "/event/v1/list"), strings.Contains(path, "/event/v1/stats"),
		strings.Contains(path, "/event/v1/trend"), strings.Contains(path, "/knowledge/v1/search"):
		return "query"
	case strings.Contains(path, "/knowledge/"):
		return "knowledge"
	default:
		return "default"
	}
}

// getLimiter 从进程内缓存获取或创建令牌桶。
// burst 设为 ceil(qps)，允许短暂突发；TTL 5 分钟后自动回收不活跃用户的桶。
func getLimiter(ctx context.Context, key string, qps float64) (*rate.Limiter, error) {
	v, err := rlLimiterCache.GetOrSetFuncLock(ctx, key, func(ctx context.Context) (interface{}, error) {
		burst := int(math.Ceil(qps))
		if burst < 1 {
			burst = 1
		}
		return rate.NewLimiter(rate.Limit(qps), burst), nil
	}, rateLimiterTTL)
	if err != nil || v == nil {
		return nil, err
	}
	var limiter *rate.Limiter
	if err = v.Scan(&limiter); err != nil {
		return nil, err
	}
	return limiter, nil
}

// checkAllow 非阻塞令牌桶检查，fail-open。
// 使用 Reserve() 而非 Allow()：若令牌需等待（Delay > 0）则取消预约并拒绝，
// 避免请求积压在桶内；获取令牌失败时 fail-open 放行，防止缓存故障导致服务不可用。
func checkAllow(ctx context.Context, key string, qps float64) bool {
	limiter, err := getLimiter(ctx, key, qps)
	if err != nil || limiter == nil {
		g.Log().Warningf(ctx, "[RateLimit] fail-open | key=%s err=%v", key, err)
		return true
	}
	rev := limiter.Reserve()
	if !rev.OK() {
		return false
	}
	if rev.Delay() == 0 {
		return true
	}
	rev.Cancel()
	return false
}

// RateLimitMiddleware 两层令牌桶限流：全局 QPS → 模块级用户 QPS。
// 登录/注册接口跳过限流；未登录请求直接返回 401（依赖 JWT 中间件已将 auth_user_id 注入 ctx）。
func RateLimitMiddleware(r *ghttp.Request) {
	path := r.URL.Path
	if strings.Contains(path, "/auth/v1/login") || strings.Contains(path, "/auth/v1/register") {
		r.Middleware.Next()
		return
	}
	loadRLConfig()

	uid := r.GetCtxVar("auth_user_id").String()
	if uid == "" {
		r.Response.WriteHeader(401)
		r.Response.WriteJson(g.Map{"code": 401, "message": "未登录"})
		r.ExitAll()
		return
	}

	ctx := r.Context()

	// 第一层：全局令牌桶，所有用户共享，超出 rate_global_qps 返回 429
	if !checkAllow(ctx, "rl:global", rlGlobalQPS) {
		r.Response.WriteHeader(429)
		r.Response.WriteJson(g.Map{"code": 429, "message": "服务繁忙，请稍后重试"})
		r.ExitAll()
		return
	}

	// 第二层：模块级用户令牌桶，key = "rl:{module}:{uid}"，按模块独立限速
	module := resolveModule(path)
	if !checkAllow(ctx, "rl:"+module+":"+uid, rlModuleQPS[module]) {
		r.Response.WriteHeader(429)
		r.Response.WriteJson(g.Map{"code": 429, "message": "请求过于频繁，请稍后重试"})
		r.ExitAll()
		return
	}

	r.Middleware.Next()
}
