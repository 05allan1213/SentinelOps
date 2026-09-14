package runtime

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	v1 "SentinelOps/api/runtime/v1"
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

func TestCapabilitiesPaginationPreservesFullCatalogFacts(t *testing.T) {
	cfg := &appconfig.Config{MCP: appconfig.MCPConfig{Servers: map[string]appconfig.MCPServer{}}}
	for i := 0; i < 105; i++ {
		cfg.MCP.Servers[fmt.Sprintf("server-%03d", i)] = appconfig.MCPServer{Transport: "stdio", Command: "/bin/echo", CWD: "/tmp"}
	}
	service := &RuntimeService{Config: cfg}
	all, err := service.GetCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) <= 100 || all.ReasonCode != "not_observed" {
		t.Fatalf("catalog=%+v", all)
	}
	for i := 1; i < len(all.Items); i++ {
		a, b := all.Items[i-1], all.Items[i]
		if a.Source > b.Source || (a.Source == b.Source && a.Name > b.Name) {
			t.Fatal("unstable catalog order")
		}
	}
	var combined []v1.CapabilityDTO
	for page := 1; page <= (len(all.Items)+49)/50; page++ {
		got, err := service.GetCapabilities(context.Background(), v1.PageRequest{Page: page, PageSize: 50})
		if err != nil {
			t.Fatal(err)
		}
		if got.ResourceMeta != all.ResourceMeta || got.Page.Total != int64(len(all.Items)) || got.Page.Page != page || got.Page.PageSize != 50 || got.Page.HasNext != (page*50 < len(all.Items)) {
			t.Fatalf("page=%+v", got)
		}
		combined = append(combined, got.Items...)
	}
	if !reflect.DeepEqual(combined, all.Items) {
		t.Fatal("pagination lost or changed catalog facts")
	}
	for _, page := range []int{len(all.Items) + 1, int(^uint(0) >> 1)} {
		got, err := service.GetCapabilities(context.Background(), v1.PageRequest{Page: page, PageSize: 100})
		if err != nil {
			t.Fatal(err)
		}
		if got.Items == nil || len(got.Items) != 0 || got.Page.HasNext || got.Page.Page != page || got.ResourceMeta != all.ResourceMeta {
			t.Fatalf("out of range=%+v", got)
		}
	}
	for _, request := range []v1.PageRequest{{0, 50}, {1, 0}, {1, 101}, {-1, 1}} {
		if _, err := service.GetCapabilities(context.Background(), request); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
}

func TestCapabilitiesEmptyAndUnavailablePagination(t *testing.T) {
	for _, meta := range []v1.ResourceMeta{completeSafetyMeta(), {Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: "worker_snapshot_malformed", NotRun: true}} {
		got, err := paginateCapabilities(nil, meta, []v1.PageRequest{{Page: 3, PageSize: 50}})
		if err != nil || got.Items == nil || len(got.Items) != 0 || got.Page != (v1.PageMeta{Page: 3, PageSize: 50}) || got.ResourceMeta != meta {
			t.Fatalf("response=%+v err=%v", got, err)
		}
	}
}
