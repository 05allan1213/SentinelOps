package bootstrap

import (
	"context"
	"strings"
	"testing"

	airuntime "SentinelOps/internal/ai/runtime"
	appconfig "SentinelOps/internal/config"
)

func TestDurableWorkerOwnerUsesExplicitProcessIdentity(t *testing.T) {
	t.Setenv(workerIDEnv, "phase43-worker-a")
	owner, err := durableWorkerOwner()
	if err != nil {
		t.Fatalf("durableWorkerOwner() error = %v", err)
	}
	if owner != "phase43-worker-a" {
		t.Fatalf("durableWorkerOwner() = %q", owner)
	}
}

func TestDurableWorkerOwnerRejectsAmbiguousIdentity(t *testing.T) {
	for _, owner := range []string{"", " padded", strings.Repeat("a", 129)} {
		t.Run(owner, func(t *testing.T) {
			t.Setenv(workerIDEnv, owner)
			if _, err := durableWorkerOwner(); err == nil {
				t.Fatalf("durableWorkerOwner() accepted %q", owner)
			}
		})
	}
}

func TestWorkerBootstrapBuildsRedactedConfiguredObservation(t *testing.T) {
	config := validBootstrapConfig("development")
	config.MCP = appconfig.MCPConfig{Enabled: true, Servers: map[string]appconfig.MCPServer{
		"inventory": {Enabled: true, Transport: "stdio", Command: "/bin/echo", CWD: "/tmp", HeaderName: "Authorization", HeaderRef: "env:MCP_SECRET"},
	}}
	evaluator, err := airuntime.NewGateEvaluator(airuntime.StaticGateCaps(config), func(_ context.Context, keys []string) (map[string]string, error) {
		values := make(map[string]string, len(keys))
		for _, key := range keys {
			values[key] = "true"
		}
		return values, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := configuredWorkerObservation(context.Background(), config, evaluator, "worker-bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	if observation.WorkerID != "worker-bootstrap" || observation.RuntimeVersion == "" || observation.RuntimeCompatibilityHash == "" || observation.ConfiguredCatalogHash == "" {
		t.Fatalf("incomplete configured observation: %#v", observation)
	}
	if len(observation.ObservedMCP) != 1 || observation.ObservedMCP[0].Name != "inventory" || observation.ObservedMCP[0].Status != "not_observed" {
		t.Fatalf("MCP was falsely reported observed: %#v", observation.ObservedMCP)
	}
	joined := strings.ToLower(observation.ObservedMCP[0].Name + observation.ObservedMCP[0].Error)
	for _, secretLike := range []string{"example.test", "authorization", "mcp_secret", "password"} {
		if strings.Contains(joined, secretLike) {
			t.Fatalf("bootstrap observation leaked %q: %s", secretLike, joined)
		}
	}
}
