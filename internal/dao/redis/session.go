package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	goredis "github.com/redis/go-redis/v9"
)

var (
	sessionKeyPrefix    string
	sessionTTL          time.Duration
	sessionConfigLoaded bool
)

func loadSessionConfig(ctx context.Context) {
	if sessionConfigLoaded {
		return
	}
	keyPrefixVal, _ := g.Cfg().Get(ctx, "redis.chat_cache.key_prefix")
	sessionKeyPrefix = keyPrefixVal.String()
	if sessionKeyPrefix == "" {
		sessionKeyPrefix = "session"
	}
	ttlVal, _ := g.Cfg().Get(ctx, "redis.chat_cache.ttl")
	sessionTTL = time.Duration(ttlVal.Int()) * time.Hour
	if sessionTTL == 0 {
		sessionTTL = 720 * time.Hour
	}
	sessionConfigLoaded = true
}

func buildRecentKey(sessionID string) string {
	return fmt.Sprintf("%s:%s:recent", sessionKeyPrefix, sessionID)
}

func buildSummaryKey(sessionID string) string {
	return fmt.Sprintf("%s:%s:summary", sessionKeyPrefix, sessionID)
}

// LoadSession 从 Redis 加载会话记忆
func LoadSession(ctx context.Context, sessionID string) ([]*schema.Message, string, error) {
	loadSessionConfig(ctx)
	rdb, err := GetClient(ctx)
	if err != nil {
		return nil, "", err
	}

	var recent []*schema.Message
	recentKey := buildRecentKey(sessionID)
	data, err := rdb.Get(ctx, recentKey).Bytes()
	if errors.Is(err, goredis.Nil) {
		recent = []*schema.Message{}
	} else if err != nil {
		return nil, "", fmt.Errorf("redis GET %s: %w", recentKey, err)
	} else {
		if err := json.Unmarshal(data, &recent); err != nil {
			return nil, "", fmt.Errorf("unmarshal recent: %w", err)
		}
	}

	var summary string
	summaryKey := buildSummaryKey(sessionID)
	summaryVal, err := rdb.Get(ctx, summaryKey).Result()
	if errors.Is(err, goredis.Nil) {
		summary = ""
	} else if err != nil {
		return nil, "", fmt.Errorf("redis GET %s: %w", summaryKey, err)
	} else {
		summary = summaryVal
	}

	return recent, summary, nil
}

// SaveSession 保存会话记忆到 Redis
func SaveSession(ctx context.Context, sessionID string, recent []*schema.Message, summary string) error {
	loadSessionConfig(ctx)
	rdb, err := GetClient(ctx)
	if err != nil {
		return err
	}

	recentData, err := json.Marshal(recent)
	if err != nil {
		return fmt.Errorf("marshal recent: %w", err)
	}

	recentKey := buildRecentKey(sessionID)
	if err := rdb.Set(ctx, recentKey, recentData, sessionTTL).Err(); err != nil {
		return fmt.Errorf("redis SET %s: %w", recentKey, err)
	}

	summaryKey := buildSummaryKey(sessionID)
	if err := rdb.Set(ctx, summaryKey, summary, sessionTTL).Err(); err != nil {
		return fmt.Errorf("redis SET %s: %w", summaryKey, err)
	}

	return nil
}
