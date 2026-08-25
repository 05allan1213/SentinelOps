package config

import (
	"strings"
	"testing"
)

func TestChatRoutingCandidatesPreserveOrderAndOptions(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	first := cfg.Routing.Chat["default"].Candidates[0].Model
	firstOptions := cfg.Routing.Chat["default"].Candidates[0].Options
	cfg.Providers["provider_b"] = Provider{SecretRef: "env:PROVIDER_B_KEY", Endpoints: map[string]string{DriverOpenAICompatibleChat: "https://provider-b.example/v1"}}
	cfg.ModelCatalog["provider_b/chat-model"] = Model{ModelID: "chat-model", Driver: DriverOpenAICompatibleChat, Capabilities: []string{"chat", "tool_calling"}, Pricing: Pricing{Revision: "test-v1", Currency: "CNY", Unit: "per_million_tokens"}}
	cfg.Routing.Chat["default"] = ChatRoute{Candidates: []Route{
		{Model: first, Options: firstOptions},
		{Model: "provider_b/chat-model"},
	}}
	if err = cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	profile := cfg.Routing.Chat["default"]
	if len(profile.Candidates) != 2 {
		t.Fatalf("default candidates = %d, want 2", len(profile.Candidates))
	}
	if profile.Candidates[0].Model != "provider_a/chat-model" || profile.Candidates[1].Model != "provider_b/chat-model" {
		t.Fatalf("default candidates = %#v, want provider_a then provider_b", profile.Candidates)
	}
	if got := profile.Candidates[0].Options.EnableThinking; got == nil || *got {
		t.Fatalf("first candidate enable_thinking = %#v, want false", got)
	}
	if got := profile.Candidates[1].Options.EnableThinking; got != nil {
		t.Fatalf("second candidate inherited route options: %#v", got)
	}
}

func TestConfigRejectsInvalidChatCandidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "missing candidate", mutate: func(c *Config) { c.Routing.Chat["default"] = ChatRoute{} }, want: "at least one candidate"},
		{name: "duplicate candidate", mutate: func(c *Config) {
			candidate := Route{Model: "provider_a/chat-model"}
			c.Routing.Chat["default"] = ChatRoute{Candidates: []Route{candidate, candidate}}
		}, want: "duplicate candidate"},
		{name: "wrong capability", mutate: func(c *Config) {
			c.Routing.Chat["default"] = ChatRoute{Candidates: []Route{{Model: "provider_a/embedding-model"}}}
		}, want: "lacks chat capability"},
		{name: "missing endpoint", mutate: func(c *Config) {
			c.Providers["provider_b"] = Provider{SecretRef: "env:PROVIDER_B_KEY", Endpoints: map[string]string{}}
			c.ModelCatalog["provider_b/chat-model"] = Model{ModelID: "shared", Driver: DriverOpenAICompatibleChat, Capabilities: []string{"chat", "tool_calling"}, Pricing: Pricing{Revision: "test-v1", Currency: "CNY", Unit: "per_million_tokens"}}
			c.Routing.Chat["default"] = ChatRoute{Candidates: []Route{{Model: "provider_b/chat-model"}}}
		}, want: "requires provider provider_b endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(validConfig))
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(cfg)
			if err = cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
