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

const appconfigTestConfig = `
providers:
  aliyun_bailian:
    api_key: test-key
    endpoints:
      openai_compatible: https://example.com/v1
      dashscope: https://example.com/v1
      dashscope_rerank: https://example.com/v1
model_catalog:
  aliyun_bailian/chat:
    model_id: vendor-chat
    driver: openai_compatible
    capabilities: [chat, tool_calling]
    pricing: {currency: CNY, unit: per_million_tokens, input: 12, cached_input: 2.4, output: 36}
  aliyun_bailian/embed:
    model_id: vendor-embed
    driver: dashscope
    capabilities: [embedding]
    dimension: 2048
    pricing: {currency: CNY, unit: per_million_tokens, input: 0.5}
  aliyun_bailian/rerank:
    model_id: vendor-rerank
    driver: dashscope_rerank
    capabilities: [rerank]
    pricing: {currency: CNY, unit: per_million_tokens, input: 0.5}
routing:
  chat:
    default: {model: aliyun_bailian/chat, options: {enable_thinking: false}}
    reasoning: {model: aliyun_bailian/chat, options: {enable_thinking: true}}
  embedding:
    default: {model: aliyun_bailian/embed}
  rerank:
    default: {model: aliyun_bailian/rerank}
`
