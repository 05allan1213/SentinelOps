// Package agent 通用 Agent 工厂
// 调用链：各 pipeline orchestration.go → NewSingletonAgent → BuildReactAgentGraph
package agent

import (
	"context"
	"sync"

	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/tools"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// AgentConfig Agent 构建配置，各 pipeline 通过此结构声明差异化参数。
type AgentConfig struct {
	// GraphName Eino DAG 图名称，用于链路追踪和日志定位
	GraphName string
	// SystemPrompt 系统提示词，保留 {date}/{documents} 兼容占位符；documents 不进入 System 边界
	SystemPrompt string
	// ModelFactory 返回支持工具调用的 LLM 实例，单例初始化时调用一次
	ModelFactory func(ctx context.Context) (model.ToolCallingChatModel, error)
	// ToolNames 从全局注册表按名取工具子集，不存在的名称静默跳过
	ToolNames []string
	// MaxStep ReAct 最大推理步数，≤0 时取默认值 15
	MaxStep int
	// RewriteEnabled 是否在 RAG 检索前启用查询重写（适合多轮对话场景）
	RewriteEnabled bool
	// SplitEnabled 是否启用子问题拆分+并行检索+Rerank（适合复杂多维查询）
	SplitEnabled bool
}

// NewSingletonAgent 返回懒初始化单例 getter，封装 sync.Once 样板代码。
// 首次调用时初始化 DAG，之后复用同一 runner；初始化失败后每次调用均快速返回同一 error。
func NewSingletonAgent(cfg AgentConfig) func(context.Context) (compose.Runnable[*base.UserMessage, *schema.Message], error) {
	var (
		runner  compose.Runnable[*base.UserMessage, *schema.Message]
		once    sync.Once
		initErr error
	)
	return func(ctx context.Context) (compose.Runnable[*base.UserMessage, *schema.Message], error) {
		once.Do(func() {
			// 用 Background ctx：模型初始化是进程级资源，不应随请求 ctx 取消
			m, err := cfg.ModelFactory(context.Background())
			if err != nil {
				initErr = err
				return
			}
			runner, initErr = base.BuildReactAgentGraph(context.Background(), base.BuildConfig{
				GraphName:      cfg.GraphName,
				SystemPrompt:   cfg.SystemPrompt,
				MaxStep:        cfg.MaxStep,
				Model:          m,
				Tools:          tools.GetMany(cfg.ToolNames),
				RewriteEnabled: cfg.RewriteEnabled,
				SplitEnabled:   cfg.SplitEnabled,
			})
		})
		return runner, initErr
	}
}
