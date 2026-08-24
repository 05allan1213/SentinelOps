package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validConfig = `
providers:
  aliyun_bailian:
    api_key: ""
    endpoints:
      openai_compatible: https://chat.example/v1
      dashscope: https://embedding.example/v1
      dashscope_rerank: https://rerank.example/v1
model_catalog:
  aliyun_bailian/chat-model:
    model_id: chat-model
    driver: openai_compatible
    capabilities: [chat, tool_calling]
    pricing:
      currency: CNY
      unit: per_million_tokens
      input: 12
      cached_input: 2.4
      output: 36
  aliyun_bailian/embedding-model:
    model_id: embedding-model
    driver: dashscope
    capabilities: [embedding]
    dimension: 2048
    pricing: {currency: CNY, unit: per_million_tokens, input: 0.5}
  aliyun_bailian/rerank-model:
    model_id: rerank-model
    driver: dashscope_rerank
    capabilities: [rerank]
    pricing: {currency: CNY, unit: per_million_tokens, input: 0.5}
routing:
  chat:
    default:
      model: aliyun_bailian/chat-model
      options: {enable_thinking: false}
    reasoning:
      model: aliyun_bailian/chat-model
      options: {enable_thinking: true}
  embedding:
    default: {model: aliyun_bailian/embedding-model}
  rerank:
    default:
      model: aliyun_bailian/rerank-model
      options: {instruct: rank these documents}
`

func TestLoadDirectoryPrefersCompleteLocalConfigWithoutMerging(t *testing.T) {
	dir := t.TempDir()
	base := strings.ReplaceAll(validConfig, "chat-model", "base-chat")
	local := strings.ReplaceAll(validConfig, "chat-model", "local-chat")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.local.yaml"), []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, selected, err := LoadDirectory(dir)
	if err != nil {
		t.Fatalf("LoadDirectory() error = %v", err)
	}
	if filepath.Base(selected) != "config.local.yaml" {
		t.Fatalf("selected = %q, want config.local.yaml", selected)
	}
	if _, ok := cfg.ModelCatalog["aliyun_bailian/local-chat"]; !ok {
		t.Fatal("local catalog was not loaded")
	}
	if _, ok := cfg.ModelCatalog["aliyun_bailian/base-chat"]; ok {
		t.Fatal("base config was merged into complete local config")
	}
}

func TestLoadDirectoryFallsBackToBaseConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(validConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	_, selected, err := LoadDirectory(dir)
	if err != nil {
		t.Fatalf("LoadDirectory() error = %v", err)
	}
	if filepath.Base(selected) != "config.yaml" {
		t.Fatalf("selected = %q, want config.yaml", selected)
	}
}

func TestValidateRejectsInvalidModelContracts(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"missing provider", func(c *Config) { delete(c.Providers, "aliyun_bailian") }, "provider"},
		{"unknown driver", func(c *Config) {
			m := c.ModelCatalog["aliyun_bailian/chat-model"]
			m.Driver = "mystery"
			c.ModelCatalog["aliyun_bailian/chat-model"] = m
		}, "driver"},
		{"missing capability", func(c *Config) {
			m := c.ModelCatalog["aliyun_bailian/chat-model"]
			m.Capabilities = []string{"chat"}
			c.ModelCatalog["aliyun_bailian/chat-model"] = m
		}, "tool_calling"},
		{"wrong embedding dimension", func(c *Config) {
			m := c.ModelCatalog["aliyun_bailian/embedding-model"]
			m.Dimension = 1024
			c.ModelCatalog["aliyun_bailian/embedding-model"] = m
		}, "2048"},
		{"bad route", func(c *Config) {
			r := c.Routing.Chat["default"]
			r.Model = "aliyun_bailian/missing"
			c.Routing.Chat["default"] = r
		}, "routing"},
		{"missing endpoint", func(c *Config) {
			p := c.Providers["aliyun_bailian"]
			p.Endpoints.OpenAICompatible = ""
			c.Providers["aliyun_bailian"] = p
		}, "endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(validConfig))
			if err != nil {
				t.Fatal(err)
			}
			tt.edit(cfg)
			err = cfg.Validate()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestTrackedConfigurationsAreCompleteAndValid(t *testing.T) {
	for _, name := range []string{"config.yaml", "config.docker.yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "manifest", "config", name))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
