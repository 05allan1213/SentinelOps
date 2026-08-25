package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"SentinelOps/internal/ai/agent/plan_pipeline"
	"SentinelOps/internal/ai/agent/skill_pipeline"
	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/indexer"
	"SentinelOps/internal/ai/ops/actions"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/knowledge"
	"SentinelOps/internal/service/scheduler"
	settingssvc "SentinelOps/internal/service/settings"

	"github.com/cloudwego/eino/adk"
	"github.com/gogf/gf/v2/frame/g"
)

func startWorker(ctx context.Context) error {
	config, err := appconfig.Current()
	if err != nil {
		return err
	}
	var durableWorker *airuntime.Worker
	if config.AgentRuntime.Enabled || config.Observability.Retention.Enabled {
		durableWorker, err = newDurableWorker(ctx, config)
		if err != nil {
			return err
		}
	}
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
	owner, err := os.Hostname()
	if err != nil || owner == "" {
		owner = "sentinelops-worker"
	}
	var retention *airuntime.RetentionCoordinator
	if config.Observability.Retention.Enabled {
		if err := store.EnsureRetentionLeaseRun(ctx); err != nil {
			return nil, err
		}
		retentionConfig := config.Observability.Retention
		retention, err = airuntime.NewRetentionCoordinator(store, dao.NewTraceDAO(), airuntime.RetentionConfig{
			Owner: owner, LeaseDuration: time.Duration(retentionConfig.LeaseDurationMS) * time.Millisecond,
			Interval: time.Duration(retentionConfig.IntervalMS) * time.Millisecond, BatchSize: retentionConfig.BatchSize,
			LoadPolicy: func(loadCtx context.Context) (airuntime.RetentionPolicy, error) {
				settings, loadErr := settingssvc.GetRetention(loadCtx)
				return airuntime.RetentionPolicy{PayloadDays: settings.PayloadDays, AuditDays: settings.AuditDays}, loadErr
			},
		})
		if err != nil {
			return nil, err
		}
	}
	if !config.AgentRuntime.Enabled {
		return airuntime.NewWorker(store, airuntime.WorkerConfig{
			Owner: owner, LeaseDuration: 30 * time.Second,
			MinPollBackoff: 100 * time.Millisecond, MaxPollBackoff: 2 * time.Second,
			Retention: retention,
		})
	}
	skillSnapshots, err := skill_pipeline.BuildConfiguredSkillSnapshots(ctx, config)
	if err != nil {
		return nil, err
	}
	snapshot, err := airuntime.BuildDurableRuntimeSnapshotWithSkills(config, skillSnapshots)
	if err != nil {
		return nil, err
	}
	handler, err := airuntime.NewHITLRuntimeHandler(store)
	if err != nil {
		return nil, err
	}
	executor, err := airuntime.NewDurableExecutor(store, func(agentCtx context.Context, name string) (adk.Agent, error) {
		if name != "plan_agent" {
			return nil, fmt.Errorf("durable Agent %q is not enabled", name)
		}
		return plan_pipeline.NewDurablePlanAgent(agentCtx, handler)
	}, snapshot.CompatibilityHash())
	if err != nil {
		return nil, err
	}
	return airuntime.NewWorker(store, airuntime.WorkerConfig{
		Owner: owner, LeaseDuration: 30 * time.Second,
		MinPollBackoff: 100 * time.Millisecond, MaxPollBackoff: 2 * time.Second,
		Execute: executor.ExecuteClaimedRun, QueryEffectTargetState: queryEffectTargetState,
		Retention: retention,
	})
}

func queryEffectTargetState(ctx context.Context, target effects.ReconciliationTarget) (effects.TargetState, error) {
	if target.EffectStep == "milvus_index" {
		var primary struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(target.PrimaryResponse), &primary); err != nil || primary.ID == "" {
			return effects.TargetState{}, fmt.Errorf("milvus reconciliation primary response is invalid")
		}
		applied, evidence, err := indexer.QueryMilvusTargetState(ctx, primary.ID)
		state := effects.TargetState{Known: err == nil, Applied: applied, Evidence: evidence}
		if applied {
			response, marshalErr := json.Marshal(map[string]string{"id": primary.ID, "indexed": "true"})
			if marshalErr != nil {
				return effects.TargetState{}, marshalErr
			}
			state.Response = string(response)
		}
		return state, err
	}
	if target.EffectType == policy.EffectProviderIdempotent {
		executor, ok := actions.Get(target.ToolName)
		if !ok {
			return effects.TargetState{}, fmt.Errorf("action %q is not registered", target.ToolName)
		}
		result, err := executor.Execute(ctx, target.Parameters)
		if err != nil {
			return effects.TargetState{Evidence: map[string]any{"same_key_retry": "unknown"}}, err
		}
		response, err := json.Marshal(result.Output)
		if err != nil {
			return effects.TargetState{}, err
		}
		return effects.TargetState{
			Known: true, Applied: result.Success, Response: string(response),
			ExternalReference: result.Output["external_reference"], Evidence: result.Output,
		}, nil
	}
	state, err := actions.QueryTargetState(ctx, target.ToolName, target.Parameters)
	if err != nil {
		return effects.TargetState{Known: state.Known, Applied: state.Applied, Evidence: state.Evidence}, err
	}
	response, err := json.Marshal(state.Evidence)
	if err != nil {
		return effects.TargetState{}, err
	}
	return effects.TargetState{Known: state.Known, Applied: state.Applied, Response: string(response), Evidence: state.Evidence}, nil
}

func waitWorker(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
