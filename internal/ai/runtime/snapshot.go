package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"
)

const (
	runtimeSnapshotSchema = "sentinelops/runtime-snapshot/v1"
	runtimeSnapshotDomain = "sentinelops/runtime-compatibility/v1\x00"
	modelSnapshotDomain   = "sentinelops/model-snapshot/v1\x00"
)

var requiredFeatureGates = []string{
	"agent_runtime.enabled",
	"agent_runtime.accept_new_runs",
	"agent_runtime.shadow_mode",
	"agent_runtime.l1_writes",
	"agent_runtime.l2_writes",
	"agent_runtime.admin_query_database_debug",
	"mcp.enabled",
	"skill.enabled",
	"langfuse.enabled",
}

// RuntimeVersionSnapshot 将 Go、Eino 与应用 revision 固化为一个可持久化版本值。
type RuntimeVersionSnapshot struct {
	Go   string `json:"go"`
	Eino string `json:"eino"`
	App  string `json:"app"`
}

// RouteOptionsSnapshot 是影响模型请求语义的候选级 Route Options。
type RouteOptionsSnapshot struct {
	EnableThinking *bool  `json:"enable_thinking,omitempty"`
	Instruct       string `json:"instruct,omitempty"`
}

// PricingSnapshot 保存可复现成本计算所需的定价 revision 与费率。
type PricingSnapshot struct {
	Revision    string  `json:"revision"`
	Currency    string  `json:"currency"`
	Unit        string  `json:"unit"`
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cached_input,omitempty"`
	Output      float64 `json:"output,omitempty"`
}

// ModelSnapshot 是 provider-qualified 的模型候选身份。
type ModelSnapshot struct {
	Kind           string               `json:"kind"`
	Profile        string               `json:"profile"`
	CandidateOrder int                  `json:"candidate_order"`
	CatalogRef     string               `json:"catalog_ref"`
	Provider       string               `json:"provider"`
	Driver         string               `json:"driver"`
	ModelID        string               `json:"model_id"`
	RouteOptions   RouteOptionsSnapshot `json:"route_options"`
	Pricing        PricingSnapshot      `json:"pricing"`
}

// Identity 返回覆盖完整候选配置、不会因厂商 Model ID 碰撞而合并的模型快照身份。
func (m ModelSnapshot) Identity() string {
	canonical, _ := policy.CanonicalJSON(m)
	digest := sha256.New()
	_, _ = digest.Write([]byte(modelSnapshotDomain))
	_, _ = digest.Write(canonical)
	return hex.EncodeToString(digest.Sum(nil))
}

// ToolSnapshot 保存 Tool revision 与规范化 Schema hash。
type ToolSnapshot struct {
	Name       string `json:"name"`
	Revision   string `json:"revision"`
	SchemaHash string `json:"schema_hash"`
}

// SkillSnapshot 只保存 Skill 名称与内容 Hash，不保存可变 backend handle。
type SkillSnapshot struct {
	Name        string `json:"name"`
	ContentHash string `json:"content_hash"`
}

// RuntimeSnapshotInput 是 Run 创建时冻结的完整兼容输入。
type RuntimeSnapshotInput struct {
	Runtime        RuntimeVersionSnapshot `json:"runtime"`
	AgentRevision  string                 `json:"agent_revision"`
	PromptHash     string                 `json:"prompt_hash"`
	PolicyHash     string                 `json:"policy_hash"`
	ConfigHash     string                 `json:"config_hash"`
	Models         []ModelSnapshot        `json:"models"`
	Tools          []ToolSnapshot         `json:"tools"`
	MCPCatalogHash string                 `json:"mcp_catalog_hash"`
	Skills         []SkillSnapshot        `json:"skills"`
	FeatureGates   map[string]bool        `json:"feature_gates"`
}

type runtimeSnapshotDocument struct {
	Schema string `json:"schema"`
	RuntimeSnapshotInput
}

// FrozenRuntimeSnapshot 与调用方切片、map 和指针隔离，只暴露副本与稳定 Hash。
type FrozenRuntimeSnapshot struct {
	document  runtimeSnapshotDocument
	canonical []byte
	hash      string
}

// FreezeRuntimeSnapshot 规范化顺序、校验安全字段并计算 v1 精确兼容 Hash。
func FreezeRuntimeSnapshot(input RuntimeSnapshotInput) (FrozenRuntimeSnapshot, error) {
	document := runtimeSnapshotDocument{Schema: runtimeSnapshotSchema, RuntimeSnapshotInput: cloneRuntimeSnapshotInput(input)}
	normalizeRuntimeSnapshot(&document.RuntimeSnapshotInput)
	if err := validateRuntimeSnapshot(document.RuntimeSnapshotInput); err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	canonical, err := policy.CanonicalJSON(document)
	if err != nil {
		return FrozenRuntimeSnapshot{}, fmt.Errorf("canonicalize Runtime Snapshot: %w", err)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(runtimeSnapshotDomain))
	_, _ = digest.Write(canonical)
	return FrozenRuntimeSnapshot{
		document: document, canonical: append([]byte(nil), canonical...),
		hash: hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

// CanonicalJSON 返回不可修改内部状态的规范化 Snapshot 副本。
func (s FrozenRuntimeSnapshot) CanonicalJSON() []byte {
	return append([]byte(nil), s.canonical...)
}

// CompatibilityHash 返回 v1 runtime compatibility identity。
func (s FrozenRuntimeSnapshot) CompatibilityHash() string { return s.hash }

// Models 返回不可修改内部状态的 provider-qualified Model Snapshot 副本。
func (s FrozenRuntimeSnapshot) Models() []ModelSnapshot {
	return cloneModels(s.document.Models)
}

// Tools 返回不可修改内部状态的 Tool Snapshot 副本。
func (s FrozenRuntimeSnapshot) Tools() []ToolSnapshot {
	return append([]ToolSnapshot(nil), s.document.Tools...)
}

// PolicyHash 返回 Run 创建时冻结的 Policy identity。
func (s FrozenRuntimeSnapshot) PolicyHash() string { return s.document.PolicyHash }

// FeatureGate 返回 Run 创建时冻结的 Gate；未知键 fail-closed。
func (s FrozenRuntimeSnapshot) FeatureGate(name string) bool {
	return s.document.FeatureGates[name]
}

// WorkflowFields 将同一个 Frozen Snapshot 拆为 P03 已有列，不重复计算身份。
func (s FrozenRuntimeSnapshot) WorkflowFields() workflow.RuntimeSnapshotFields {
	runtimeVersion, _ := policy.CanonicalJSON(s.document.Runtime)
	models, _ := policy.CanonicalJSON(s.document.Models)
	tools, _ := policy.CanonicalJSON(s.document.Tools)
	skills, _ := policy.CanonicalJSON(s.document.Skills)
	features, _ := policy.CanonicalJSON(s.document.FeatureGates)
	return workflow.RuntimeSnapshotFields{
		RuntimeVersion:           string(runtimeVersion),
		RuntimeCompatibilityHash: s.hash,
		AgentRevision:            s.document.AgentRevision,
		ModelSnapshotJSON:        models,
		ToolSnapshotJSON:         tools,
		MCPCatalogHash:           s.document.MCPCatalogHash,
		SkillSnapshotJSON:        skills,
		PromptHash:               s.document.PromptHash,
		PolicyHash:               s.document.PolicyHash,
		ConfigHash:               s.document.ConfigHash,
		FeatureSnapshotJSON:      features,
	}
}

// RuntimeSnapshotFromRun 只从 MySQL Run 列重建 Snapshot，并验证存储 Hash 精确匹配。
func RuntimeSnapshotFromRun(run mysql.WorkflowRun) (FrozenRuntimeSnapshot, error) {
	required := map[string]*string{
		"runtime_version": run.RuntimeVersion, "runtime_compatibility_hash": run.RuntimeCompatibilityHash,
		"agent_revision": run.AgentRevision, "model_snapshot": run.ModelSnapshot,
		"tool_snapshot": run.ToolSnapshot, "mcp_catalog_hash": run.MCPCatalogHash,
		"skill_snapshot": run.SkillSnapshot, "prompt_hash": run.PromptHash,
		"policy_hash": run.PolicyHash, "config_hash": run.ConfigHash,
		"feature_snapshot": run.FeatureSnapshot,
	}
	for name, value := range required {
		if value == nil || strings.TrimSpace(*value) == "" {
			return FrozenRuntimeSnapshot{}, fmt.Errorf("workflow Run is missing %s", name)
		}
	}
	var input RuntimeSnapshotInput
	if err := decodeSnapshotField("runtime_version", *run.RuntimeVersion, &input.Runtime); err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	if err := decodeSnapshotField("model_snapshot", *run.ModelSnapshot, &input.Models); err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	if err := decodeSnapshotField("tool_snapshot", *run.ToolSnapshot, &input.Tools); err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	if err := decodeSnapshotField("skill_snapshot", *run.SkillSnapshot, &input.Skills); err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	if err := decodeSnapshotField("feature_snapshot", *run.FeatureSnapshot, &input.FeatureGates); err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	input.AgentRevision = *run.AgentRevision
	input.PromptHash = *run.PromptHash
	input.PolicyHash = *run.PolicyHash
	input.ConfigHash = *run.ConfigHash
	input.MCPCatalogHash = *run.MCPCatalogHash
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	if err := RequireExactCompatibility(*run.RuntimeCompatibilityHash, frozen.CompatibilityHash()); err != nil {
		return FrozenRuntimeSnapshot{}, err
	}
	return frozen, nil
}

// ModelSnapshotFromRoute 从唯一 Provider → Model Catalog → Routing 构造模型身份。
func ModelSnapshotFromRoute(cfg *appconfig.Config, kind, profile string) (ModelSnapshot, error) {
	models, err := ModelSnapshotsFromRoute(cfg, kind, profile)
	if err != nil {
		return ModelSnapshot{}, err
	}
	return models[0], nil
}

// ModelSnapshotsFromRoute 为 Chat Profile 保留全部有序候选身份。
func ModelSnapshotsFromRoute(cfg *appconfig.Config, kind, profile string) ([]ModelSnapshot, error) {
	if cfg == nil {
		return nil, fmt.Errorf("application configuration is required")
	}
	var routes []appconfig.Route
	switch kind {
	case "chat":
		chatRoute, ok := cfg.Routing.Chat[profile]
		if !ok || len(chatRoute.Candidates) == 0 {
			return nil, fmt.Errorf("routing.%s.%s is not configured", kind, profile)
		}
		routes = chatRoute.Candidates
	case "embedding":
		route, ok := cfg.Routing.Embedding[profile]
		if !ok {
			return nil, fmt.Errorf("routing.%s.%s is not configured", kind, profile)
		}
		routes = []appconfig.Route{route}
	case "rerank":
		route, ok := cfg.Routing.Rerank[profile]
		if !ok {
			return nil, fmt.Errorf("routing.%s.%s is not configured", kind, profile)
		}
		routes = []appconfig.Route{route}
	default:
		return nil, fmt.Errorf("unsupported model route kind %q", kind)
	}
	result := make([]ModelSnapshot, 0, len(routes))
	for order, route := range routes {
		_, catalogModel, err := cfg.Resolve(route)
		if err != nil {
			return nil, err
		}
		provider, _, qualified := strings.Cut(route.Model, "/")
		if !qualified || provider == "" {
			return nil, fmt.Errorf("model reference %q is not provider-qualified", route.Model)
		}
		result = append(result, ModelSnapshot{
			Kind: kind, Profile: profile, CandidateOrder: order, CatalogRef: route.Model, Provider: provider,
			Driver: catalogModel.Driver, ModelID: catalogModel.ModelID,
			RouteOptions: RouteOptionsSnapshot{EnableThinking: cloneBool(route.Options.EnableThinking), Instruct: route.Options.Instruct},
			Pricing: PricingSnapshot{
				Revision: catalogModel.Pricing.Revision, Currency: catalogModel.Pricing.Currency, Unit: catalogModel.Pricing.Unit,
				Input: catalogModel.Pricing.Input, CachedInput: catalogModel.Pricing.CachedInput, Output: catalogModel.Pricing.Output,
			},
		})
	}
	return result, nil
}

func decodeSnapshotField(name, value string, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func normalizeRuntimeSnapshot(input *RuntimeSnapshotInput) {
	sort.Slice(input.Models, func(i, j int) bool {
		left, right := input.Models[i], input.Models[j]
		return fmt.Sprintf("%s\x00%s\x00%09d\x00%s", left.Kind, left.Profile, left.CandidateOrder, left.CatalogRef) <
			fmt.Sprintf("%s\x00%s\x00%09d\x00%s", right.Kind, right.Profile, right.CandidateOrder, right.CatalogRef)
	})
	sort.Slice(input.Tools, func(i, j int) bool { return input.Tools[i].Name < input.Tools[j].Name })
	sort.Slice(input.Skills, func(i, j int) bool { return input.Skills[i].Name < input.Skills[j].Name })
}

func validateRuntimeSnapshot(input RuntimeSnapshotInput) error {
	for name, value := range map[string]string{
		"runtime.go": input.Runtime.Go, "runtime.eino": input.Runtime.Eino, "runtime.app": input.Runtime.App,
		"agent_revision": input.AgentRevision,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%s is required and must not contain NUL", name)
		}
	}
	for name, value := range map[string]string{
		"prompt_hash": input.PromptHash, "policy_hash": input.PolicyHash,
		"config_hash": input.ConfigHash, "mcp_catalog_hash": input.MCPCatalogHash,
	} {
		if err := validateSnapshotHash(name, value); err != nil {
			return err
		}
	}
	for index, model := range input.Models {
		provider, _, ok := strings.Cut(model.CatalogRef, "/")
		if !ok || provider != model.Provider || model.Kind == "" || model.Profile == "" || model.Driver == "" || model.ModelID == "" {
			return fmt.Errorf("model snapshot %d has invalid provider-qualified identity", index)
		}
		if strings.TrimSpace(model.Pricing.Revision) == "" || model.Pricing.Currency == "" || model.Pricing.Unit == "" {
			return fmt.Errorf("model snapshot %d has incomplete pricing identity", index)
		}
	}
	for index, tool := range input.Tools {
		if tool.Name == "" || tool.Revision == "" {
			return fmt.Errorf("tool snapshot %d has incomplete identity", index)
		}
		if err := validateSnapshotHash("tool schema hash", tool.SchemaHash); err != nil {
			return err
		}
	}
	for index, skill := range input.Skills {
		if skill.Name == "" {
			return fmt.Errorf("skill snapshot %d name is required", index)
		}
		if err := validateSnapshotHash("skill content hash", skill.ContentHash); err != nil {
			return err
		}
	}
	if len(input.FeatureGates) != len(requiredFeatureGates) {
		return fmt.Errorf("feature snapshot must contain exactly %d gates", len(requiredFeatureGates))
	}
	for _, gate := range requiredFeatureGates {
		if _, ok := input.FeatureGates[gate]; !ok {
			return fmt.Errorf("feature snapshot is missing %s", gate)
		}
	}
	return nil
}

func validateSnapshotHash(name, value string) error {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return fmt.Errorf("%s must be a lowercase SHA-256 digest", name)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("%s must be a lowercase SHA-256 digest", name)
	}
	return nil
}

func cloneRuntimeSnapshotInput(input RuntimeSnapshotInput) RuntimeSnapshotInput {
	cloned := input
	cloned.Models = append([]ModelSnapshot(nil), input.Models...)
	for index := range cloned.Models {
		cloned.Models[index].RouteOptions.EnableThinking = cloneBool(input.Models[index].RouteOptions.EnableThinking)
	}
	cloned.Tools = append([]ToolSnapshot(nil), input.Tools...)
	cloned.Skills = append([]SkillSnapshot(nil), input.Skills...)
	cloned.FeatureGates = make(map[string]bool, len(input.FeatureGates))
	for key, value := range input.FeatureGates {
		cloned.FeatureGates[key] = value
	}
	return cloned
}

func cloneModels(models []ModelSnapshot) []ModelSnapshot {
	result := make([]ModelSnapshot, len(models))
	copy(result, models)
	for index := range result {
		result[index].RouteOptions.EnableThinking = cloneBool(result[index].RouteOptions.EnableThinking)
	}
	return result
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
