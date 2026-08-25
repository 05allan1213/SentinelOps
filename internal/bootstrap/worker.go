package bootstrap

import (
	"context"
	"fmt"
	"os"
	"time"

	"SentinelOps/internal/ai/agent/event_analysis_pipeline"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/knowledge"
	"SentinelOps/internal/service/scheduler"

	"github.com/cloudwego/eino/adk"
	"github.com/gogf/gf/v2/frame/g"
)

func startWorker(ctx context.Context) error {
	config, err := appconfig.Current()
	if err != nil {
		return err
	}
	var durableWorker *airuntime.Worker
	if config.AgentRuntime.Enabled {
		durableWorker, err = newDurableWorker(ctx, config)
		if err != nil {
			return err
		}
	}
	scheduler.RunOpsCompensationScan(ctx)
	knowledge.StartWorkerPool(ctx)
	scheduler.Run(ctx)
	if durableWorker != nil {
		go func() {
			if runErr := durableWorker.Run(ctx); runErr != nil && ctx.Err() == nil {
				g.Log().Errorf(ctx, "durable Worker stopped: %v", runErr)
			}
		}()
	}
	return nil
}

func newDurableWorker(ctx context.Context, config *appconfig.Config) (*airuntime.Worker, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}
	store := workflow.NewGORMStore(db)
	snapshot, err := airuntime.BuildP20L0Snapshot(config)
	if err != nil {
		return nil, err
	}
	executor, err := airuntime.NewDurableExecutor(store, func(agentCtx context.Context, name string) (adk.Agent, error) {
		if name != "event_analysis_agent" {
			return nil, fmt.Errorf("durable Agent %q is not enabled in P20", name)
		}
		return event_analysis_pipeline.GetDurableEventAnalysisAgent(agentCtx)
	}, snapshot.CompatibilityHash())
	if err != nil {
		return nil, err
	}
	owner, err := os.Hostname()
	if err != nil || owner == "" {
		owner = "sentinelops-worker"
	}
	return airuntime.NewWorker(store, airuntime.WorkerConfig{
		Owner: owner, LeaseDuration: 30 * time.Second,
		MinPollBackoff: 100 * time.Millisecond, MaxPollBackoff: 2 * time.Second,
		Execute: executor.ExecuteClaimedRun,
	})
}

func waitWorker(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
