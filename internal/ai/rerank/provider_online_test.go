package rerank

import (
	"context"
	"os"
	"testing"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/schema"
)

func TestBailianRerankOnline(t *testing.T) {
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
	client, err := GetClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	results := client.Rerank(context.Background(), "Kubernetes incident response", []*schema.Document{
		{Content: "A pasta recipe."},
		{Content: "Diagnose Kubernetes pod failures and recover the workload."},
	}, 1)
	if len(results) != 1 || results[0].Doc.Content == "" {
		t.Fatalf("unexpected rerank result: %#v", results)
	}
}
