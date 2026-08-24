package embedder

import (
	"context"
	"os"
	"testing"

	appconfig "SentinelOps/internal/config"
)

func TestProviderEmbeddingOnline(t *testing.T) {
	if os.Getenv("SENTINELOPS_ONLINE_TEST") != "1" {
		t.Skip("set SENTINELOPS_ONLINE_TEST=1 to run configured provider online tests")
	}
	cfg, _, err := appconfig.LoadDirectory("../../../manifest/config")
	if err != nil {
		t.Fatal(err)
	}
	route := cfg.Routing.Embedding["default"]
	provider, _, err := cfg.Resolve(route)
	if err != nil {
		t.Fatal(err)
	}
	if err := appconfig.UseSecret(context.Background(), provider.SecretRef, func([]byte) error { return nil }); err != nil {
		t.Fatalf("provider referenced by %s has an unavailable secret reference", route.Model)
	}
	model, err := NewDenseEmbedder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := model.EmbedStrings(context.Background(), []string{"SentinelOps online embedding check"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 2048 {
		t.Fatalf("embedding shape = %d x %d, want 1 x 2048", len(vectors), len(vectors[0]))
	}
}
