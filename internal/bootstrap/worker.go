package bootstrap

import (
	"context"

	"SentinelOps/internal/service/knowledge"
	"SentinelOps/internal/service/scheduler"
)

func startWorker(ctx context.Context) error {
	scheduler.RunOpsCompensationScan(ctx)
	knowledge.StartWorkerPool(ctx)
	scheduler.Run(ctx)
	return nil
}

func waitWorker(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
