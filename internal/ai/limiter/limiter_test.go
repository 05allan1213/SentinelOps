package limiter

import (
	"context"
	"testing"
)

func TestLimiterCatalogRefIsolation(t *testing.T) {
	registry, err := NewRegistry(Settings{QPS: 1, Burst: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Allow("provider_a/shared") {
		t.Fatal("first provider_a request was denied")
	}
	if registry.Allow("provider_a/shared") {
		t.Fatal("provider_a burst was not enforced")
	}
	if !registry.Allow("provider_b/shared") {
		t.Fatal("provider_b inherited provider_a limiter state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = registry.Wait(ctx, "provider_c/shared"); err == nil {
		t.Fatal("canceled limiter wait succeeded")
	}
}

func TestLimiterConfigureSameSettingsPreservesCandidateState(t *testing.T) {
	settings := Settings{QPS: 1, Burst: 1}
	if err := Configure(settings); err != nil {
		t.Fatal(err)
	}
	if !Allow("provider_a/configured") {
		t.Fatal("first configured token was denied")
	}
	if err := Configure(settings); err != nil {
		t.Fatal(err)
	}
	if Allow("provider_a/configured") {
		t.Fatal("same settings reset the process candidate token bucket")
	}
}
