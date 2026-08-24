package compat_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type contractCheckpointStore struct{}

func (*contractCheckpointStore) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (*contractCheckpointStore) Set(context.Context, string, []byte) error {
	return nil
}

func (*contractCheckpointStore) Delete(context.Context, string) error {
	return nil
}

func TestEinoPublicContracts(t *testing.T) {
	var _ adk.CheckPointStore = (*contractCheckpointStore)(nil)
	var _ adk.CheckPointDeleter = (*contractCheckpointStore)(nil)
	var _ adk.ChatModelAgentMiddleware = (*adk.BaseChatModelAgentMiddleware)(nil)

	var newRunner func(context.Context, adk.RunnerConfig) *adk.Runner = adk.NewRunner
	var newChatModelAgent func(context.Context, *adk.ChatModelAgentConfig) (*adk.ChatModelAgent, error) = adk.NewChatModelAgent
	var newAgentTool func(context.Context, adk.Agent, ...adk.AgentToolOption) tool.BaseTool = adk.NewAgentTool
	var newSkillMiddleware func(context.Context, *skill.Config) (adk.ChatModelAgentMiddleware, error) = skill.NewMiddleware
	var newToolSearchMiddleware func(context.Context, *toolsearch.Config) (adk.ChatModelAgentMiddleware, error) = toolsearch.New

	config := adk.ChatModelAgentConfig{
		Handlers:            []adk.ChatModelAgentMiddleware{},
		ModelRetryConfig:    &adk.ModelRetryConfig{},
		ModelFailoverConfig: &adk.ModelFailoverConfig[*schema.Message]{},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				ToolCallMiddlewares: []compose.ToolMiddleware{},
			},
		},
	}

	if newRunner == nil || newChatModelAgent == nil || newAgentTool == nil ||
		newSkillMiddleware == nil || newToolSearchMiddleware == nil || config.ModelRetryConfig == nil {
		t.Fatal("Eino v0.9.15 public contract is incomplete")
	}
}
