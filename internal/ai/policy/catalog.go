package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/schema"
)

const (
	// PolicyMutationDisabled 是 Effect/HITL 接线前 Mutation Tool 的固定拒绝码。
	PolicyMutationDisabled = "POLICY_MUTATION_DISABLED"
	// ToolInventoryVersion 是 durable 专业 Agent inventory 的版本标识。
	ToolInventoryVersion = "sentinelops.agent.tool-inventory/v1"
	// AdminQueryDatabaseDebugGate 是通用只读 SQL debug builder 的默认关闭 Gate。
	AdminQueryDatabaseDebugGate = "agent_runtime.admin_query_database_debug"
)

var (
	// ErrUnknownCatalogTool 表示服务端 Catalog 未登记 Tool，调用方必须 fail-closed。
	ErrUnknownCatalogTool = errors.New("unknown catalog tool")
	// ErrMutationDisabled 表示 L1/L2 在 Effect/HITL 接线前固定不可执行。
	ErrMutationDisabled = errors.New(PolicyMutationDisabled)
)

// EffectType 是 Catalog 声明的唯一 Effect 执行语义。
type EffectType string

const (
	EffectNone                     EffectType = ""
	EffectTransactionalDB          EffectType = "transactional_db"
	EffectReconcilable             EffectType = "reconcilable"
	EffectProviderIdempotent       EffectType = "provider_idempotent"
	EffectNonReconciliableExternal EffectType = "non_reconcilable_external"
)

// ToolAudience 限制 Catalog 项可进入的 builder 类型。
type ToolAudience string

const (
	AudienceDurable    ToolAudience = "durable"
	AudienceAdminDebug ToolAudience = "admin_debug"
)

// CatalogEntry 是现有 Tool Registry 的服务端风险与 Effect 元数据侧表。
// 它只保存静态值，不保存 Tool 实例、endpoint 或动作函数。
type CatalogEntry struct {
	Name             string
	Risk             RiskLevel
	Revision         string
	SchemaHash       string
	EffectType       EffectType
	EffectSteps      []string
	Policy           string
	Audience         ToolAudience
	RequiredGate     string
	RequiredRole     Role
	SelectOnly       bool
	AllowedRelations []string
}

// ToolPolicyError 是 RuntimeHandler 接线前可稳定识别的结构化拒绝。
type ToolPolicyError struct {
	Code     string    `json:"code"`
	ToolName string    `json:"tool_name"`
	Risk     RiskLevel `json:"risk_level"`
}

func (e *ToolPolicyError) Error() string {
	return fmt.Sprintf("%s: tool=%s risk=%s", e.Code, e.ToolName, e.Risk)
}

func (e *ToolPolicyError) Unwrap() error {
	if e.Code == PolicyMutationDisabled {
		return ErrMutationDisabled
	}
	return nil
}

// 首版 Catalog 固定风险矩阵。schema hash 在 ToolInfo 参数 Schema 变化时显式升级。
var catalog = map[string]CatalogEntry{
	"query_events":           durableL0("query_events", "04c80b955a2c6498876daf6574d0df67721bd0d2bb7a063ec09d8afd655d3b58", "domain_read_v1"),
	"search_similar_events":  durableL0("search_similar_events", "59dccda02393291044827b605040395a2e6e298c363785d98153a5c8a1295f08", "evidence_read_v1"),
	"query_subscriptions":    durableL0("query_subscriptions", "d57681d70606e25bd937035ee6ee6ec43b0dfa0c98c849cb0b49b339ffd7ffa8", "domain_read_v1"),
	"query_reports":          durableL0("query_reports", "d61597f04799ef704eb60971808d5dd35ae50ea6623dd3325e2b144a625a2752", "domain_read_v1"),
	"query_report_templates": durableL0("query_report_templates", "1ae5cf323d7d6e0d3c87ea3ef84b00175620a931c8089ec2a48396f626efda44", "domain_read_v1"),
	"get_current_time":       durableL0("get_current_time", "99334726611ccf58a148b0814696bfa6fe08c1b2d027e946beccf5a74331c9aa", "framework_read_v1"),
	"query_internal_docs":    durableL0("query_internal_docs", "72670b9b54054b90b6204b82508e25034a1317683922a91e8214dacd89894e3a", "evidence_read_v1"),
	"web_search":             durableL0("web_search", "fd92f268c8ceebab9e5257ed0abafe365069dd8d7771ce0861c5929e17575a5f", "external_read_v1"),
	"trigger_ops":            durableL0("trigger_ops", "537e34e78497d1d55232bde268b3865c95b958f4876e6ea123991774ce95d14e", "ops_proposal_v1"),
	"create_report":          durableMutation("create_report", RiskL1, "e585a0e4204df40f004106c31106f8878a5fda39dfe0060b0f61bf160b9ddd9d", EffectTransactionalDB, []string{"primary"}),
	"save_intelligence":      durableMutation("save_intelligence", RiskL1, "50747841fcbf81c7774210eeaba2924603f48d24615f76fa02f6f018a98cf509", EffectTransactionalDB, []string{"primary", "milvus_index"}),
	"update_event_status":    durableMutation("update_event_status", RiskL1, "40bf387edca943bac035119082e40f7c10b824c9fb5bdabd1d14f129f3e47fdc", EffectTransactionalDB, []string{"primary"}),
	"block_ip":               durableMutation("block_ip", RiskL2, "be34ad3658d92b5bcd16d692d7183de57cec6f14305554a8e0add0aa1a370573", EffectReconcilable, []string{"primary", "nginx_reload"}),
	"notify_dingtalk":        durableMutation("notify_dingtalk", RiskL2, "703c119af23182856ed4ea05b0f1bf3450a34bda75fdf46a9b7de8f37c3ca419", EffectNonReconciliableExternal, []string{"primary"}),
	"notify_wecom":           durableMutation("notify_wecom", RiskL2, "2586c79fad8589a5093513cc93aa5359360004a5bc5d788ba62e6335a64bcc56", EffectNonReconciliableExternal, []string{"primary"}),
	"notify_email":           durableMutation("notify_email", RiskL2, "eef7181e2387c8d36aad2f7dbffec4295d21f2ffb60c08feab6893983aca28d1", EffectNonReconciliableExternal, []string{"primary"}),
	"webhook_out":            durableMutation("webhook_out", RiskL2, "585972c40375f8bf1e8a97276026c4666b25f1aa05a836ed64b76c633466ac3d", EffectProviderIdempotent, []string{"primary"}),
	"query_database": {
		Name:             "query_database",
		Risk:             RiskL0,
		Revision:         "v1",
		SchemaHash:       "06ed48155af18d67f2d9d72d70f8294b8b4a497e5599a1512fa7a514220a95e8",
		Policy:           "admin_debug_select_allowlist_v1",
		Audience:         AudienceAdminDebug,
		RequiredGate:     AdminQueryDatabaseDebugGate,
		RequiredRole:     RoleAdmin,
		SelectOnly:       true,
		AllowedRelations: []string{"events", "reports", "subscriptions"},
	},
	"event_analysis_agent":  durableFramework("event_analysis_agent", "309ed652cdc1b2290633f633f9f2efff36d6b647ed1a48a7e6b6a22dcf8f1a83"),
	"report_agent":          durableFramework("report_agent", "309ed652cdc1b2290633f633f9f2efff36d6b647ed1a48a7e6b6a22dcf8f1a83"),
	"risk_assessment_agent": durableFramework("risk_assessment_agent", "309ed652cdc1b2290633f633f9f2efff36d6b647ed1a48a7e6b6a22dcf8f1a83"),
	"solve_agent":           durableFramework("solve_agent", "309ed652cdc1b2290633f633f9f2efff36d6b647ed1a48a7e6b6a22dcf8f1a83"),
	"intelligence_agent":    durableFramework("intelligence_agent", "309ed652cdc1b2290633f633f9f2efff36d6b647ed1a48a7e6b6a22dcf8f1a83"),
	"ops_agent":             durableFramework("ops_agent", "309ed652cdc1b2290633f633f9f2efff36d6b647ed1a48a7e6b6a22dcf8f1a83"),
	"mcp_agent":             durableFramework("mcp_agent", "309ed652cdc1b2290633f633f9f2efff36d6b647ed1a48a7e6b6a22dcf8f1a83"),
}

var durableInventories = map[string][]string{
	"EventAnalysisAgent": {"query_events", "search_similar_events", "query_subscriptions", "query_reports", "query_internal_docs", "get_current_time", "web_search"},
	"RiskAgent":          {"query_events", "query_reports", "search_similar_events", "query_internal_docs", "query_subscriptions", "get_current_time", "web_search"},
	"SolveAgent":         {"search_similar_events", "query_internal_docs", "web_search"},
	"ReportAgent":        {"query_events", "query_reports", "query_report_templates", "search_similar_events", "get_current_time", "create_report", "web_search"},
	"IntelligenceAgent":  {"query_internal_docs", "get_current_time", "web_search", "save_intelligence"},
	"OpsAgent":           {"query_events", "trigger_ops", "update_event_status", "block_ip", "notify_dingtalk", "notify_wecom", "notify_email", "webhook_out", "get_current_time"},
}

func durableL0(name, schemaHash, policy string) CatalogEntry {
	return CatalogEntry{Name: name, Risk: RiskL0, Revision: "v1", SchemaHash: schemaHash, Policy: policy, Audience: AudienceDurable}
}

func durableMutation(name string, risk RiskLevel, schemaHash string, effectType EffectType, steps []string) CatalogEntry {
	return CatalogEntry{
		Name: name, Risk: risk, Revision: "v1", SchemaHash: schemaHash,
		EffectType: effectType, EffectSteps: steps, Policy: "mutation_disabled_v1", Audience: AudienceDurable,
	}
}

func durableFramework(name, schemaHash string) CatalogEntry {
	return CatalogEntry{Name: name, Risk: RiskL0, Revision: "v1", SchemaHash: schemaHash, Policy: "framework_agent_tool_v1", Audience: AudienceDurable}
}

// LookupCatalog 返回不可修改 Catalog 真值的值副本。
func LookupCatalog(name string) (CatalogEntry, error) {
	entry, ok := catalog[name]
	if !ok {
		return CatalogEntry{}, fmt.Errorf("tool %q: %w", name, ErrUnknownCatalogTool)
	}
	return cloneCatalogEntry(entry), nil
}

// CatalogEntries 返回 Catalog 的深拷贝，防止调用方修改服务端元数据。
func CatalogEntries() map[string]CatalogEntry {
	entries := make(map[string]CatalogEntry, len(catalog))
	for name, entry := range catalog {
		entries[name] = cloneCatalogEntry(entry)
	}
	return entries
}

// RequireExecutable 对未知 Tool 和尚未接线的 Mutation Tool fail-closed。
// L0 通过只表示可进入 P14 的 Scope、预算、deadline 与 Trace 检查，不代表直接授权 endpoint。
func RequireExecutable(name string) error {
	entry, err := LookupCatalog(name)
	if err != nil {
		return err
	}
	if entry.Audience != AudienceDurable {
		return fmt.Errorf("tool %q audience %q is not durable: %w", name, entry.Audience, ErrUnknownCatalogTool)
	}
	if entry.Risk != RiskL0 {
		return &ToolPolicyError{Code: PolicyMutationDisabled, ToolName: name, Risk: entry.Risk}
	}
	return nil
}

// ToolSchemaHash 从 Eino ToolInfo 的参数 JSON Schema 计算 canonical SHA-256。
func ToolSchemaHash(info *schema.ToolInfo) (string, error) {
	if info == nil {
		return "", fmt.Errorf("ToolInfo is nil")
	}
	var schemaValue any
	if info.ParamsOneOf != nil {
		jsonSchema, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			return "", fmt.Errorf("convert ParamsOneOf: %w", err)
		}
		raw, err := json.Marshal(jsonSchema)
		if err != nil {
			return "", fmt.Errorf("marshal JSON Schema: %w", err)
		}
		schemaValue, err = decodeJSON(raw)
		if err != nil {
			return "", fmt.Errorf("decode JSON Schema: %w", err)
		}
	}
	canonical, err := CanonicalJSON(schemaValue)
	if err != nil {
		return "", fmt.Errorf("canonicalize JSON Schema: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// CanonicalToolArgumentsJSON 校验 Tool 参数是无重名 key 的 JSON object 并返回 canonical bytes。
func CanonicalToolArgumentsJSON(raw []byte) ([]byte, error) {
	decoded, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	if _, ok := decoded.(map[string]any); !ok {
		return nil, fmt.Errorf("tool arguments must be a JSON object")
	}
	return CanonicalJSON(decoded)
}

// DurableInventories 返回服务端专业 Agent inventory 真值的深拷贝。
func DurableInventories() map[string][]string {
	inventories := make(map[string][]string, len(durableInventories))
	for name, tools := range durableInventories {
		inventories[name] = append([]string(nil), tools...)
	}
	return inventories
}

// ValidateDurableInventory 校验普通 durable Agent 的 Catalog 完整性与 audience。
func ValidateDurableInventory(agentName string, names []string) error {
	if strings.TrimSpace(agentName) == "" {
		return fmt.Errorf("agent name is required")
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("agent %q has duplicate tool %q", agentName, name)
		}
		seen[name] = struct{}{}
		entry, err := LookupCatalog(name)
		if err != nil {
			return fmt.Errorf("agent %q: %w", agentName, err)
		}
		if entry.Audience != AudienceDurable {
			return fmt.Errorf("agent %q tool %q is restricted to %s admin/debug builder", agentName, name, entry.Audience)
		}
	}
	return nil
}

// RequiredDurableToolNames 返回所有普通 durable inventory 的去重有序并集。
func RequiredDurableToolNames() []string {
	set := make(map[string]struct{})
	for _, names := range durableInventories {
		for _, name := range names {
			set[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DurableFrameworkToolNames 返回 Executor 暴露的官方 AgentTool Catalog 名称。
// framework Tool 只计量编排调用，不拥有 Effect step。
func DurableFrameworkToolNames() []string {
	names := make([]string, 0, 6)
	for name, entry := range catalog {
		if entry.Audience == AudienceDurable && entry.Policy == "framework_agent_tool_v1" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// DefaultRegistryToolNames 返回默认 Registry 必须持有且校验 Schema 的 Catalog 名称。
func DefaultRegistryToolNames() []string {
	names := make([]string, 0, len(catalog)-1)
	for name, entry := range catalog {
		if entry.Audience == AudienceDurable && entry.Policy != "framework_agent_tool_v1" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// ValidateCatalog 校验服务端 Catalog 与专业 Agent inventory 的静态自洽性。
func ValidateCatalog() error {
	for key, entry := range catalog {
		if key != entry.Name || strings.TrimSpace(entry.Revision) == "" || strings.TrimSpace(entry.Policy) == "" {
			return fmt.Errorf("catalog entry %q has incomplete identity", key)
		}
		if err := entry.Risk.Validate(); err != nil {
			return fmt.Errorf("catalog entry %q: %w", key, err)
		}
		switch entry.Audience {
		case AudienceDurable, AudienceAdminDebug:
		default:
			return fmt.Errorf("catalog entry %q has unknown audience %q", key, entry.Audience)
		}
		if err := validateSHA256("schema_hash", entry.SchemaHash); err != nil {
			return fmt.Errorf("catalog entry %q: %w", key, err)
		}
		if entry.Risk == RiskL0 && (entry.EffectType != EffectNone || len(entry.EffectSteps) != 0) {
			return fmt.Errorf("catalog L0 entry %q declares Effect", key)
		}
		if entry.Risk != RiskL0 && (entry.EffectType == EffectNone || len(entry.EffectSteps) == 0 || entry.EffectSteps[0] != "primary") {
			return fmt.Errorf("catalog mutation entry %q lacks deterministic primary Effect", key)
		}
		if entry.Audience == AudienceDurable && entry.Risk != RiskL0 && entry.Policy != "mutation_disabled_v1" {
			return fmt.Errorf("catalog mutation entry %q is not disabled", key)
		}
		steps := make(map[string]struct{}, len(entry.EffectSteps))
		for _, step := range entry.EffectSteps {
			if strings.TrimSpace(step) == "" {
				return fmt.Errorf("catalog entry %q has empty Effect step", key)
			}
			if _, duplicate := steps[step]; duplicate {
				return fmt.Errorf("catalog entry %q has duplicate Effect step %q", key, step)
			}
			steps[step] = struct{}{}
		}
	}
	for agentName, names := range durableInventories {
		if err := ValidateDurableInventory(agentName, names); err != nil {
			return err
		}
	}
	return nil
}

func cloneCatalogEntry(entry CatalogEntry) CatalogEntry {
	entry.EffectSteps = append([]string(nil), entry.EffectSteps...)
	entry.AllowedRelations = append([]string(nil), entry.AllowedRelations...)
	return entry
}
