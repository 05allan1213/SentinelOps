// Package models 根据 SentinelOps Routing 创建对话模型。
package models

import (
	"context"
	"fmt"
	"strings"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

type chatProfile struct {
	CatalogRef  string
	Provider    string
	Driver      string
	SecretRef   appconfig.SecretRef
	BaseURL     string
	Model       string
	ExtraFields map[string]any
}

func resolveChatProfile(cfg *appconfig.Config, profile string) (chatProfile, error) {
	candidates, err := resolveChatCandidates(cfg, profile)
	if err != nil {
		return chatProfile{}, err
	}
	return candidates[0], nil
}

func resolveChatCandidates(cfg *appconfig.Config, profile string) ([]chatProfile, error) {
	chatRoute, ok := cfg.Routing.Chat[profile]
	if !ok || len(chatRoute.Candidates) == 0 {
		return nil, fmt.Errorf("routing.chat.%s candidates are not configured", profile)
	}
	resolved := make([]chatProfile, 0, len(chatRoute.Candidates))
	for index, route := range chatRoute.Candidates {
		provider, catalogModel, err := cfg.Resolve(route)
		if err != nil {
			return nil, fmt.Errorf("routing.chat.%s candidate %d: %w", profile, index, err)
		}
		if catalogModel.Driver != appconfig.DriverOpenAICompatibleChat {
			return nil, fmt.Errorf("routing.chat.%s candidate %d uses driver %s, want %s", profile, index, catalogModel.Driver, appconfig.DriverOpenAICompatibleChat)
		}
		providerName, _, qualified := strings.Cut(route.Model, "/")
		if !qualified || providerName == "" {
			return nil, fmt.Errorf("routing.chat.%s candidate %d is not provider-qualified", profile, index)
		}
		extraFields := make(map[string]any, 1)
		if route.Options.EnableThinking != nil {
			// enable_thinking 是可选的 Provider 扩展字段，未配置时不能擅自下发。
			extraFields["enable_thinking"] = *route.Options.EnableThinking
		}
		resolved = append(resolved, chatProfile{
			CatalogRef: route.Model, Provider: providerName, Driver: catalogModel.Driver,
			SecretRef: provider.SecretRef, BaseURL: provider.Endpoints[catalogModel.Driver],
			Model: catalogModel.ModelID, ExtraFields: extraFields,
		})
	}
	return resolved, nil
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
	var model einomodel.ToolCallingChatModel
	err = appconfig.UseSecret(ctx, resolved.SecretRef, func(secret []byte) error {
		created, createErr := openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:      string(secret),
			BaseURL:     resolved.BaseURL,
			Model:       resolved.Model,
			ExtraFields: resolved.ExtraFields,
		})
		model = created
		return createErr
	})
	return model, err
}

// ChatDefault 创建默认对话 Profile。
func ChatDefault(ctx context.Context) (einomodel.ToolCallingChatModel, error) {
	return buildChatProfile(ctx, "default")
}

// ChatReasoning 创建推理对话 Profile。
func ChatReasoning(ctx context.Context) (einomodel.ToolCallingChatModel, error) {
	return buildChatProfile(ctx, "reasoning")
}
