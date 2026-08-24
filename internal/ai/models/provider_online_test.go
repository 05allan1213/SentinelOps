package models

import (
	"context"
	"os"
	"testing"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/schema"
)

func requireOnlineConfig(t *testing.T) {
	t.Helper()
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
}

func TestBailianTextGenerationOnline(t *testing.T) {
	requireOnlineConfig(t)
	model, err := ChatDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	message, err := model.Generate(context.Background(), []*schema.Message{schema.UserMessage("Reply with exactly: SENTINELOPS_OK")})
	if err != nil {
		t.Fatal(err)
	}
	if message.Content == "" {
		t.Fatal("empty model response")
	}
}

func TestBailianToolCallingOnline(t *testing.T) {
	requireOnlineConfig(t)
	model, err := ChatDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bound, err := model.WithTools([]*schema.ToolInfo{{Name: "sentinelops_healthcheck", Desc: "Always call this tool to perform the requested health check."}})
	if err != nil {
		t.Fatal(err)
	}
	message, err := bound.Generate(context.Background(), []*schema.Message{schema.UserMessage("Call sentinelops_healthcheck now. Do not answer in text.")})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.ToolCalls) == 0 || message.ToolCalls[0].Function.Name != "sentinelops_healthcheck" {
		t.Fatalf("expected sentinelops_healthcheck tool call, got %#v", message.ToolCalls)
	}
}
