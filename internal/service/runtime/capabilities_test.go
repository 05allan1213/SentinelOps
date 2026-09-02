package runtime

import (
	"context"
	"strings"
	"testing"

	"SentinelOps/internal/ai/policy"
	appconfig "SentinelOps/internal/config"
)

func TestCapabilitiesUseStrictCatalog(t *testing.T) {
	res, err := (&RuntimeService{}).GetCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := len(policy.RequiredDurableToolNames()) + len(policy.DurableFrameworkToolNames())
	if len(res.Items) != want {
		t.Fatalf("items=%d want=%d", len(res.Items), want)
	}
	for _, item := range res.Items {
		if item.Revision == "" || item.SchemaHash == "" {
			t.Fatalf("incomplete item: %+v", item)
		}
	}
}

func TestCapabilitiesSeparateConfiguredAndObserved(t *testing.T) {
	cfg := &appconfig.Config{MCP: appconfig.MCPConfig{Enabled: true, Servers: map[string]appconfig.MCPServer{"demo": {Enabled: true, Transport: "stdio", Command: "/bin/echo", CWD: "/tmp", AllowedTools: []string{"read"}}}}}
	res, err := (&RuntimeService{Config: cfg}).GetCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) == len(policy.RequiredDurableToolNames())+len(policy.DurableFrameworkToolNames()) {
		t.Logf("no mcp item; config conversion likely rejected")
	}
	for _, item := range res.Items {
		if item.Source == "mcp" {
			if item.ConfiguredState != "disabled" || item.ObservedWorkerState != "unknown" || item.Availability != "unavailable" || item.ReasonCode != "not_observed" {
				t.Fatalf("mcp state=%+v", item)
			}
			return
		}
	}
	t.Fatal("missing mcp capability")
}

func TestCapabilitiesHideMCPSecrets(t *testing.T) {
	cfg := &appconfig.Config{MCP: appconfig.MCPConfig{Enabled: true, Servers: map[string]appconfig.MCPServer{"demo": {Enabled: true, Transport: "stdio", Command: "/bin/echo", CWD: "/tmp", HeaderName: "Authorization", HeaderRef: "env:TEST_TOKEN"}}}}
	res, err := (&RuntimeService{Config: cfg}).GetCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range res.Items {
		if strings.Contains(item.Name, "secret") || strings.Contains(item.Name, "Authorization") {
			t.Fatalf("secret leaked: %+v", item)
		}
	}
}

func TestCapabilitiesAreReadOnly(t *testing.T) {
	before := policy.CatalogEntries()
	_, _ = (&RuntimeService{}).GetCapabilities(context.Background())
	after := policy.CatalogEntries()
	if len(before) != len(after) {
		t.Fatal("catalog mutated")
	}
}
