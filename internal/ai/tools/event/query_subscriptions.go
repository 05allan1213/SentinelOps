package event

import (
	dao "SentinelOps/internal/dao/mysql"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
)

// QuerySubscriptionsInput 查询订阅参数
type QuerySubscriptionsInput struct {
	Enabled *bool `json:"enabled" jsonschema:"description=是否只查启用的订阅，空则查全部"`
}

// NewQuerySubscriptionsTool 创建 query_subscriptions 工具，从 MySQL 查询订阅源
func NewQuerySubscriptionsTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"query_subscriptions",
		"Query subscription sources from the dao. Use when user asks about subscriptions, data sources, or RSS feeds.",
		func(ctx context.Context, input *QuerySubscriptionsInput, opts ...tool.Option) (output string, err error) {
			db, err := dao.DB(ctx)
			if err != nil {
				return "", fmt.Errorf("database not available: %w", err)
			}

			enabledFilter := "all"
			if input != nil && input.Enabled != nil {
				if *input.Enabled {
					enabledFilter = "true"
				} else {
					enabledFilter = "false"
				}
			}
			g.Log().Infof(ctx, "[Tool] query_subscriptions 开始 | enabled=%s", enabledFilter)

			var subs []dao.Subscription
			q := db.Order("created_at DESC")
			if input != nil && input.Enabled != nil {
				q = q.Where("enabled = ?", *input.Enabled)
			}
			if err = q.Find(&subs).Error; err != nil {
				return "", fmt.Errorf("query subscriptions: %w", err)
			}

			g.Log().Infof(ctx, "[Tool] query_subscriptions 完成 | 返回=%d 条", len(subs))
			b, _ := json.Marshal(subs)
			return string(b), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}
