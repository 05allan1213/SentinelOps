package redis

import (
	"context"
	"fmt"

	"github.com/gogf/gf/v2/frame/g"
)

// GetSessionSnapshot 获取会话对话快照
func GetSessionSnapshot(ctx context.Context, sessionID string) (string, error) {
	keyPrefixVal, _ := g.Cfg().Get(ctx, "redis.chat_cache.key_prefix")
	prefix := keyPrefixVal.String()
	if prefix == "" {
		prefix = "session"
	}
	recentKey := fmt.Sprintf("%s:%s:recent", prefix, sessionID)

	rdb, err := GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("get redis client: %w", err)
	}

	data, err := rdb.Get(ctx, recentKey).Bytes()
	if err != nil {
		return "[]", nil // 返回空数组而非错误
	}

	return string(data), nil
}
