package bootstrap

import (
	"context"

	"SentinelOps/internal/ai/rule"
	aitrace "SentinelOps/internal/ai/trace"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/knowledge"

	einocallbacks "github.com/cloudwego/eino/callbacks"
)

func initializeSharedRuntime(ctx context.Context) error {
	dao.SeedSettings(ctx)
	dao.SeedTermMappings(ctx)
	knowledge.EnsureDefaultBase(ctx)
	if err := dao.RegisterPlugin(aitrace.NewGORMPlugin()); err != nil {
		return err
	}
	rule.InitTermMappings(ctx)
	einocallbacks.AppendGlobalHandlers(aitrace.NewCallbackHandler())
	return nil
}
