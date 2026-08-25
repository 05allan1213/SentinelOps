package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	goruntime "runtime"
	"runtime/debug"
	"sort"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/prompt/agents"
	appconfig "SentinelOps/internal/config"
)

const (
	einoVersion   = "v0.9.15"
	agentRevision = "sentinelops-agent-contract-v2"
)

// BuildDurableRuntimeSnapshot 从非敏感配置与完整的 Plan/专业 Agent Catalog
// 构造 API/Worker 共用的精确快照。L1/L2 静态上限在 P42 前保持关闭。
func BuildDurableRuntimeSnapshot(config *appconfig.Config) (FrozenRuntimeSnapshot, error) {
	if config == nil {
		return FrozenRuntimeSnapshot{}, fmt.Errorf("application configuration is required")
	}
	models := make([]ModelSnapshot, 0, 6)
	for _, route := range []struct{ kind, profile string }{{"chat", "default"}, {"chat", "reasoning"}, {"embedding", "default"}, {"rerank", "default"}} {
		_, chatRouteExists := config.Routing.Chat[route.profile]
		if (route.kind == "chat" && !chatRouteExists) ||
			(route.kind == "embedding" && len(config.Routing.Embedding) == 0) ||
			(route.kind == "rerank" && len(config.Routing.Rerank) == 0) {
			continue
		}
		resolved, err := ModelSnapshotsFromRoute(config, route.kind, route.profile)
		if err != nil {
			return FrozenRuntimeSnapshot{}, err
		}
		models = append(models, resolved...)
	}

	inventory := append(policy.RequiredDurableToolNames(), policy.DurableFrameworkToolNames()...)
	sort.Strings(inventory)
	tools := make([]ToolSnapshot, 0, len(inventory))
	policyEntries := make([]policy.CatalogEntry, 0, len(inventory))
	for _, name := range inventory {
		entry, err := policy.LookupCatalog(name)
		if err != nil {
			return FrozenRuntimeSnapshot{}, err
		}
		tools = append(tools, ToolSnapshot{Name: name, Revision: entry.Revision, SchemaHash: entry.SchemaHash})
		policyEntries = append(policyEntries, entry)
	}
	sort.Slice(policyEntries, func(i, j int) bool { return policyEntries[i].Name < policyEntries[j].Name })

	configIdentity := struct {
		Models       []ModelSnapshot        `json:"models"`
		AgentRuntime appconfig.AgentRuntime `json:"agent_runtime"`
	}{Models: models, AgentRuntime: config.AgentRuntime}
	return FreezeRuntimeSnapshot(RuntimeSnapshotInput{
		Runtime:       RuntimeVersionSnapshot{Go: goruntime.Version(), Eino: einoVersion, App: buildRevision()},
		AgentRevision: agentRevision,
		PromptHash: hashSnapshotValue(map[string]string{
			"event_analysis": agents.EventAnalysis, "risk": agents.Risk, "solve": agents.Solve,
			"report": agents.Report, "intelligence": agents.Intelligence, "ops": agents.Ops,
			"planner": agents.Planner,
		}),
		PolicyHash: hashSnapshotValue(policyEntries),
		ConfigHash: hashSnapshotValue(configIdentity),
		Models:     models,
		Tools:      tools,
		MCPCatalogHash: hashSnapshotValue(struct {
			Enabled bool `json:"enabled"`
		}{Enabled: false}),
		Skills: []SkillSnapshot{},
		FeatureGates: map[string]bool{
			"agent_runtime.enabled":                    config.AgentRuntime.Enabled,
			"agent_runtime.accept_new_runs":            config.AgentRuntime.AcceptNewRuns,
			"agent_runtime.shadow_mode":                config.AgentRuntime.ShadowMode,
			"agent_runtime.l1_writes":                  false,
			"agent_runtime.l2_writes":                  false,
			"agent_runtime.admin_query_database_debug": config.AgentRuntime.AdminQueryDatabaseDebug,
			"mcp.enabled":                              false,
			"skill.enabled":                            false,
			"langfuse.enabled":                         false,
		},
	})
}

func hashSnapshotValue(value any) string {
	canonical, err := policy.CanonicalJSON(value)
	if err != nil {
		panic(fmt.Sprintf("canonical snapshot identity: %v", err))
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return "development"
}
