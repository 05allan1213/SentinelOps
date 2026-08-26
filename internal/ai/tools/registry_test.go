package tools

import (
	"context"
	"strings"
	"testing"

	"SentinelOps/internal/ai/policy"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type registryTestTool struct {
	info *schema.ToolInfo
}

func (t *registryTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func TestGetManyRequiredFailsFast(t *testing.T) {
	names := policy.DefaultRegistryToolNames()
	got, err := GetManyRequired(names)
	if err != nil {
		t.Fatalf("合法 durable inventory: %v", err)
	}
	if len(got) != len(names) {
		t.Fatalf("返回数 = %d, want %d", len(got), len(names))
	}
	registered := All()
	if len(registered) != len(names) {
		t.Fatalf("默认 Registry 数量 = %d, Catalog 数量 = %d", len(registered), len(names))
	}
	for _, name := range policy.RequiredDurableToolNames() {
		if _, ok := registered[name]; !ok {
			t.Errorf("durable inventory Tool %q 未来自唯一 Registry", name)
		}
	}

	t.Run("missing", func(t *testing.T) {
		withRegistryMutation(t, func() {
			delete(registry, "query_events")
			if _, err := GetManyRequired([]string{"query_events"}); err == nil || !strings.Contains(err.Error(), "missing") {
				t.Fatalf("缺失 Tool 应 fail-fast: %v", err)
			}
		})
	})

	t.Run("duplicate request", func(t *testing.T) {
		if _, err := GetManyRequired([]string{"query_events", "query_events"}); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("重名请求应 fail-fast: %v", err)
		}
	})

	t.Run("duplicate registration", func(t *testing.T) {
		withRegistryMutation(t, func() {
			Register("query_events", registry["query_events"])
			if _, err := GetManyRequired([]string{"query_events"}); err == nil || !strings.Contains(err.Error(), "registered") {
				t.Fatalf("重名注册应 fail-fast: %v", err)
			}
		})
	})

	t.Run("ToolInfo name mismatch", func(t *testing.T) {
		withRegistryMutation(t, func() {
			original := registry["query_events"]
			info, err := original.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			copyInfo := *info
			copyInfo.Name = "other_name"
			registry["query_events"] = &registryTestTool{info: &copyInfo}
			if _, err = GetManyRequired([]string{"query_events"}); err == nil || !strings.Contains(err.Error(), "ToolInfo.Name") {
				t.Fatalf("ToolInfo.Name 漂移应 fail-fast: %v", err)
			}
		})
	})

	t.Run("schema hash mismatch", func(t *testing.T) {
		withRegistryMutation(t, func() {
			original := registry["query_events"]
			info, err := original.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			copyInfo := *info
			copyInfo.ParamsOneOf = schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"drifted": {Type: schema.String, Required: true},
			})
			registry["query_events"] = &registryTestTool{info: &copyInfo}
			if _, err = GetManyRequired([]string{"query_events"}); err == nil || !strings.Contains(err.Error(), "schema hash") {
				t.Fatalf("schema 漂移应 fail-fast: %v", err)
			}
		})
	})
}

func TestQueryDatabaseIsNotRegisteredForDurableUse(t *testing.T) {
	if _, exists := All()["query_database"]; exists {
		t.Fatal("query_database 不得在默认 Tool Registry 注册")
	}
	if _, err := GetManyRequired([]string{"query_database"}); err == nil {
		t.Fatal("query_database 不得通过普通 GetManyRequired 解析")
	}
}

func TestGetManyRequiredIsolatesToolInfoMutation(t *testing.T) {
	entry, err := policy.LookupCatalog("trigger_ops")
	if err != nil {
		t.Fatal(err)
	}
	got, err := GetManyRequired([]string{"trigger_ops"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := got[0].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	js.Required = append(js.Required, "framework_mutation_probe")

	canonical, err := registry["trigger_ops"].Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := policy.ToolSchemaHash(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if hash != entry.SchemaHash {
		t.Fatalf("canonical ToolInfo changed through durable result: got %s want %s", hash, entry.SchemaHash)
	}
}

func withRegistryMutation(t *testing.T, mutate func()) {
	t.Helper()
	mu.Lock()
	registryBefore := cloneRegistry(registry)
	registrationsBefore := cloneRegistrations(registrations)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		registry = registryBefore
		registrations = registrationsBefore
		mu.Unlock()
	})
	mutate()
}

func cloneRegistry(source map[string]tool.BaseTool) map[string]tool.BaseTool {
	clone := make(map[string]tool.BaseTool, len(source))
	for name, instance := range source {
		clone[name] = instance
	}
	return clone
}

func cloneRegistrations(source map[string]int) map[string]int {
	clone := make(map[string]int, len(source))
	for name, count := range source {
		clone[name] = count
	}
	return clone
}
