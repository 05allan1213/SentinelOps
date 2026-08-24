package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
	goredis "github.com/redis/go-redis/v9"
)

var (
	globalRedisClient *goredis.Client
	redisOnce         sync.Once
	redisInitErr      error
)

// GetRedisClient 返回全局单例 Redis 客户端（懒初始化，线程安全）。
//
// 配置从 config.yaml 的 redis 节读取，包含连接地址、认证信息、数据库索引等。
// 连通性检查：启动时立即发现配置错误，而非在首次查询时才暴露问题。
func GetRedisClient(ctx context.Context) (*goredis.Client, error) {
	redisOnce.Do(func() {
		addr, err := g.Cfg().Get(ctx, "redis.addr")
		if err != nil {
			redisInitErr = fmt.Errorf("read redis.addr from config: %w", err)
			return
		}
		password, _ := g.Cfg().Get(ctx, "redis.password")
		db, _ := g.Cfg().Get(ctx, "redis.db")

		globalRedisClient = goredis.NewClient(&goredis.Options{
			Addr:     addr.String(),
			Password: password.String(),
			DB:       db.Int(),
		})

		// 连通性检查：启动时立即发现配置错误，而非在首次查询时才暴露问题。
		if err := globalRedisClient.Ping(ctx).Err(); err != nil {
			redisInitErr = fmt.Errorf("redis ping failed (addr=%s): %w", addr.String(), err)
			globalRedisClient = nil
			return
		}
	})
	return globalRedisClient, redisInitErr
}
