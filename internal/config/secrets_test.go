package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type trackingResolver struct {
	values map[SecretRef]string
	calls  []SecretRef
}

func (r *trackingResolver) Resolve(_ context.Context, ref SecretRef) ([]byte, error) {
	r.calls = append(r.calls, ref)
	value, ok := r.values[ref]
	if !ok {
		return nil, fmt.Errorf("missing test secret")
	}
	return []byte(value), nil
}

func TestConfigPersistsSecretReferencesOnly(t *testing.T) {
	cfg := Config{
		Database: Database{MySQL: MySQL{DSNRef: "env:DB_DSN"}},
		Auth: Auth{
			JWT:  JWT{SecretRef: "env:JWT_SECRET"},
			Seed: Seed{AdminPasswordRef: "env:ADMIN_PASSWORD"},
		},
		Providers: map[string]Provider{"provider_a": {SecretRef: "env:MODEL_KEY"}},
		SOAR:      SOAR{Integrations: Integrations{Email: Email{SMTPPasswordRef: "env:SMTP_PASSWORD"}}},
		Secrets: SecretReferences{
			MCPHeader: "env:MCP_HEADER",
			Effect:    "env:EFFECT_SECRET",
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(data)
	for _, ref := range []string{"env:DB_DSN", "env:JWT_SECRET", "env:ADMIN_PASSWORD", "env:MODEL_KEY", "env:MCP_HEADER", "env:SMTP_PASSWORD", "env:EFFECT_SECRET"} {
		if !strings.Contains(serialized, ref) {
			t.Fatalf("serialized config is missing reference %q", ref)
		}
	}
	for _, plaintext := range []string{"database-plaintext", "jwt-plaintext", "admin-plaintext", "model-plaintext", "mcp-plaintext", "smtp-plaintext", "effect-plaintext"} {
		if strings.Contains(serialized, plaintext) {
			t.Fatalf("serialized config contains plaintext secret category %q", plaintext)
		}
	}
}

func TestConfigRejectsLegacyPlaintextSecretFields(t *testing.T) {
	for name, fragment := range map[string]string{
		"database DSN": "database:\n  mysql:\n    dsn: plaintext-dsn\n",
		"JWT":          "auth:\n  jwt:\n    secret: plaintext-jwt\n",
		"admin":        "auth:\n  seed:\n    admin_password: plaintext-admin\n",
		"provider":     "providers:\n  provider_a:\n    api_key: plaintext-model-key\n",
		"SMTP":         "soar:\n  integrations:\n    email:\n      smtp_pass: plaintext-smtp\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Parse([]byte("app:\n  environment: test\n" + fragment))
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "plaintext") {
				t.Fatalf("Validate() error = %v, want plaintext field rejection", err)
			}
		})
	}
}

func TestSecretResolverIsSingleAndEphemeral(t *testing.T) {
	refs := []SecretRef{
		"env:JWT_SECRET",
		"env:ADMIN_PASSWORD",
		"env:MODEL_KEY",
		"env:MCP_HEADER",
		"env:SMTP_PASSWORD",
		"env:EFFECT_SECRET",
	}
	resolver := &trackingResolver{values: make(map[SecretRef]string, len(refs))}
	for _, ref := range refs {
		resolver.values[ref] = "short-lived-secret"
	}
	SetSecretResolver(resolver)

	var observed [][]byte
	for _, ref := range refs {
		if err := UseSecret(context.Background(), ref, func(value []byte) error {
			if string(value) != "short-lived-secret" {
				t.Fatalf("resolved value was not delivered to explicit call site")
			}
			observed = append(observed, value)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(resolver.calls) != len(refs) {
		t.Fatalf("resolver calls = %d, want %d", len(resolver.calls), len(refs))
	}
	for i, value := range observed {
		for _, b := range value {
			if b != 0 {
				t.Fatalf("temporary secret %d was not cleared after callback", i)
			}
		}
	}
}

func TestProviderCatalogRoutingRemainsSingleSource(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers["provider_b"] = Provider{
		SecretRef: "env:PROVIDER_B_KEY",
		Endpoints: map[string]string{DriverOpenAICompatibleChat: "https://provider-b.example/v1"},
	}
	cfg.ModelCatalog["provider_b/chat-model"] = Model{
		ModelID:      "chat-model",
		Driver:       DriverOpenAICompatibleChat,
		Capabilities: []string{"chat", "tool_calling"},
		Pricing:      Pricing{Currency: "CNY", Unit: "per_million_tokens"},
	}
	route := cfg.Routing.Chat["default"]
	route.Model = "provider_b/chat-model"
	provider, model, err := cfg.Resolve(route)
	if err != nil {
		t.Fatal(err)
	}
	if provider.SecretRef != "env:PROVIDER_B_KEY" || provider.Endpoints[DriverOpenAICompatibleChat] != "https://provider-b.example/v1" {
		t.Fatalf("route did not select provider_b: %#v", provider)
	}
	if model.ModelID != "chat-model" {
		t.Fatalf("vendor model id = %q, want same id isolated by provider-qualified ref", model.ModelID)
	}
}

func TestConfigLocalIsCompleteIgnoredReplacement(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	localPath := filepath.Join("manifest", "config", "config.local.yaml")
	command := exec.Command("git", "check-ignore", "--quiet", localPath)
	command.Dir = repoRoot
	if err := command.Run(); err != nil {
		t.Fatalf("config.local.yaml must remain ignored: %v", err)
	}
	tracked := exec.Command("git", "ls-files", "--error-unmatch", localPath)
	tracked.Dir = repoRoot
	if err := tracked.Run(); err == nil {
		t.Fatal("config.local.yaml must not be tracked")
	}
	actualLocalPath := filepath.Join(repoRoot, localPath)
	if _, err := os.Stat(actualLocalPath); err == nil {
		actualConfig, selectedActual, loadErr := LoadDirectory(filepath.Join(repoRoot, "manifest", "config"))
		if loadErr != nil {
			t.Fatalf("ignored complete local configuration failed the shared validator: %v", loadErr)
		}
		if filepath.Base(selectedActual) != localFileName {
			t.Fatalf("selected actual config = %s, want ignored local replacement", selectedActual)
		}
		if actualConfig.AgentRuntime.Enabled {
			t.Fatal("durable Agent gate must remain disabled in local replacement")
		}
		baseData, readErr := os.ReadFile(filepath.Join(repoRoot, "manifest", "config", baseFileName))
		if readErr != nil {
			t.Fatal(readErr)
		}
		localData, readErr := os.ReadFile(actualLocalPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var baseShape, localShape map[string]any
		if err := yaml.Unmarshal(baseData, &baseShape); err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(localData, &localShape); err != nil {
			t.Fatal(err)
		}
		if missing := missingConfigKeys(baseShape, localShape, ""); len(missing) > 0 {
			t.Fatalf("config.local.yaml is not a complete replacement; missing keys: %v", missing)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	dir := t.TempDir()
	base := strings.ReplaceAll(validConfig, "chat-model", "base-chat")
	local := strings.ReplaceAll(validConfig, "chat-model", "local-chat")
	if err := os.WriteFile(filepath.Join(dir, baseFileName), []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, localFileName), []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, selected, err := LoadDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(selected) != localFileName {
		t.Fatalf("selected = %s, want complete local replacement", selected)
	}
	if _, merged := cfg.ModelCatalog["provider_a/base-chat"]; merged {
		t.Fatal("base configuration was merged into local replacement")
	}
}

func missingConfigKeys(base, replacement map[string]any, prefix string) []string {
	var missing []string
	for key, baseValue := range base {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		replacementValue, ok := replacement[key]
		if !ok {
			missing = append(missing, path)
			continue
		}
		baseMap, baseIsMap := baseValue.(map[string]any)
		replacementMap, replacementIsMap := replacementValue.(map[string]any)
		if baseIsMap && replacementIsMap {
			missing = append(missing, missingConfigKeys(baseMap, replacementMap, path)...)
		}
	}
	return missing
}

func TestDriverRequiresOnlyItsEndpoint(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	embeddingProvider := cfg.Providers["provider_a"]
	embeddingProvider.SecretRef = "env:EMBEDDING_ONLY_KEY"
	embeddingProvider.Endpoints = map[string]string{
		DriverOpenAICompatibleEmbedding: embeddingProvider.Endpoints[DriverOpenAICompatibleEmbedding],
	}
	cfg.Providers["embedding_only"] = embeddingProvider
	embeddingModel := cfg.ModelCatalog["provider_a/embedding-model"]
	delete(cfg.ModelCatalog, "provider_a/embedding-model")
	cfg.ModelCatalog["embedding_only/embedding-model"] = embeddingModel
	embeddingRoute := cfg.Routing.Embedding["default"]
	embeddingRoute.Model = "embedding_only/embedding-model"
	cfg.Routing.Embedding["default"] = embeddingRoute

	provider := cfg.Providers["provider_a"]
	delete(provider.Endpoints, DriverOpenAICompatibleEmbedding)
	cfg.Providers["provider_a"] = provider
	if err := cfg.Validate(); err != nil {
		t.Fatalf("chat and rerank provider required an unused embedding endpoint: %v", err)
	}

	embeddingProvider = cfg.Providers["embedding_only"]
	delete(embeddingProvider.Endpoints, DriverOpenAICompatibleEmbedding)
	cfg.Providers["embedding_only"] = embeddingProvider
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), DriverOpenAICompatibleEmbedding) {
		t.Fatalf("Validate() error = %v, want embedding endpoint failure", err)
	}
}
