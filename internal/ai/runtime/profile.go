package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	goruntime "runtime"
	"runtime/debug"
	"sort"
	"strings"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/prompt/agents"
	mcptools "SentinelOps/internal/ai/tools/mcp"
	appconfig "SentinelOps/internal/config"
)

const (
	einoVersion   = "v0.9.15"
	agentRevision = "sentinelops-agent-contract-v2"
	// RuntimeVersionEnv 为 release overlay 注入与镜像 digest 对齐的版本身份。
	RuntimeVersionEnv       = "SENTINELOPS_RUNTIME_VERSION"
	developmentBuildVersion = "development"
)

// BuildDurableRuntimeSnapshot 从非敏感配置与完整的 Plan/专业 Agent Catalog 构造精确快照。
func BuildDurableRuntimeSnapshot(config *appconfig.Config) (FrozenRuntimeSnapshot, error) {
	return BuildDurableRuntimeSnapshotWithSkills(config, nil)
}

// BuildDurableRuntimeSnapshotWithSkills 在同一 Runtime Snapshot 中冻结只读 Skill 内容身份。
func BuildDurableRuntimeSnapshotWithSkills(config *appconfig.Config, skills []SkillSnapshot) (FrozenRuntimeSnapshot, error) {
	return BuildDurableRuntimeSnapshotWithSkillsAndGates(config, skills, StaticGateCaps(config))
}

// BuildDurableRuntimeSnapshotWithSkillsAndGates 冻结调用时已经求值的完整 Gate 向量。
func BuildDurableRuntimeSnapshotWithSkillsAndGates(config *appconfig.Config, skills []SkillSnapshot, gates GateVector) (FrozenRuntimeSnapshot, error) {
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
		Models []ModelSnapshot `json:"models"`
	}{Models: models}
	mcpConfig, err := mcptools.FromAppConfig(config)
	if err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	// MCP 的启用位已经独立冻结在 FeatureGates；重建恢复身份时必须使用
	// Run 自身的 frozen 值，不能让当前静态 cap 改写历史 compatibility hash。
	mcpConfig.Enabled = gates.MCPEnabled
	mcpCatalogHash, err := mcptools.ConfigCatalogHash(mcpConfig)
	if err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	return FreezeRuntimeSnapshot(RuntimeSnapshotInput{
		Runtime:       CurrentRuntimeVersionSnapshot(),
		AgentRevision: agentRevision,
		PromptHash: hashSnapshotValue(map[string]string{
			"event_analysis": agents.EventAnalysis, "risk": agents.Risk, "solve": agents.Solve,
			"report": agents.Report, "intelligence": agents.Intelligence, "ops": agents.Ops,
			"planner": agents.Planner,
		}),
		PolicyHash:     hashSnapshotValue(policyEntries),
		ConfigHash:     hashSnapshotValue(configIdentity),
		Models:         models,
		Tools:          tools,
		MCPCatalogHash: mcpCatalogHash,
		Skills:         append([]SkillSnapshot(nil), skills...),
		FeatureGates:   gates.Map(),
	})
}

// CurrentRuntimeVersionSnapshot 返回 Worker claim 与 Run snapshot 共用的精确版本身份。
func CurrentRuntimeVersionSnapshot() RuntimeVersionSnapshot {
	revision := strings.TrimSpace(os.Getenv(RuntimeVersionEnv))
	if revision == "" {
		revision = buildRevision()
	}
	return RuntimeVersionSnapshot{Go: goruntime.Version(), Eino: einoVersion, App: revision}
}

// CurrentRuntimeVersion 返回 workflow_runs.runtime_version 使用的 canonical JSON。
func CurrentRuntimeVersion() string {
	value, _ := policy.CanonicalJSON(CurrentRuntimeVersionSnapshot())
	return string(value)
}

// ValidateCurrentRuntimeVersion 拒绝 release Worker 共享 development 或可变版本身份。
func ValidateCurrentRuntimeVersion() error {
	revision := CurrentRuntimeVersionSnapshot().App
	raw := revision
	if strings.HasPrefix(raw, "sha256:") {
		raw = strings.TrimPrefix(raw, "sha256:")
	}
	if revision == developmentBuildVersion || len(raw) != 40 && len(raw) != 64 || raw != strings.ToLower(raw) {
		return fmt.Errorf("runtime version must be an immutable Git SHA or sha256 digest")
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != len(raw)/2 {
		return fmt.Errorf("runtime version must be an immutable Git SHA or sha256 digest")
	}
	return nil
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
		revision := ""
		modified := false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
		if revision != "" && !modified {
			return revision
		}
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return developmentBuildVersion
}
