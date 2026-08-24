// Package models builds the chat profiles configured by SentinelOps routing.
package models

import (
	"context"
	"fmt"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

type chatProfile struct {
	APIKey      string
	BaseURL     string
	Model       string
	ExtraFields map[string]any
}

func resolveChatProfile(cfg *appconfig.Config, profile string) (chatProfile, error) {
	route, ok := cfg.Routing.Chat[profile]
	if !ok {
		return chatProfile{}, fmt.Errorf("routing.chat.%s is not configured", profile)
	}
	provider, catalogModel, err := cfg.Resolve(route)
	if err != nil {
		return chatProfile{}, err
	}
	if catalogModel.Driver != "openai_compatible" {
		return chatProfile{}, fmt.Errorf("routing.chat.%s model uses driver %s, want openai_compatible", profile, catalogModel.Driver)
	}
	return chatProfile{
		APIKey:  provider.APIKey,
		BaseURL: provider.Endpoints.OpenAICompatible,
		Model:   catalogModel.ModelID,
		ExtraFields: map[string]any{
			"enable_thinking": route.Options.EnableThinking,
		},
	}, nil
}

func buildChatProfile(ctx context.Context, profile string) (einomodel.ToolCallingChatModel, error) {
	cfg, err := appconfig.Current()
	if err != nil {
		return nil, err
	}
	resolved, err := resolveChatProfile(cfg, profile)
	if err != nil {
		return nil, err
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      resolved.APIKey,
		BaseURL:     resolved.BaseURL,
		Model:       resolved.Model,
		ExtraFields: resolved.ExtraFields,
	})
}

// ChatDefault creates the standard profile with thinking disabled.
func ChatDefault(ctx context.Context) (einomodel.ToolCallingChatModel, error) {
	return buildChatProfile(ctx, "default")
}

// ChatReasoning creates the reasoning profile with thinking enabled.
func ChatReasoning(ctx context.Context) (einomodel.ToolCallingChatModel, error) {
	return buildChatProfile(ctx, "reasoning")
}
