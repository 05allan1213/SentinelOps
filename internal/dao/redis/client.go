// Package redis 提供 Redis 数据访问层
package redis

import (
	"context"
	"fmt"

	"SentinelOps/utility/client"

	goredis "github.com/redis/go-redis/v9"
)

// GetClient 获取 Redis 客户端
func GetClient(ctx context.Context) (*goredis.Client, error) {
	rdb, err := client.GetRedisClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("get redis client: %w", err)
	}
	return rdb, nil
}
