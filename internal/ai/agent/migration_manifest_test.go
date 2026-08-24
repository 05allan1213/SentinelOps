package agent_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	agentprompts "SentinelOps/internal/ai/prompt/agents"

	"gopkg.in/yaml.v3"
)

type migrationManifest struct {
	Version                      string          `yaml:"version"`
	PromptInputs                 []string        `yaml:"prompt_inputs"`
	ApprovedLeastPrivilegeDeltas []manifestDelta `yaml:"approved_least_privilege_deltas"`
	Agents                       []manifestAgent `yaml:"agents"`
}

type manifestDelta struct {
	ID string `yaml:"id"`
}

type manifestAgent struct {
	LegacyGraphName     string   `yaml:"legacy_graph_name"`
	Name                string   `yaml:"name"`
	Description         string   `yaml:"description"`
	InstructionRef      string   `yaml:"instruction_ref"`
	InstructionSHA256   string   `yaml:"instruction_sha256"`
	InstructionInputs   []string `yaml:"instruction_inputs"`
	LegacyToolInventory []string `yaml:"legacy_tool_inventory"`
	ToolInventory       []string `yaml:"tool_inventory"`
	ReturnDirectly      []string `yaml:"return_directly"`
	MaxIterations       int      `yaml:"max_iterations"`
}

type expectedAgentContract struct {
	graph             string
	description       string
	instructionRef    string
	instruction       string
	instructionInputs []string
	legacyTools       []string
	durableTools      []string
	maxIterations     int
}

func TestMigrationManifestFreezesSpecialistContracts(t *testing.T) {
	raw, err := os.ReadFile("../../../manifest/agent/migration-contract-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var manifest migrationManifest
	if err = yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}

	if manifest.Version != "sentinelops.agent.migration-contract/v1" {
		t.Fatalf("version = %q", manifest.Version)
	}
	if !reflect.DeepEqual(manifest.PromptInputs, []string{"query", "history", "documents", "date"}) {
		t.Fatalf("prompt_inputs = %#v", manifest.PromptInputs)
	}
	deltaIDs := make([]string, 0, len(manifest.ApprovedLeastPrivilegeDeltas))
	for _, delta := range manifest.ApprovedLeastPrivilegeDeltas {
		deltaIDs = append(deltaIDs, delta.ID)
	}
	if !reflect.DeepEqual(deltaIDs, []string{"event-analysis-remove-save-intelligence", "durable-remove-query-database"}) {
		t.Fatalf("approved delta IDs = %#v", deltaIDs)
	}

	expected := expectedMigrationContracts()
	if len(manifest.Agents) != len(expected) {
		t.Fatalf("agents = %d, want %d", len(manifest.Agents), len(expected))
	}
	seen := make(map[string]struct{}, len(manifest.Agents))
	for _, got := range manifest.Agents {
		want, ok := expected[got.Name]
		if !ok {
			t.Fatalf("unexpected agent %q", got.Name)
		}
		if _, duplicate := seen[got.Name]; duplicate {
			t.Fatalf("duplicate agent %q", got.Name)
		}
		seen[got.Name] = struct{}{}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(want.instruction)))
		if got.LegacyGraphName != want.graph || got.Description != want.description ||
			got.InstructionRef != want.instructionRef || got.InstructionSHA256 != hash ||
			!reflect.DeepEqual(got.InstructionInputs, want.instructionInputs) ||
			!reflect.DeepEqual(got.LegacyToolInventory, want.legacyTools) ||
			!reflect.DeepEqual(got.ToolInventory, want.durableTools) ||
			len(got.ReturnDirectly) != 0 || got.MaxIterations != want.maxIterations {
			t.Errorf("agent %q contract drifted: got %#v, want instruction hash %s", got.Name, got, hash)
		}
	}
}

func TestMigrationManifestDurableInventoryMatchesCatalog(t *testing.T) {
	type inventoryManifest struct {
		Agents map[string][]string `yaml:"agents"`
	}
	raw, err := os.ReadFile("../../../manifest/agent/tool-inventory-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var inventory inventoryManifest
	if err = yaml.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	migrationRaw, err := os.ReadFile("../../../manifest/agent/migration-contract-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var manifest migrationManifest
	if err = yaml.Unmarshal(migrationRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, contract := range manifest.Agents {
		want, ok := inventory.Agents[contract.LegacyGraphName]
		if !ok {
			t.Fatalf("tool inventory missing %q", contract.LegacyGraphName)
		}
		if !reflect.DeepEqual(contract.ToolInventory, want) {
			t.Fatalf("%s durable tools = %#v, want catalog %#v", contract.Name, contract.ToolInventory, want)
		}
	}

	names := make([]string, 0, len(inventory.Agents))
	for name := range inventory.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != len(manifest.Agents) {
		t.Fatalf("catalog agents = %#v, manifest count = %d", names, len(manifest.Agents))
	}
}

func expectedMigrationContracts() map[string]expectedAgentContract {
	eventTools := []string{"query_events", "search_similar_events", "query_subscriptions", "query_reports", "query_internal_docs", "get_current_time", "web_search"}
	return map[string]expectedAgentContract{
		"event_analysis_agent": {
			graph: "EventAnalysisAgent", description: "Call the Event Analysis Agent to query, analyze and correlate security events. Handles: recent events listing, CVE analysis, severity distribution, event timeline, subscription status, threat correlation. Returns structured analysis results.",
			instructionRef: "agents.EventAnalysis", instruction: agentprompts.EventAnalysis, instructionInputs: []string{"date", "documents"},
			legacyTools: append(append([]string(nil), eventTools...), "save_intelligence"), durableTools: eventTools, maxIterations: 25,
		},
		"report_agent": {
			graph: "ReportAgent", description: "Call the Report Agent to generate structured security reports (weekly/monthly/custom). Handles: creating new reports, querying existing reports, fetching report templates, summarizing event trends. Returns report content or creation confirmation.",
			instructionRef: "agents.Report", instruction: agentprompts.Report, instructionInputs: []string{"date", "documents"},
			legacyTools: []string{"query_events", "query_reports", "query_report_templates", "search_similar_events", "get_current_time", "create_report", "web_search"}, durableTools: []string{"query_events", "query_reports", "query_report_templates", "search_similar_events", "get_current_time", "create_report", "web_search"}, maxIterations: 30,
		},
		"risk_assessment_agent": {
			graph: "RiskAgent", description: "Call the Risk Assessment Agent to evaluate CVE severity, attack paths, and impact scope. Handles: CVE risk scoring, vulnerability assessment, CVSS analysis, attack surface analysis, mitigation priority ranking. Returns structured risk assessment.",
			instructionRef: "agents.Risk", instruction: agentprompts.Risk, instructionInputs: []string{"date", "documents"},
			legacyTools: []string{"query_events", "query_reports", "search_similar_events", "query_internal_docs", "query_subscriptions", "get_current_time", "web_search"}, durableTools: []string{"query_events", "query_reports", "search_similar_events", "query_internal_docs", "query_subscriptions", "get_current_time", "web_search"}, maxIterations: 25,
		},
		"solve_agent": {
			graph: "SolveAgent", description: "Call the Solve Agent to generate emergency response plans for specific security incidents. Handles: incident containment steps, patch recommendations, remediation procedures, recovery guidance for a single event. Returns structured three-phase response plan.",
			instructionRef: "agents.Solve", instruction: agentprompts.Solve, instructionInputs: []string{"date", "documents"},
			legacyTools: []string{"search_similar_events", "query_internal_docs", "web_search"}, durableTools: []string{"search_similar_events", "query_internal_docs", "web_search"}, maxIterations: 10,
		},
		"intelligence_agent": {
			graph: "IntelligenceAgent", description: "Call the Intelligence Agent to search and analyze the latest threat intelligence from the internet. Handles: CVE details lookup, vulnerability advisories, exploit PoC status, threat actor profiling, malicious IP/domain reputation. Automatically saves findings to the local knowledge base. Returns structured threat intelligence report.",
			instructionRef: "agents.Intelligence", instruction: agentprompts.Intelligence, instructionInputs: []string{"date", "documents"},
			legacyTools: []string{"query_internal_docs", "get_current_time", "web_search", "save_intelligence"}, durableTools: []string{"query_internal_docs", "get_current_time", "web_search", "save_intelligence"}, maxIterations: 12,
		},
		"ops_agent": {
			graph: "OpsAgent", description: "Call the Ops Agent to trigger automated incident response for a specific security event. Handles: IP blocking, multi-channel alert notifications (DingTalk/WeCom/Email), event status updates. Requires event_id in the query. Returns execution result.",
			instructionRef: "agents.Ops", instruction: agentprompts.Ops, instructionInputs: []string{"date"},
			legacyTools: []string{"query_events", "trigger_ops", "update_event_status", "block_ip", "notify_dingtalk", "notify_wecom", "notify_email", "get_current_time"}, durableTools: []string{"query_events", "trigger_ops", "update_event_status", "block_ip", "notify_dingtalk", "notify_wecom", "notify_email", "get_current_time"}, maxIterations: 20,
		},
	}
}
