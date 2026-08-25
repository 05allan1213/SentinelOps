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
		t.Skip("set SENTINELOPS_ONLINE_TEST=1 to run configured provider online tests")
	}
	cfg, _, err := appconfig.LoadDirectory("../../../manifest/config")
	if err != nil {
		t.Fatal(err)
	}
	route := cfg.Routing.Chat["default"].Candidates[0]
	provider, _, err := cfg.Resolve(route)
	if err != nil {
		t.Fatal(err)
	}
	if err := appconfig.UseSecret(context.Background(), provider.SecretRef, func([]byte) error { return nil }); err != nil {
		t.Fatalf("provider referenced by %s has an unavailable secret reference", route.Model)
	}
}

func TestProviderTextGenerationOnline(t *testing.T) {
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

func TestProviderToolCallingOnline(t *testing.T) {
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
