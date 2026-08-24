package embedder

import (
	"context"
	"os"
	"testing"

	appconfig "SentinelOps/internal/config"
)

func TestBailianEmbeddingOnline(t *testing.T) {
	if os.Getenv("SENTINELOPS_ONLINE_TEST") != "1" {
		t.Skip("set SENTINELOPS_ONLINE_TEST=1 to run Bailian online tests")
	}
	cfg, _, err := appconfig.LoadDirectory("../../../manifest/config")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["aliyun_bailian"].APIKey == "" {
		t.Fatal("providers.aliyun_bailian.api_key is empty in config.local.yaml")
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
