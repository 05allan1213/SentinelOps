package base

import (
	"context"
	"io"
	"time"

	"SentinelOps/internal/ai/evidence"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// BuildConfig 公共 RAG+ReAct DAG 构建配置，由 agent.NewSingletonAgent 工厂调用。
type BuildConfig struct {
	GraphName      string                     // Eino 链路追踪和日志定位用
	SystemPrompt   string                     // 保留 {date}/{documents} 兼容占位符；documents 会被移到不受信任 User 数据消息
	MaxStep        int                        // ReAct 最大步数，≤0 取默认值 15
	Model          model.ToolCallingChatModel // 支持 Function Calling 的 LLM 实例
	Tools          []tool.BaseTool            // ReAct 可调用的工具集
	RewriteEnabled bool                       // 检索前查询重写（适合多轮对话，消除代词歧义）
	SplitEnabled   bool                       // 子问题拆分+并行检索+Rerank（适合复杂多维查询）
}

// BuildReactAgentGraph 构建标准 RAG+ReAct DAG，返回可执行 Runnable。
// 适用于 event_analysis / report / risk / solve 四个 Agent，配置不同但拓扑一致。
//
// 拓扑 A（SplitEnabled=false）：
//
//	START → RetrievalNode（归一化→可选改写→检索）─┐
//	START → InputToChat ──────────────────────────┤
//	                                               ▼
//	                                       Template → ReactAgent → END
//
// 拓扑 B（SplitEnabled=true）：
//
//	START → RetrievalNode（重写→拆分→并行检索→去重→Rerank）─┐
//	START → InputToChat ─────────────────────────────────────┤
//	                                                           ▼
//	                                               Template → ReactAgent → END
//
// Template 使用 AllPredecessor 触发模式，等待两条并行支路全部完成后才汇聚。
func BuildReactAgentGraph(ctx context.Context, cfg BuildConfig) (compose.Runnable[*UserMessage, *schema.Message], error) {
	// 节点名称常量：用于 AddEdge 时引用
	const (
		InputToChat    = "InputToChat"    // 两种拓扑共用：构建 Prompt 变量 map
		RetrievalNode  = "RetrievalNode"  // 两种拓扑共用：执行唯一检索阶段
		EvidencePrompt = "EvidencePrompt" // 将检索结果放入不受信任的 User 数据边界
		Template       = "Template"       // 两种拓扑共用：FString 组装消息列表
		ReactAgent     = "ReactAgent"     // 两种拓扑共用：ReAct 推理循环
	)

	maxStep := cfg.MaxStep
	if maxStep <= 0 {
		maxStep = 15
	}

	topology := "A(RetrievalNode)"
	if cfg.SplitEnabled {
		topology = "B(RetrievalNode)"
	}
	g.Log().Infof(ctx, "[Builder] 构建 DAG | graph=%s | topology=%s | maxStep=%d | tools=%d | rewrite=%v",
		cfg.GraphName, topology, maxStep, len(cfg.Tools), cfg.RewriteEnabled)

	graph := compose.NewGraph[*UserMessage, *schema.Message]()

	// ── InputToChat：两种拓扑共用 ──────────────────────────────────────────
	// 将 *UserMessage 转换为 Template 所需的变量 map，与检索支路并行执行
	_ = graph.AddLambdaNode(InputToChat, compose.InvokableLambda(
		func(ctx context.Context, input *UserMessage) (map[string]any, error) {
			return map[string]any{
				"content": input.Query,
				"history": input.History,
				"date":    time.Now().Format("2006-01-02 15:04:05"),
			}, nil
		},
	), compose.WithNodeName(InputToChat))

	// 两种旧 Graph 拓扑调用同一共享检索函数；差异只由 RetrievalOptions 表达。
	_ = graph.AddLambdaNode(RetrievalNode, compose.InvokableLambda(
		func(ctx context.Context, input *UserMessage) ([]*schema.Document, error) {
			return RetrieveDocuments(ctx, input, RetrievalOptions{
				RewriteEnabled: cfg.RewriteEnabled,
				SplitEnabled:   cfg.SplitEnabled,
			})
		},
	), compose.WithOutputKey("documents"), compose.WithNodeName(RetrievalNode))
	_ = graph.AddLambdaNode(EvidencePrompt, compose.InvokableLambda(
		func(_ context.Context, docs []*schema.Document) (string, error) {
			formatted, _, err := evidence.FormatDocumentsContext(ctx, docs)
			return formatted, err
		},
	), compose.WithInputKey("documents"), compose.WithOutputKey("documents"), compose.WithNodeName(EvidencePrompt))

	_ = graph.AddEdge(compose.START, RetrievalNode)
	_ = graph.AddEdge(compose.START, InputToChat)

	// ── Template：两种拓扑共用 ─────────────────────────────────────────────
	// FString 组装：SystemMessage(仅安全规则) + UserMessage(不受信任 Evidence) + MessagesPlaceholder(history) + UserMessage({content})
	// fan-in：InputToChat 输出的 map 与检索节点的 "documents" key 在此汇聚
	// 注意：指向 Template 的边必须在节点加入 graph 之后才能添加
	ctp := prompt.FromMessages(schema.FString,
		schema.SystemMessage(evidence.SafeInstruction(cfg.SystemPrompt)),
		schema.UserMessage("{documents}"),
		schema.MessagesPlaceholder("history", false),
		schema.UserMessage("{content}"),
	)
	_ = graph.AddChatTemplateNode(Template, ctp)

	// 指向 Template 的汇聚边（两种拓扑），在 Template 节点加入后添加
	_ = graph.AddEdge(RetrievalNode, EvidencePrompt)
	_ = graph.AddEdge(EvidencePrompt, Template)
	_ = graph.AddEdge(InputToChat, Template)

	// ── ReactAgent：两种拓扑共用 ───────────────────────────────────────────
	// AnyLambda 同时封装 Generate（同步）和 Stream（流式），支持 SSE 调用
	agentCfg := &react.AgentConfig{MaxStep: maxStep}
	agentCfg.ToolCallingModel = cfg.Model
	agentCfg.ToolsConfig.Tools = cfg.Tools
	// 部分模型的流式输出顺序是先文字内容、后 Tool Calls。
	// 默认 firstChunkStreamToolCallChecker 只检查第一个 chunk：遇到文字内容就返回 false（无工具调用），
	// 导致 agent 提前路由到 END，工具永远不会被执行，只有规划文字出现在输出中。
	// 此处读取完整流来检测，确保不同 Provider 的 Tool Calls 都能被正确识别。
	agentCfg.StreamToolCallChecker = func(ctx context.Context, sr *schema.StreamReader[*schema.Message]) (bool, error) {
		defer sr.Close()
		for {
			msg, err := sr.Recv()
			if err == io.EOF {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if len(msg.ToolCalls) > 0 {
				return true, nil
			}
		}
	}
	agent, err := react.NewAgent(ctx, agentCfg)
	if err != nil {
		g.Log().Errorf(ctx, "[Builder] ReactAgent 初始化失败 | graph=%s: %v", cfg.GraphName, err)
		return nil, err
	}
	reactLambda, err := compose.AnyLambda(agent.Generate, agent.Stream, nil, nil)
	if err != nil {
		return nil, err
	}
	_ = graph.AddLambdaNode(ReactAgent, reactLambda, compose.WithNodeName(ReactAgent))

	_ = graph.AddEdge(Template, ReactAgent)
	_ = graph.AddEdge(ReactAgent, compose.END)

	return graph.Compile(ctx,
		compose.WithGraphName(cfg.GraphName),
		// AllPredecessor：Template 等待检索支路和 InputToChat 全部完成后才触发
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
}
