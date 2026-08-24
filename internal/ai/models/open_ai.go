// Package models 根据 SentinelOps Routing 创建对话模型。
package models

import (
	"context"
	"fmt"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

type chatProfile struct {
	SecretRef   appconfig.SecretRef
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
	if catalogModel.Driver != appconfig.DriverOpenAICompatibleChat {
		return chatProfile{}, fmt.Errorf("routing.chat.%s model uses driver %s, want %s", profile, catalogModel.Driver, appconfig.DriverOpenAICompatibleChat)
	}
	extraFields := make(map[string]any, 1)
	if route.Options.EnableThinking != nil {
		// enable_thinking 是可选的 Provider 扩展字段，未配置时不能擅自下发。
		extraFields["enable_thinking"] = *route.Options.EnableThinking
	}
	return chatProfile{
		SecretRef:   provider.SecretRef,
		BaseURL:     provider.Endpoints[catalogModel.Driver],
		Model:       catalogModel.ModelID,
		ExtraFields: extraFields,
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
