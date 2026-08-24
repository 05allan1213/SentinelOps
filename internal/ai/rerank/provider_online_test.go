package rerank

import (
	"context"
	"os"
	"testing"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/schema"
)

func TestProviderRerankOnline(t *testing.T) {
	if os.Getenv("SENTINELOPS_ONLINE_TEST") != "1" {
		t.Skip("set SENTINELOPS_ONLINE_TEST=1 to run configured provider online tests")
	}
	cfg, _, err := appconfig.LoadDirectory("../../../manifest/config")
	if err != nil {
		t.Fatal(err)
	}
	route := cfg.Routing.Rerank["default"]
	provider, _, err := cfg.Resolve(route)
	if err != nil {
		t.Fatal(err)
	}
	if err := appconfig.UseSecret(context.Background(), provider.SecretRef, func([]byte) error { return nil }); err != nil {
		t.Fatalf("provider referenced by %s has an unavailable secret reference", route.Model)
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
