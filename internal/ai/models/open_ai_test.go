package models

import (
	"testing"

	appconfig "SentinelOps/internal/config"
)

func TestChatProfilesPassThinkingOption(t *testing.T) {
	cfg, err := appconfig.Parse([]byte(appconfigTestConfig))
	if err != nil {
		t.Fatal(err)
	}

	standard, err := resolveChatProfile(cfg, "default")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := resolveChatProfile(cfg, "reasoning")
	if err != nil {
		t.Fatal(err)
	}
	if got := standard.ExtraFields["enable_thinking"]; got != false {
		t.Fatalf("default enable_thinking = %#v, want false", got)
	}
	if got := reasoning.ExtraFields["enable_thinking"]; got != true {
		t.Fatalf("reasoning enable_thinking = %#v, want true", got)
	}
}

func TestChatProfileUsesProviderSelectedByRouting(t *testing.T) {
	cfg, err := appconfig.Parse([]byte(appconfigTestConfig))
	if err != nil {
		t.Fatal(err)
	}
	route := cfg.Routing.Chat["default"].Candidates[0]
	route.Model = "provider_b/chat"
	cfg.Routing.Chat["default"] = appconfig.ChatRoute{Candidates: []appconfig.Route{route}}

	profile, err := resolveChatProfile(cfg, "default")
	if err != nil {
		t.Fatal(err)
	}
	if profile.SecretRef != "env:PROVIDER_B_KEY" || profile.BaseURL != "https://provider-b.example/v1" || profile.Model != "provider-b-chat" {
		t.Fatalf("resolved profile = %#v, want provider_b configuration", profile)
	}
}

func TestChatProfileOmitsUnconfiguredThinkingOption(t *testing.T) {
	cfg, err := appconfig.Parse([]byte(appconfigTestConfig))
	if err != nil {
		t.Fatal(err)
	}
	route := cfg.Routing.Chat["default"].Candidates[0]
	route.Options = appconfig.RouteOptions{}
	cfg.Routing.Chat["default"] = appconfig.ChatRoute{Candidates: []appconfig.Route{route}}

	profile, err := resolveChatProfile(cfg, "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := profile.ExtraFields["enable_thinking"]; ok {
		t.Fatalf("unconfigured provider option was sent: %#v", profile.ExtraFields)
	}
}

const appconfigTestConfig = `
providers:
  provider_a:
    secret_ref: env:PROVIDER_A_KEY
    endpoints:
      openai_compatible_chat: https://example.com/v1
      openai_compatible_embedding: https://example.com/v1
      dashscope_compatible_rerank: https://example.com/v1
  provider_b:
    secret_ref: env:PROVIDER_B_KEY
    endpoints:
      openai_compatible_chat: https://provider-b.example/v1
model_catalog:
  provider_a/chat:
    model_id: vendor-chat
    driver: openai_compatible_chat
    capabilities: [chat, tool_calling]
    pricing: {revision: test-v1, currency: CNY, unit: per_million_tokens, input: 12, cached_input: 2.4, output: 36}
  provider_b/chat:
    model_id: provider-b-chat
    driver: openai_compatible_chat
    capabilities: [chat, tool_calling]
    pricing: {revision: test-v1, currency: CNY, unit: per_million_tokens, input: 1, output: 2}
  provider_a/embed:
    model_id: vendor-embed
    driver: openai_compatible_embedding
    capabilities: [embedding]
    dimension: 2048
    pricing: {revision: test-v1, currency: CNY, unit: per_million_tokens, input: 0.5}
  provider_a/rerank:
    model_id: vendor-rerank
    driver: dashscope_compatible_rerank
    capabilities: [rerank]
    pricing: {revision: test-v1, currency: CNY, unit: per_million_tokens, input: 0.5}
routing:
  chat:
    default:
      candidates:
        - {model: provider_a/chat, options: {enable_thinking: false}}
    reasoning:
      candidates:
        - {model: provider_a/chat, options: {enable_thinking: true}}
  embedding:
    default: {model: provider_a/embed}
  rerank:
    default: {model: provider_a/rerank}
`
