package runtime

import (
	"context"
	"fmt"

	appconfig "SentinelOps/internal/config"
)

const (
	// GateAgentRuntimeEnabled 控制 durable Runtime 是否允许 Worker 认领。
	GateAgentRuntimeEnabled = "agent_runtime.enabled"
	// GateAgentRuntimeAcceptNewRuns 控制 API 是否允许创建新 Run。
	GateAgentRuntimeAcceptNewRuns = "agent_runtime.accept_new_runs"
	// GateAgentRuntimeShadowMode 强制当前或 frozen Run 保持零 Mutation。
	GateAgentRuntimeShadowMode = "agent_runtime.shadow_mode"
	// GateAgentRuntimeL1Writes 控制 L1 Approval/Effect 写入。
	GateAgentRuntimeL1Writes = "agent_runtime.l1_writes"
	// GateAgentRuntimeL2Writes 控制 L2 Approval/Effect 写入。
	GateAgentRuntimeL2Writes = "agent_runtime.l2_writes"
	// GateAgentRuntimeAdminQueryDatabaseDebug 控制 admin 只读数据库调试工具。
	GateAgentRuntimeAdminQueryDatabaseDebug = "agent_runtime.admin_query_database_debug"
	// GateMCPEnabled 控制 MCP Session 与远端 Tool 调用。
	GateMCPEnabled = "mcp.enabled"
	// GateSkillEnabled 控制 Skill Backend 与 SOP 读取。
	GateSkillEnabled = "skill.enabled"
	// GateLangfuseEnabled 控制新 Attempt 的 Langfuse 导出。
	GateLangfuseEnabled = "langfuse.enabled"
)

var canonicalGateKeys = [...]string{
	GateAgentRuntimeEnabled,
	GateAgentRuntimeAcceptNewRuns,
	GateAgentRuntimeShadowMode,
	GateAgentRuntimeL1Writes,
	GateAgentRuntimeL2Writes,
	GateAgentRuntimeAdminQueryDatabaseDebug,
	GateMCPEnabled,
	GateSkillEnabled,
	GateLangfuseEnabled,
}

var canonicalGateKeySet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(canonicalGateKeys))
	for _, key := range canonicalGateKeys {
		result[key] = struct{}{}
	}
	return result
}()

// GateVector 是九项发布 Gate 的不可变值对象。
type GateVector struct {
	AgentRuntimeEnabled                 bool `json:"agent_runtime.enabled"`
	AgentRuntimeAcceptNewRuns           bool `json:"agent_runtime.accept_new_runs"`
	AgentRuntimeShadowMode              bool `json:"agent_runtime.shadow_mode"`
	AgentRuntimeL1Writes                bool `json:"agent_runtime.l1_writes"`
	AgentRuntimeL2Writes                bool `json:"agent_runtime.l2_writes"`
	AgentRuntimeAdminQueryDatabaseDebug bool `json:"agent_runtime.admin_query_database_debug"`
	MCPEnabled                          bool `json:"mcp.enabled"`
	SkillEnabled                        bool `json:"skill.enabled"`
	LangfuseEnabled                     bool `json:"langfuse.enabled"`
}

// DynamicGateReader 只读取调用方指定的 canonical settings key。
type DynamicGateReader func(context.Context, []string) (map[string]string, error)

// GateEvaluator 组合进程静态上限、当前动态开关与 Run frozen snapshot。
type GateEvaluator struct {
	static GateVector
	read   DynamicGateReader
}

// LegacyCompatibilityGate 统一控制仍保留的旧 Event/Ops 入口。
type LegacyCompatibilityGate interface {
	AllowLegacyCompatibility(context.Context) bool
}

// CurrentGateState 是一次动态读取对应的静态、动态和当前 effective 三层视图。
type CurrentGateState struct {
	StaticCaps       GateVector
	DynamicCaps      GateVector
	CurrentEffective GateVector
}

// CanonicalGateKeys 返回固定顺序的九项 canonical key 副本。
func CanonicalGateKeys() []string {
	return append([]string(nil), canonicalGateKeys[:]...)
}

// NewGateVector 要求输入精确包含九项 key；未知、缺失或别名均拒绝。
func NewGateVector(values map[string]bool) (GateVector, error) {
	if len(values) != len(canonicalGateKeys) {
		return GateVector{}, fmt.Errorf("runtime Gate vector must contain exactly %d canonical keys", len(canonicalGateKeys))
	}
	for key := range values {
		if !isCanonicalGateKey(key) {
			return GateVector{}, fmt.Errorf("unknown runtime Gate %q", key)
		}
	}
	for _, key := range canonicalGateKeys {
		if _, ok := values[key]; !ok {
			return GateVector{}, fmt.Errorf("runtime Gate vector is missing %s", key)
		}
	}
	return gateVectorFromTrustedMap(values), nil
}

// StaticGateCaps 从应用配置读取不能被动态设置放大的静态上限。
func StaticGateCaps(config *appconfig.Config) GateVector {
	if config == nil {
		return GateVector{}
	}
	return GateVector{
		AgentRuntimeEnabled:                 config.AgentRuntime.Enabled,
		AgentRuntimeAcceptNewRuns:           config.AgentRuntime.AcceptNewRuns,
		AgentRuntimeShadowMode:              config.AgentRuntime.ShadowMode,
		AgentRuntimeL1Writes:                config.AgentRuntime.L1Writes,
		AgentRuntimeL2Writes:                config.AgentRuntime.L2Writes,
		AgentRuntimeAdminQueryDatabaseDebug: config.AgentRuntime.AdminQueryDatabaseDebug,
		MCPEnabled:                          config.MCP.Enabled,
		SkillEnabled:                        config.Skill.Enabled,
		LangfuseEnabled:                     config.Observability.Langfuse.Enabled,
	}
}

// NewGateEvaluator 创建唯一 Gate evaluator；动态 reader 缺失时保持全关闭。
func NewGateEvaluator(static GateVector, reader DynamicGateReader) (*GateEvaluator, error) {
	if reader == nil {
		reader = func(context.Context, []string) (map[string]string, error) { return nil, nil }
	}
	return &GateEvaluator{static: static, read: reader}, nil
}

// StaticCaps 返回静态上限副本。
func (e *GateEvaluator) StaticCaps() GateVector {
	if e == nil {
		return GateVector{}
	}
	return e.static
}

// DynamicCaps 读取当前动态值；缺失、非法或 reader 返回未知 key 时 fail-closed。
func (e *GateEvaluator) DynamicCaps(ctx context.Context) (GateVector, error) {
	if e == nil || e.read == nil {
		return GateVector{}, nil
	}
	values, err := e.read(ctx, CanonicalGateKeys())
	if err != nil {
		return GateVector{}, err
	}
	for key := range values {
		if !isCanonicalGateKey(key) {
			return GateVector{}, nil
		}
	}
	parsed := make(map[string]bool, len(canonicalGateKeys))
	for _, key := range canonicalGateKeys {
		parsed[key] = values[key] == "true"
	}
	return gateVectorFromTrustedMap(parsed), nil
}

// Current 返回当前静态上限与动态开关的交集；shadow 强制关闭 L1/L2。
func (e *GateEvaluator) Current(ctx context.Context) (GateVector, error) {
	state, err := e.CurrentState(ctx)
	if err != nil {
		return GateVector{}, err
	}
	return state.CurrentEffective, nil
}

// CurrentState 只读取一次动态 settings，避免管理接口返回跨更新时刻的混合视图。
func (e *GateEvaluator) CurrentState(ctx context.Context) (CurrentGateState, error) {
	if e == nil {
		return CurrentGateState{}, nil
	}
	dynamic, err := e.DynamicCaps(ctx)
	if err != nil {
		return CurrentGateState{}, err
	}
	return CurrentGateState{
		StaticCaps:       e.static,
		DynamicCaps:      dynamic,
		CurrentEffective: closeWritesInShadow(e.static.And(dynamic)),
	}, nil
}

// AllowLegacyCompatibility 仅在 durable Runtime 已启用且处于 shadow 时开放旧只读兼容入口。
func (e *GateEvaluator) AllowLegacyCompatibility(ctx context.Context) bool {
	if e == nil {
		return false
	}
	current, err := e.Current(ctx)
	return err == nil && current.Enabled(GateAgentRuntimeEnabled) && current.Enabled(GateAgentRuntimeShadowMode)
}

// AllowLegacyOpsWrites 实现旧 Ops 已有的兼容 Gate；durable Context 仍由调用方优先拒绝。
func (e *GateEvaluator) AllowLegacyOpsWrites(ctx context.Context) bool {
	return e.AllowLegacyCompatibility(ctx)
}

// Effective 返回历史 frozen Gate 与当前 Gate 的交集；任何一层都只能关闭能力。
func (e *GateEvaluator) Effective(ctx context.Context, frozen FrozenRuntimeSnapshot) (GateVector, error) {
	current, err := e.Current(ctx)
	if err != nil {
		return GateVector{}, err
	}
	effective := frozen.Gates().And(current)
	if frozen.FeatureGate(GateAgentRuntimeShadowMode) || current.Enabled(GateAgentRuntimeShadowMode) {
		effective.AgentRuntimeL1Writes = false
		effective.AgentRuntimeL2Writes = false
	}
	return effective, nil
}

// Enabled 对未知 key 一律返回 false。
func (v GateVector) Enabled(key string) bool {
	switch key {
	case GateAgentRuntimeEnabled:
		return v.AgentRuntimeEnabled
	case GateAgentRuntimeAcceptNewRuns:
		return v.AgentRuntimeAcceptNewRuns
	case GateAgentRuntimeShadowMode:
		return v.AgentRuntimeShadowMode
	case GateAgentRuntimeL1Writes:
		return v.AgentRuntimeL1Writes
	case GateAgentRuntimeL2Writes:
		return v.AgentRuntimeL2Writes
	case GateAgentRuntimeAdminQueryDatabaseDebug:
		return v.AgentRuntimeAdminQueryDatabaseDebug
	case GateMCPEnabled:
		return v.MCPEnabled
	case GateSkillEnabled:
		return v.SkillEnabled
	case GateLangfuseEnabled:
		return v.LangfuseEnabled
	default:
		return false
	}
}

// Map 返回完整九项 map 副本。
func (v GateVector) Map() map[string]bool {
	return map[string]bool{
		GateAgentRuntimeEnabled:                 v.AgentRuntimeEnabled,
		GateAgentRuntimeAcceptNewRuns:           v.AgentRuntimeAcceptNewRuns,
		GateAgentRuntimeShadowMode:              v.AgentRuntimeShadowMode,
		GateAgentRuntimeL1Writes:                v.AgentRuntimeL1Writes,
		GateAgentRuntimeL2Writes:                v.AgentRuntimeL2Writes,
		GateAgentRuntimeAdminQueryDatabaseDebug: v.AgentRuntimeAdminQueryDatabaseDebug,
		GateMCPEnabled:                          v.MCPEnabled,
		GateSkillEnabled:                        v.SkillEnabled,
		GateLangfuseEnabled:                     v.LangfuseEnabled,
	}
}

// And 逐项计算 Gate 交集。
func (v GateVector) And(other GateVector) GateVector {
	result := make(map[string]bool, len(canonicalGateKeys))
	for _, key := range canonicalGateKeys {
		result[key] = v.Enabled(key) && other.Enabled(key)
	}
	return gateVectorFromTrustedMap(result)
}

func gateVectorFromTrustedMap(values map[string]bool) GateVector {
	return GateVector{
		AgentRuntimeEnabled:                 values[GateAgentRuntimeEnabled],
		AgentRuntimeAcceptNewRuns:           values[GateAgentRuntimeAcceptNewRuns],
		AgentRuntimeShadowMode:              values[GateAgentRuntimeShadowMode],
		AgentRuntimeL1Writes:                values[GateAgentRuntimeL1Writes],
		AgentRuntimeL2Writes:                values[GateAgentRuntimeL2Writes],
		AgentRuntimeAdminQueryDatabaseDebug: values[GateAgentRuntimeAdminQueryDatabaseDebug],
		MCPEnabled:                          values[GateMCPEnabled],
		SkillEnabled:                        values[GateSkillEnabled],
		LangfuseEnabled:                     values[GateLangfuseEnabled],
	}
}

func closeWritesInShadow(vector GateVector) GateVector {
	if vector.AgentRuntimeShadowMode {
		vector.AgentRuntimeL1Writes = false
		vector.AgentRuntimeL2Writes = false
	}
	return vector
}

func isCanonicalGateKey(key string) bool {
	_, ok := canonicalGateKeySet[key]
	return ok
}
