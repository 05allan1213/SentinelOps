package policy

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCatalogFixedRiskMatrixAndMetadata(t *testing.T) {
	t.Parallel()

	expected := map[string]RiskLevel{
		"query_events":           RiskL0,
		"search_similar_events":  RiskL0,
		"query_subscriptions":    RiskL0,
		"query_reports":          RiskL0,
		"query_report_templates": RiskL0,
		"get_current_time":       RiskL0,
		"query_internal_docs":    RiskL0,
		"web_search":             RiskL0,
		"trigger_ops":            RiskL0,
		"create_report":          RiskL1,
		"save_intelligence":      RiskL1,
		"update_event_status":    RiskL1,
		"block_ip":               RiskL2,
		"notify_dingtalk":        RiskL2,
		"notify_wecom":           RiskL2,
		"notify_email":           RiskL2,
		"webhook_out":            RiskL2,
	}

	entries := CatalogEntries()
	for name, risk := range expected {
		entry, ok := entries[name]
		if !ok {
			t.Errorf("Catalog 缺少 %q", name)
			continue
		}
		if entry.Name != name || entry.Risk != risk {
			t.Errorf("Catalog[%q] = name %q risk %q, want %q/%q", name, entry.Name, entry.Risk, name, risk)
		}
		if entry.Revision == "" || len(entry.SchemaHash) != 64 || entry.Policy == "" {
			t.Errorf("Catalog[%q] 缺少 revision/schema hash/policy: %+v", name, entry)
		}
		if risk == RiskL0 && (entry.EffectType != EffectNone || len(entry.EffectSteps) != 0) {
			t.Errorf("L0 %q 不得声明 Effect: %+v", name, entry)
		}
		if risk != RiskL0 && (entry.EffectType == EffectNone || !containsString(entry.EffectSteps, "primary")) {
			t.Errorf("Mutation %q 必须声明确定性 primary Effect: %+v", name, entry)
		}
	}

	debug, ok := entries["query_database"]
	if !ok {
		t.Fatal("保留的 query_database 缺少专用 admin/debug Catalog Policy")
	}
	if debug.Audience != AudienceAdminDebug || debug.RequiredGate != AdminQueryDatabaseDebugGate || debug.RequiredRole != RoleAdmin || !debug.SelectOnly || len(debug.AllowedRelations) == 0 {
		t.Fatalf("query_database 专用 Policy 不完整: %+v", debug)
	}
}

func TestCatalogUnknownFailsClosedAndMutationDisabledBeforeEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := LookupCatalog("unknown_tool"); !errors.Is(err, ErrUnknownCatalogTool) {
		t.Fatalf("未知 Tool 应 fail-closed，得到 %v", err)
	}

	endpointCalls := 0
	for _, name := range []string{"create_report", "save_intelligence", "update_event_status", "block_ip", "notify_dingtalk", "notify_wecom", "notify_email", "webhook_out"} {
		err := RequireExecutable(name)
		if err == nil {
			endpointCalls++
			continue
		}
		var policyErr *ToolPolicyError
		if !errors.As(err, &policyErr) || policyErr.Code != PolicyMutationDisabled || policyErr.ToolName != name {
			t.Errorf("%q 应返回结构化 %s，得到 %v", name, PolicyMutationDisabled, err)
		}
	}
	if endpointCalls != 0 {
		t.Fatalf("Mutation endpoint 调用次数 = %d, want 0", endpointCalls)
	}
	if err := RequireExecutable("query_events"); err != nil {
		t.Fatalf("Catalog L0 应可进入后续 RuntimeHandler 检查: %v", err)
	}
	if err := RequireExecutable("unknown_tool"); !errors.Is(err, ErrUnknownCatalogTool) {
		t.Fatalf("未知 Tool 应在 endpoint 前 fail-closed: %v", err)
	}
}

func TestInventoryMatchesManifestAndLeastPrivilege(t *testing.T) {
	t.Parallel()

	type inventoryManifest struct {
		Version string              `yaml:"version"`
		Agents  map[string][]string `yaml:"agents"`
	}
	raw, err := os.ReadFile("../../../manifest/agent/tool-inventory-v1.yaml")
	if err != nil {
		t.Fatalf("读取 inventory manifest: %v", err)
	}
	var manifest inventoryManifest
	if err = yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("解析 inventory manifest: %v", err)
	}
	if manifest.Version != ToolInventoryVersion {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, ToolInventoryVersion)
	}

	inventories := DurableInventories()
	if !reflect.DeepEqual(manifest.Agents, inventories) {
		t.Fatalf("manifest 与服务端 inventory 真值漂移\nmanifest=%v\nserver=%v", manifest.Agents, inventories)
	}
	for agentName, names := range inventories {
		if err := ValidateDurableInventory(agentName, names); err != nil {
			t.Errorf("inventory %q: %v", agentName, err)
		}
		for _, name := range names {
			if name == "query_database" {
				t.Errorf("普通 durable inventory %q 包含 query_database", agentName)
			}
		}
	}

	for _, agentName := range []string{"EventAnalysisAgent", "RiskAgent", "SolveAgent"} {
		for _, name := range inventories[agentName] {
			entry, err := LookupCatalog(name)
			if err != nil || entry.Risk != RiskL0 {
				t.Errorf("%s 暴露 Mutation/未知 Tool %q: entry=%+v err=%v", agentName, name, entry, err)
			}
		}
	}
	for _, domainTool := range []string{"query_events", "query_reports", "query_internal_docs", "query_subscriptions"} {
		if !inventoryContains(inventories, domainTool) {
			t.Errorf("普通 durable inventory 未复用领域 Tool %q", domainTool)
		}
	}
	if err := ValidateDurableInventory("bad", []string{"query_database"}); err == nil || !strings.Contains(err.Error(), "admin/debug") {
		t.Fatalf("query_database 应被普通 durable inventory 拒绝: %v", err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func inventoryContains(inventories map[string][]string, expected string) bool {
	for _, names := range inventories {
		if containsString(names, expected) {
			return true
		}
	}
	return false
}
