package bootstrap

import (
	"context"

	"SentinelOps/internal/ai/ops/engine"
	toolsops "SentinelOps/internal/ai/tools/ops"
	"SentinelOps/internal/service/knowledge"
	"SentinelOps/internal/service/scheduler"
)

func startWorker(ctx context.Context) error {
	toolsops.SetTriggerFunc(engine.TriggerForEvent)
	scheduler.RunOpsCompensationScan(ctx)
	knowledge.StartWorkerPool(ctx)
	scheduler.Run(ctx)
	return nil
}

func waitWorker(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
