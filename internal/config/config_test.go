package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validConfig = `
app:
  environment: test
providers:
  provider_a:
    secret_ref: env:PROVIDER_A_KEY
    endpoints:
      openai_compatible_chat: https://chat.example/v1
      openai_compatible_embedding: https://embedding.example/v1
      dashscope_compatible_rerank: https://rerank.example/v1
model_catalog:
  provider_a/chat-model:
    model_id: chat-model
    driver: openai_compatible_chat
    capabilities: [chat, tool_calling]
    pricing:
      revision: "2026-08-24"
      currency: CNY
      unit: per_million_tokens
      input: 12
      cached_input: 2.4
      output: 36
  provider_a/embedding-model:
    model_id: embedding-model
    driver: openai_compatible_embedding
    capabilities: [embedding]
    dimension: 2048
    pricing: {revision: "2026-08-24", currency: CNY, unit: per_million_tokens, input: 0.5}
  provider_a/rerank-model:
    model_id: rerank-model
    driver: dashscope_compatible_rerank
    capabilities: [rerank]
    pricing: {revision: "2026-08-24", currency: CNY, unit: per_million_tokens, input: 0.5}
routing:
  chat:
    default:
      model: provider_a/chat-model
      options: {enable_thinking: false}
    reasoning:
      model: provider_a/chat-model
      options: {enable_thinking: true}
  embedding:
    default: {model: provider_a/embedding-model}
  rerank:
    default:
      model: provider_a/rerank-model
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
	if _, ok := cfg.ModelCatalog["provider_a/local-chat"]; !ok {
		t.Fatal("local catalog was not loaded")
	}
	if _, ok := cfg.ModelCatalog["provider_a/base-chat"]; ok {
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
		{"missing provider", func(c *Config) { delete(c.Providers, "provider_a") }, "provider"},
		{"unknown driver", func(c *Config) {
			m := c.ModelCatalog["provider_a/chat-model"]
			m.Driver = "mystery"
			c.ModelCatalog["provider_a/chat-model"] = m
		}, "driver"},
		{"unknown endpoint driver", func(c *Config) {
			p := c.Providers["provider_a"]
			p.Endpoints["provider_specific"] = "https://provider.example/v1"
			c.Providers["provider_a"] = p
		}, "endpoint driver"},
		{"missing capability", func(c *Config) {
			m := c.ModelCatalog["provider_a/chat-model"]
			m.Capabilities = []string{"chat"}
			c.ModelCatalog["provider_a/chat-model"] = m
		}, "tool_calling"},
		{"wrong embedding dimension", func(c *Config) {
			m := c.ModelCatalog["provider_a/embedding-model"]
			m.Dimension = 1024
			c.ModelCatalog["provider_a/embedding-model"] = m
		}, "2048"},
		{"missing pricing revision", func(c *Config) {
			m := c.ModelCatalog["provider_a/chat-model"]
			m.Pricing.Revision = ""
			c.ModelCatalog["provider_a/chat-model"] = m
		}, "pricing revision"},
		{"bad route", func(c *Config) {
			r := c.Routing.Chat["default"]
			r.Model = "provider_a/missing"
			c.Routing.Chat["default"] = r
		}, "routing"},
		{"missing chat endpoint", func(c *Config) {
			p := c.Providers["provider_a"]
			p.Endpoints[DriverOpenAICompatibleChat] = ""
			c.Providers["provider_a"] = p
		}, DriverOpenAICompatibleChat},
		{"missing embedding endpoint", func(c *Config) {
			p := c.Providers["provider_a"]
			p.Endpoints[DriverOpenAICompatibleEmbedding] = ""
			c.Providers["provider_a"] = p
		}, DriverOpenAICompatibleEmbedding},
		{"missing rerank endpoint", func(c *Config) {
			p := c.Providers["provider_a"]
			p.Endpoints[DriverDashScopeCompatibleRerank] = ""
			c.Providers["provider_a"] = p
		}, DriverDashScopeCompatibleRerank},
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

func TestResolveUsesProviderNamedByModelReference(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers["provider_b"] = Provider{
		SecretRef: "env:PROVIDER_B_KEY",
		Endpoints: map[string]string{DriverOpenAICompatibleChat: "https://provider-b.example/v1"},
	}
	cfg.ModelCatalog["provider_b/chat-model"] = Model{
		ModelID:      "provider-b-chat",
		Driver:       DriverOpenAICompatibleChat,
		Capabilities: []string{"chat", "tool_calling"},
		Pricing:      Pricing{Revision: "test-v1", Currency: "CNY", Unit: "per_million_tokens", Input: 1, Output: 2},
	}
	route := cfg.Routing.Chat["default"]
	route.Model = "provider_b/chat-model"
	cfg.Routing.Chat["default"] = route

	provider, model, err := cfg.Resolve(route)
	if err != nil {
		t.Fatal(err)
	}
	if provider.SecretRef != "env:PROVIDER_B_KEY" || provider.Endpoints[DriverOpenAICompatibleChat] != "https://provider-b.example/v1" {
		t.Fatalf("resolved provider = %#v, want provider_b configuration", provider)
	}
	if model.ModelID != "provider-b-chat" {
		t.Fatalf("resolved model = %q, want provider-b-chat", model.ModelID)
	}
}

func TestValidateRequiresOnlyEndpointsUsedByEachProvider(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers["chat_only"] = Provider{
		SecretRef: "env:CHAT_ONLY_KEY",
		Endpoints: map[string]string{DriverOpenAICompatibleChat: "https://chat-only.example/v1"},
	}
	cfg.ModelCatalog["chat_only/chat-model"] = Model{
		ModelID:      "chat-only-model",
		Driver:       DriverOpenAICompatibleChat,
		Capabilities: []string{"chat", "tool_calling"},
		Pricing:      Pricing{Revision: "test-v1", Currency: "CNY", Unit: "per_million_tokens"},
	}
	route := cfg.Routing.Chat["default"]
	route.Model = "chat_only/chat-model"
	cfg.Routing.Chat["default"] = route

	if err := cfg.Validate(); err != nil {
		t.Fatalf("chat-only provider rejected: %v", err)
	}
	provider := cfg.Providers["chat_only"]
	provider.Endpoints[DriverOpenAICompatibleChat] = ""
	cfg.Providers["chat_only"] = provider
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), DriverOpenAICompatibleChat) {
		t.Fatalf("Validate() error = %v, want missing chat endpoint", err)
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
			if cfg.AgentRuntime.Enabled {
				t.Fatal("durable Agent gate must remain disabled in tracked configurations")
			}
		})
	}
}
