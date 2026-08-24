package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appconfig "SentinelOps/internal/config"
)

type fakeResolver map[appconfig.SecretRef]string

func (r fakeResolver) Resolve(_ context.Context, ref appconfig.SecretRef) ([]byte, error) {
	value, ok := r[ref]
	if !ok {
		return nil, fmt.Errorf("missing test secret")
	}
	return []byte(value), nil
}

func validBootstrapConfig(environment string) *appconfig.Config {
	return &appconfig.Config{
		App: appconfig.App{Environment: environment},
		Database: appconfig.Database{MySQL: appconfig.MySQL{
			DSNRef: "env:TEST_DATABASE_DSN",
		}},
		Auth: appconfig.Auth{
			JWT:  appconfig.JWT{Enabled: true, SecretRef: "env:TEST_JWT_SECRET"},
			Seed: appconfig.Seed{AdminPasswordRef: "env:TEST_ADMIN_PASSWORD"},
		},
		Providers: map[string]appconfig.Provider{
			"provider_a": {
				SecretRef: "env:TEST_MODEL_KEY",
				Endpoints: map[string]string{
					appconfig.DriverOpenAICompatibleChat:      "https://chat.example/v1",
					appconfig.DriverOpenAICompatibleEmbedding: "https://embedding.example/v1",
					appconfig.DriverDashScopeCompatibleRerank: "https://rerank.example/v1",
				},
			},
		},
		ModelCatalog: map[string]appconfig.Model{
			"provider_a/chat": {
				ModelID: "same-vendor-id", Driver: appconfig.DriverOpenAICompatibleChat,
				Capabilities: []string{"chat", "tool_calling"},
				Pricing:      appconfig.Pricing{Revision: "test-v1", Currency: "CNY", Unit: "per_million_tokens"},
			},
			"provider_a/embed": {
				ModelID: "embed", Driver: appconfig.DriverOpenAICompatibleEmbedding,
				Capabilities: []string{"embedding"}, Dimension: 2048,
				Pricing: appconfig.Pricing{Revision: "test-v1", Currency: "CNY", Unit: "per_million_tokens"},
			},
			"provider_a/rerank": {
				ModelID: "rerank", Driver: appconfig.DriverDashScopeCompatibleRerank,
				Capabilities: []string{"rerank"},
				Pricing:      appconfig.Pricing{Revision: "test-v1", Currency: "CNY", Unit: "per_million_tokens"},
			},
		},
		Routing: appconfig.Routing{
			Chat: map[string]appconfig.Route{
				"default":   {Model: "provider_a/chat"},
				"reasoning": {Model: "provider_a/chat"},
			},
			Embedding: map[string]appconfig.Route{"default": {Model: "provider_a/embed"}},
			Rerank:    map[string]appconfig.Route{"default": {Model: "provider_a/rerank"}},
		},
	}
}

func validResolver() fakeResolver {
	return fakeResolver{
		"env:TEST_DATABASE_DSN":   "test-database-dsn",
		"env:TEST_JWT_SECRET":     "test-jwt-secret-with-sufficient-entropy",
		"env:TEST_ADMIN_PASSWORD": "test-admin-password",
		"env:TEST_MODEL_KEY":      "test-model-key",
	}
}

func recordingDependencies(cfg *appconfig.Config, calls *[]string) dependencies {
	record := func(name string) { *calls = append(*calls, name) }
	return dependencies{
		loadConfig: func(string) (*appconfig.Config, string, error) {
			record("config")
			return cfg, "test-config.yaml", nil
		},
		initDatabase: func(context.Context, []byte) error { record("database"); return nil },
		initAuth:     func([]byte) error { record("auth"); return nil },
		seedAdmin:    func(context.Context, []byte) error { record("admin"); return nil },
		warmUp:       func(context.Context) error { record("warmup"); return nil },
		bindAPI:      func(context.Context) error { record("api"); return nil },
		startWorker:  func(context.Context) error { record("worker"); return nil },
		serveAPI:     func(context.Context) error { record("serve"); return nil },
		waitWorker:   func(context.Context) error { record("wait"); return nil },
	}
}

func TestBootstrapAPIDoesNotStartWorker(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("production")
	err := run(context.Background(), Options{Role: RoleAPI, Resolver: validResolver()}, recordingDependencies(cfg, &calls))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, ",")
	if !strings.Contains(joined, "api") || strings.Contains(joined, "worker") || strings.Contains(joined, "wait") {
		t.Fatalf("calls = %v, want API without worker", calls)
	}
}

func TestBootstrapParsesExplicitRoles(t *testing.T) {
	for _, want := range []Role{RoleAPI, RoleWorker, RoleAll} {
		got, err := ParseRole([]string{string(want)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("ParseRole(%q) = %q, want %q", want, got, want)
		}
	}
	got, err := ParseRole(nil, func(name string) (string, bool) {
		if name != "SENTINELOPS_ROLE" {
			t.Fatalf("lookup name = %q", name)
		}
		return "worker", true
	})
	if err != nil || got != RoleWorker {
		t.Fatalf("environment role = %q, %v; want worker", got, err)
	}
}

func TestBootstrapWorkerDoesNotBindHTTP(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("production")
	err := run(context.Background(), Options{Role: RoleWorker, Resolver: validResolver()}, recordingDependencies(cfg, &calls))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, ",")
	if !strings.Contains(joined, "worker") || strings.Contains(joined, "api") || strings.Contains(joined, "serve") {
		t.Fatalf("calls = %v, want worker without HTTP", calls)
	}
}

func TestBootstrapAllIsDevelopmentOnly(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("production")
	err := run(context.Background(), Options{Role: RoleAll, Resolver: validResolver()}, recordingDependencies(cfg, &calls))
	if err == nil || !strings.Contains(err.Error(), "development") {
		t.Fatalf("run() error = %v, want all role rejected outside development", err)
	}

	calls = nil
	cfg.App.Environment = "development"
	if err := run(context.Background(), Options{Role: RoleAll, Resolver: validResolver()}, recordingDependencies(cfg, &calls)); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, ",")
	if !strings.Contains(joined, "api") || !strings.Contains(joined, "worker") {
		t.Fatalf("calls = %v, want both API and worker", calls)
	}
}

func TestProductionRejectsMissingDatabase(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("production")
	cfg.Database.MySQL.DSNRef = ""
	err := run(context.Background(), Options{Role: RoleAPI, Resolver: validResolver()}, recordingDependencies(cfg, &calls))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "database") {
		t.Fatalf("run() error = %v, want missing production database", err)
	}
	if strings.Contains(strings.Join(calls, ","), "api") {
		t.Fatalf("API started after database validation failed: %v", calls)
	}
}

func TestProductionRejectsUnavailableDatabase(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("production")
	deps := recordingDependencies(cfg, &calls)
	deps.initDatabase = func(context.Context, []byte) error {
		return fmt.Errorf("test database unavailable")
	}
	err := run(context.Background(), Options{Role: RoleWorker, Resolver: validResolver()}, deps)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "database") {
		t.Fatalf("run() error = %v, want unavailable production database", err)
	}
	if strings.Contains(strings.Join(calls, ","), "worker") {
		t.Fatalf("worker started after database initialization failed: %v", calls)
	}
}

func TestProductionRejectsDefaultSecrets(t *testing.T) {
	for name, ref := range map[string]appconfig.SecretRef{
		"jwt":      "env:TEST_JWT_SECRET",
		"admin":    "env:TEST_ADMIN_PASSWORD",
		"database": "env:TEST_DATABASE_DSN",
	} {
		t.Run(name, func(t *testing.T) {
			var calls []string
			resolver := validResolver()
			resolver[ref] = map[string]string{
				"jwt": "change-me-in-production", "admin": "admin123", "database": "root:sentinel123@tcp(mysql:3306)/sentinelops",
			}[name]
			err := run(context.Background(), Options{Role: RoleAPI, Resolver: resolver}, recordingDependencies(validBootstrapConfig("production"), &calls))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "default") {
				t.Fatalf("run() error = %v, want default secret rejection", err)
			}
		})
	}
}

func TestProductionRejectsEmptySecrets(t *testing.T) {
	var calls []string
	resolver := validResolver()
	resolver["env:TEST_MODEL_KEY"] = ""
	err := run(context.Background(), Options{Role: RoleWorker, Resolver: resolver}, recordingDependencies(validBootstrapConfig("production"), &calls))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Fatalf("run() error = %v, want empty production model secret rejection", err)
	}
}

func TestBootstrapLoadsConfigBeforeRetrievalWarmUp(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("production")
	if err := run(context.Background(), Options{Role: RoleAPI, Resolver: validResolver()}, recordingDependencies(cfg, &calls)); err != nil {
		t.Fatal(err)
	}
	configIndex, warmUpIndex := -1, -1
	for i, call := range calls {
		switch call {
		case "config":
			configIndex = i
		case "warmup":
			warmUpIndex = i
		}
	}
	if configIndex < 0 || warmUpIndex < 0 || configIndex >= warmUpIndex {
		t.Fatalf("calls = %v, want config before warmup", calls)
	}
}

func TestConfigDurableAgentGateRemainsClosed(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("development")
	cfg.AgentRuntime.Enabled = true
	err := run(context.Background(), Options{Role: RoleAPI, Resolver: validResolver()}, recordingDependencies(cfg, &calls))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "durable agent gate") {
		t.Fatalf("run() error = %v, want durable Agent gate rejection", err)
	}
	if strings.Contains(strings.Join(calls, ","), "api") {
		t.Fatalf("API started with durable Agent gate enabled: %v", calls)
	}
}
