package models

import (
	"testing"

	appconfig "SentinelOps/internal/config"
)

func TestResolveChatCandidatesUsesCatalogRefsAndIsolatesOptions(t *testing.T) {
	cfg, err := appconfig.Parse([]byte(appconfigCandidatesTestConfig))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := resolveChatCandidates(cfg, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2", len(candidates))
	}
	if candidates[0].CatalogRef != "provider_a/chat" || candidates[1].CatalogRef != "provider_b/chat" {
		t.Fatalf("candidate order = %#v", candidates)
	}
	if got := candidates[0].ExtraFields["enable_thinking"]; got != false {
		t.Fatalf("first candidate enable_thinking = %#v, want false", got)
	}
	if _, ok := candidates[1].ExtraFields["enable_thinking"]; ok {
		t.Fatalf("second candidate inherited first options: %#v", candidates[1].ExtraFields)
	}
	if candidates[0].Model != candidates[1].Model {
		t.Fatalf("vendor model IDs differ: %q != %q; fixture must prove provider-qualified isolation", candidates[0].Model, candidates[1].Model)
	}
	if candidates[0].Provider == candidates[1].Provider {
		t.Fatalf("provider identity collapsed: %#v", candidates)
	}
}

const appconfigCandidatesTestConfig = `
providers:
  provider_a:
    secret_ref: env:PROVIDER_A_KEY
    endpoints: {openai_compatible_chat: https://provider-a.example/v1}
  provider_b:
    secret_ref: env:PROVIDER_B_KEY
    endpoints: {openai_compatible_chat: https://provider-b.example/v1}
model_catalog:
  provider_a/chat:
    model_id: shared-vendor-model
    driver: openai_compatible_chat
    capabilities: [chat, tool_calling]
    pricing: {revision: a-v1, currency: CNY, unit: per_million_tokens, input: 1, output: 2}
  provider_b/chat:
    model_id: shared-vendor-model
    driver: openai_compatible_chat
    capabilities: [chat, tool_calling]
    pricing: {revision: b-v1, currency: CNY, unit: per_million_tokens, input: 3, output: 4}
routing:
  chat:
    default:
      candidates:
        - {model: provider_a/chat, options: {enable_thinking: false}}
        - {model: provider_b/chat}
    reasoning:
      candidates:
        - {model: provider_a/chat, options: {enable_thinking: true}}
`
