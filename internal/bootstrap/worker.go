package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"SentinelOps/internal/ai/agent/plan_pipeline"
	"SentinelOps/internal/ai/agent/skill_pipeline"
	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/indexer"
	"SentinelOps/internal/ai/ops/actions"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	mcptools "SentinelOps/internal/ai/tools/mcp"
	aitrace "SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/knowledge"
	"SentinelOps/internal/service/scheduler"
	settingssvc "SentinelOps/internal/service/settings"

	"github.com/cloudwego/eino/adk"
	"github.com/gogf/gf/v2/frame/g"
)

const workerIDEnv = "SENTINELOPS_WORKER_ID"

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
	evaluator, err := airuntime.NewGateEvaluator(airuntime.StaticGateCaps(config), dao.GetSettings)
	if err != nil {
		return nil, err
	}
	owner, err := durableWorkerOwner()
	if err != nil {
		return nil, err
	}
	observation, err := configuredWorkerObservation(ctx, config, evaluator, owner)
	if err != nil {
		return nil, err
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
			Gates:     evaluator, SnapshotDB: db, Observation: observation,
		})
	}
	handler, err := airuntime.NewHITLRuntimeHandler(store, evaluator)
	if err != nil {
		return nil, err
	}
	executor, err := airuntime.NewDurableExecutorWithReleaseControls(store, func(agentCtx context.Context, name string) (adk.Agent, error) {
		if name != "plan_agent" {
			return nil, fmt.Errorf("durable Agent %q is not enabled", name)
		}
		return plan_pipeline.NewDurablePlanAgent(agentCtx, handler)
	}, evaluator, func(attemptCtx context.Context) (*aitrace.LangfuseRuntime, error) {
		return newAttemptLangfuseRuntime(attemptCtx, config)
	}, func(compatibilityCtx context.Context, frozen airuntime.FrozenRuntimeSnapshot) (string, error) {
		return expectedFrozenGateCompatibilityWithEvaluator(compatibilityCtx, config, evaluator, frozen)
	})
	if err != nil {
		return nil, err
	}
	return airuntime.NewWorker(store, airuntime.WorkerConfig{
		Owner: owner, LeaseDuration: 30 * time.Second,
		MinPollBackoff: 100 * time.Millisecond, MaxPollBackoff: 2 * time.Second,
		Execute: executor.ExecuteClaimedRun, QueryEffectTargetState: queryEffectTargetState,
		Retention: retention, RuntimeVersion: airuntime.CurrentRuntimeVersion(), Gates: evaluator,
		SnapshotDB: db, Observation: observation,
	})
}

func configuredWorkerObservation(ctx context.Context, config *appconfig.Config, evaluator *airuntime.GateEvaluator, owner string) (airuntime.WorkerObservation, error) {
	if config == nil || evaluator == nil {
		return airuntime.WorkerObservation{}, fmt.Errorf("Worker snapshot configuration and Gate evaluator are required")
	}
	gates, err := evaluator.Current(ctx)
	if err != nil {
		return airuntime.WorkerObservation{}, err
	}
	var skills []airuntime.SkillSnapshot
	if gates.Enabled(airuntime.GateSkillEnabled) {
		skills, err = skill_pipeline.BuildConfiguredSkillSnapshots(ctx, config)
		if err != nil {
			return airuntime.WorkerObservation{}, err
		}
	}
	frozen, err := airuntime.BuildDurableRuntimeSnapshotWithSkillsAndGates(config, skills, gates)
	if err != nil {
		return airuntime.WorkerObservation{}, err
	}
	mcpConfig, err := mcptools.FromAppConfig(config)
	if err != nil {
		return airuntime.WorkerObservation{}, err
	}
	observedMCP := make([]airuntime.ObservedRuntimeComponent, 0, len(mcpConfig.Servers))
	for _, server := range mcpConfig.Servers {
		status := "disabled"
		if mcpConfig.Enabled && server.Enabled && gates.Enabled(airuntime.GateMCPEnabled) {
			status = "not_observed"
		}
		observedMCP = append(observedMCP, airuntime.ObservedRuntimeComponent{Name: server.Name, Validation: "not_run", Status: status})
	}
	observedSkills := make([]airuntime.ObservedRuntimeComponent, 0, len(skills))
	for _, skill := range skills {
		observedSkills = append(observedSkills, airuntime.ObservedRuntimeComponent{
			Name: skill.Name, Hash: skill.ContentHash, Validation: "valid", Status: "loaded",
		})
	}
	sort.Slice(observedSkills, func(i, j int) bool { return observedSkills[i].Name < observedSkills[j].Name })
	fields := frozen.WorkflowFields()
	return airuntime.WorkerObservation{
		WorkerID: owner, RuntimeVersion: frozen.RuntimeVersion(),
		RuntimeCompatibilityHash: frozen.CompatibilityHash(), ConfiguredCatalogHash: fields.MCPCatalogHash,
		ObservedMCP: observedMCP, ObservedSkill: observedSkills, Status: airuntime.WorkerStatusIdle,
	}, nil
}

// durableWorkerOwner 允许同一主机上的独立 Worker 使用可审计的 lease owner。
func durableWorkerOwner() (string, error) {
	if configured, ok := os.LookupEnv(workerIDEnv); ok {
		if configured != strings.TrimSpace(configured) || configured == "" || len(configured) > 128 {
			return "", fmt.Errorf("%s must contain 1 to 128 unpadded bytes", workerIDEnv)
		}
		return configured, nil
	}
	owner, err := os.Hostname()
	if err != nil || strings.TrimSpace(owner) == "" {
		owner = "sentinelops-worker"
	}
	if owner != strings.TrimSpace(owner) || len(owner) > 128 {
		return "", fmt.Errorf("worker hostname must contain 1 to 128 unpadded bytes")
	}
	return owner, nil
}

func expectedFrozenGateCompatibilityWithEvaluator(
	ctx context.Context,
	config *appconfig.Config,
	evaluator *airuntime.GateEvaluator,
	frozen airuntime.FrozenRuntimeSnapshot,
) (string, error) {
	if config == nil {
		return "", fmt.Errorf("application configuration is required")
	}
	frozenGates := frozen.Gates()
	skills := frozen.Skills()
	loadCurrentSkills := frozenGates.Enabled(airuntime.GateSkillEnabled)
	if evaluator != nil {
		effective, err := evaluator.Effective(ctx, frozen)
		if err != nil {
			return "", err
		}
		// 当前静态 cap 或动态开关关闭 Skill 时，不建立 Backend；保留 Run
		// 自身 frozen catalog，避免关闭 Gate 改写历史 compatibility hash。
		loadCurrentSkills = effective.Enabled(airuntime.GateSkillEnabled)
	}
	if loadCurrentSkills {
		currentSkills, err := skill_pipeline.BuildConfiguredSkillSnapshots(ctx, config)
		if err != nil {
			return "", err
		}
		skills = currentSkills
	}
	compatibilityConfig := *config
	expected, err := airuntime.BuildDurableRuntimeSnapshotWithSkillsAndGates(&compatibilityConfig, skills, frozenGates)
	if err != nil {
		return "", err
	}
	return expected.CompatibilityHash(), nil
}

func newAttemptLangfuseRuntime(ctx context.Context, config *appconfig.Config) (*aitrace.LangfuseRuntime, error) {
	langfuseConfig := config.Observability.Langfuse
	var runtime *aitrace.LangfuseRuntime
	err := appconfig.UseSecret(ctx, langfuseConfig.PublicKeyRef, func(publicKey []byte) error {
		return appconfig.UseSecret(ctx, langfuseConfig.SecretKeyRef, func(secretKey []byte) error {
			created, createErr := aitrace.NewLangfuseRuntime(ctx, aitrace.LangfuseOptions{
				StaticEnabled: true, DynamicEnabled: true,
				Host: langfuseConfig.Host, PublicKey: string(publicKey), SecretKey: string(secretKey),
				ServiceName: langfuseConfig.ServiceName, Environment: config.App.Environment,
				Version: airuntime.CurrentRuntimeVersion(), SampleRate: langfuseConfig.SampleRate,
				Timeout:                 time.Duration(langfuseConfig.TimeoutMS) * time.Millisecond,
				MaxAttributeValueLength: langfuseConfig.MaxAttributeValueLength,
				MaxSpanAttributeBytes:   langfuseConfig.MaxSpanAttributeBytes,
			})
			runtime = created
			return createErr
		})
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Attempt Langfuse: %w", err)
	}
	return runtime, nil
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
