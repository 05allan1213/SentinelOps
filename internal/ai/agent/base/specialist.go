package base

import (
	"context"
	"fmt"
	"time"

	"SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/tools"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// SpecialistConfig describes one durable read-only specialist. Tool names are
// resolved through the strict Registry; callers never construct business tools.
type SpecialistConfig struct {
	Name             string
	Description      string
	Instruction      string
	Model            model.BaseChatModel
	ToolNames        []string
	MaxIterations    int
	RetrievalOptions RetrievalOptions
	RuntimeHandler   *runtime.RuntimeHandler
}

// SpecialistPromptConfig controls the explicit prompt assembly used by all
// migrated specialists. Retrieval is injected for deterministic contract tests.
type SpecialistPromptConfig struct {
	Instruction      string
	RetrievalOptions RetrievalOptions
	Retrieval        func(context.Context, *UserMessage, RetrievalOptions) ([]*schema.Document, error)
}

// NewSpecialistAgent creates an official Eino ChatModelAgent with an explicit
// GenModelInput and the shared RuntimeHandler as its outermost handler.
func NewSpecialistAgent(ctx context.Context, cfg SpecialistConfig) (adk.Agent, error) {
	if ctx == nil {
		return nil, fmt.Errorf("specialist context is required")
	}
	if cfg.Name == "" || cfg.Description == "" || cfg.Instruction == "" {
		return nil, fmt.Errorf("specialist name, description and instruction are required")
	}
	if cfg.Model == nil {
		return nil, fmt.Errorf("specialist model is required")
	}
	if cfg.RuntimeHandler == nil {
		return nil, fmt.Errorf("specialist RuntimeHandler is required")
	}
	registered, err := tools.GetManyRequired(cfg.ToolNames)
	if err != nil {
		return nil, fmt.Errorf("resolve %s tools: %w", cfg.Name, err)
	}
	handlers, err := runtime.RuntimeHandlerFirst(cfg.RuntimeHandler)
	if err != nil {
		return nil, err
	}
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          cfg.Name,
		Description:   cfg.Description,
		Instruction:   cfg.Instruction,
		Model:         cfg.Model,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: registered}},
		GenModelInput: NewSpecialistGenModelInput(SpecialistPromptConfig{Instruction: cfg.Instruction, RetrievalOptions: cfg.RetrievalOptions}),
		MaxIterations: cfg.MaxIterations,
		Handlers:      handlers,
	})
}

// NewSpecialistGenModelInput builds the exact old prompt shape without ADK's
// default SessionValues FString interpolation. Only the current query is
// retrieved; preceding messages are passed as history once.
func NewSpecialistGenModelInput(cfg SpecialistPromptConfig) adk.GenModelInput {
	retrieve := cfg.Retrieval
	if retrieve == nil {
		retrieve = RetrieveDocuments
	}
	template := prompt.FromMessages(schema.FString,
		schema.SystemMessage(cfg.Instruction),
		schema.MessagesPlaceholder("history", false),
		schema.UserMessage("{content}"),
	)
	return func(ctx context.Context, _ string, input *adk.AgentInput) ([]adk.Message, error) {
		if input == nil || len(input.Messages) == 0 {
			return nil, fmt.Errorf("specialist input requires at least one message")
		}
		current := input.Messages[len(input.Messages)-1]
		if current == nil || current.Role != schema.User {
			return nil, fmt.Errorf("specialist input must end with a user message")
		}
		history := append([]*schema.Message(nil), input.Messages[:len(input.Messages)-1]...)
		docs, err := retrieve(ctx, &UserMessage{Query: current.Content, History: history}, cfg.RetrievalOptions)
		if err != nil {
			return nil, fmt.Errorf("specialist retrieval: %w", err)
		}
		return template.Format(ctx, map[string]any{
			"content":   current.Content,
			"history":   history,
			"date":      time.Now().Format("2006-01-02 15:04:05"),
			"documents": docs,
		})
	}
}
