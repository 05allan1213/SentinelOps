package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/agent/skill_pipeline"
	airuntime "SentinelOps/internal/ai/runtime"
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
		ModelReliability: appconfig.ModelReliability{
			Retry:   appconfig.ModelRetry{MaxRetries: 1, BaseBackoffMS: 1},
			Breaker: appconfig.ModelBreaker{FailureThreshold: 3, OpenTimeoutMS: 30000},
			Limiter: appconfig.ModelLimiter{QPS: 1000, Burst: 1000},
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
			Chat: map[string]appconfig.ChatRoute{
				"default":   {Candidates: []appconfig.Route{{Model: "provider_a/chat"}}},
				"reasoning": {Candidates: []appconfig.Route{{Model: "provider_a/chat"}}},
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

func TestLangfuseShutdownUsesConfiguredDeadline(t *testing.T) {
	var deadline time.Time
	var calls []string
	deps := recordingDependencies(validBootstrapConfig("production"), &calls)
	deps.shutdownRuntime = func(ctx context.Context) error {
		var ok bool
		deadline, ok = ctx.Deadline()
		if !ok {
			return fmt.Errorf("shutdown context has no deadline")
		}
		return nil
	}
	deps.initRuntime = func(context.Context) error { return nil }
	deps.waitWorker = func(context.Context) error { return nil }
	config := validBootstrapConfig("production")
	config.Observability.Langfuse.ShutdownTimeoutMS = 37
	deps.loadConfig = func(string) (*appconfig.Config, string, error) { return config, "test-config.yaml", nil }
	started := time.Now()
	if err := run(context.Background(), Options{Role: RoleWorker, Resolver: validResolver()}, deps); err != nil {
		t.Fatal(err)
	}
	if deadline.IsZero() || deadline.Before(started.Add(30*time.Millisecond)) || deadline.After(started.Add(500*time.Millisecond)) {
		t.Fatalf("shutdown deadline=%v, want about 37ms after start %v", deadline, started)
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

func TestProductionAllowsStaticRuntimeCapsBehindDynamicGate(t *testing.T) {
	var calls []string
	t.Setenv(airuntime.RuntimeVersionEnv, "sha256:"+strings.Repeat("a", 64))
	cfg := validBootstrapConfig("production")
	cfg.AgentRuntime.Enabled = true
	cfg.AgentRuntime.AcceptNewRuns = true
	err := run(context.Background(), Options{Role: RoleAPI, Resolver: validResolver()}, recordingDependencies(cfg, &calls))
	if err != nil {
		t.Fatalf("production static Runtime caps were rejected: %v", err)
	}
	if !strings.Contains(strings.Join(calls, ","), "api") {
		t.Fatalf("API did not start behind dynamic Runtime Gate: %v", calls)
	}
}

func TestProductionRejectsAmbiguousRuntimeVersion(t *testing.T) {
	var calls []string
	t.Setenv(airuntime.RuntimeVersionEnv, "development")
	cfg := validBootstrapConfig("production")
	cfg.AgentRuntime.Enabled = true
	err := run(context.Background(), Options{Role: RoleAPI, Resolver: validResolver()}, recordingDependencies(cfg, &calls))
	if err == nil || !strings.Contains(err.Error(), "immutable Git SHA or sha256 digest") {
		t.Fatalf("run() error = %v, want ambiguous runtime version rejection", err)
	}
}

func TestRollbackCompatibilityUsesFrozenGateAndCurrentSkillContent(t *testing.T) {
	t.Setenv(airuntime.RuntimeVersionEnv, strings.Repeat("a", 40))
	baseDir := t.TempDir()
	skillDir := filepath.Join(baseDir, "p42-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	writeSkill := func(body string) {
		t.Helper()
		content := "---\nname: p42-skill\ndescription: P42 compatibility fixture.\n---\n\n" + body + "\n"
		if err := os.WriteFile(skillPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("first content")

	config := validBootstrapConfig("test")
	config.Skill = appconfig.SkillConfig{Enabled: true, BaseDir: baseDir, MaxBytes: 4096}
	skills, err := skill_pipeline.BuildConfiguredSkillSnapshots(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := airuntime.BuildDurableRuntimeSnapshotWithSkills(config, skills)
	if err != nil {
		t.Fatal(err)
	}

	openDynamic := func(context.Context, []string) (map[string]string, error) {
		values := make(map[string]string, len(airuntime.CanonicalGateKeys()))
		for _, key := range airuntime.CanonicalGateKeys() {
			values[key] = "true"
		}
		return values, nil
	}
	closedStaticValues := make(map[string]bool, len(airuntime.CanonicalGateKeys()))
	for _, key := range airuntime.CanonicalGateKeys() {
		closedStaticValues[key] = true
	}
	closedStaticValues[airuntime.GateSkillEnabled] = false
	closedStatic, err := airuntime.NewGateVector(closedStaticValues)
	if err != nil {
		t.Fatal(err)
	}
	closedEvaluator, err := airuntime.NewGateEvaluator(closedStatic, openDynamic)
	if err != nil {
		t.Fatal(err)
	}
	config.Skill.BaseDir = filepath.Join(baseDir, "missing-skill-dir")
	unchanged, err := expectedFrozenGateCompatibilityWithEvaluator(context.Background(), config, closedEvaluator, frozen)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != frozen.CompatibilityHash() {
		t.Fatalf("current static Skill cap rewrote frozen compatibility: got=%s want=%s", unchanged, frozen.CompatibilityHash())
	}

	writeSkill("changed content")
	config.Skill.BaseDir = skillDir
	openStaticValues := make(map[string]bool, len(airuntime.CanonicalGateKeys()))
	for _, key := range airuntime.CanonicalGateKeys() {
		openStaticValues[key] = true
	}
	openStatic, err := airuntime.NewGateVector(openStaticValues)
	if err != nil {
		t.Fatal(err)
	}
	openEvaluator, err := airuntime.NewGateEvaluator(openStatic, openDynamic)
	if err != nil {
		t.Fatal(err)
	}
	drifted, err := expectedFrozenGateCompatibilityWithEvaluator(context.Background(), config, openEvaluator, frozen)
	if err != nil {
		t.Fatal(err)
	}
	if drifted == frozen.CompatibilityHash() {
		t.Fatal("current Worker Skill content drift was hidden by the frozen snapshot")
	}
}

func TestDevelopmentExplicitlyEnablesDurableRuntime(t *testing.T) {
	var calls []string
	cfg := validBootstrapConfig("development")
	cfg.AgentRuntime.Enabled = true
	cfg.AgentRuntime.AcceptNewRuns = true
	if err := run(context.Background(), Options{Role: RoleAll, Resolver: validResolver()}, recordingDependencies(cfg, &calls)); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, ",")
	if !strings.Contains(joined, "api") || !strings.Contains(joined, "worker") {
		t.Fatalf("calls=%v, want explicit development API and Worker", calls)
	}
}
