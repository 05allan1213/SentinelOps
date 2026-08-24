package base

import (
	"context"
	"io"
	"time"

	"SentinelOps/internal/ai/rerank"
	"SentinelOps/internal/ai/retrieval"
	"SentinelOps/internal/ai/rewrite"
	"SentinelOps/internal/ai/rule"
	"SentinelOps/internal/ai/split"

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
	SystemPrompt   string                     // 须含 {date}、{documents}、{content}、{history} 占位符
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
//	START → InputToRag → MilvusRetriever ─┐
//	START → InputToChat ──────────────────┤
//	                                       ▼
//	                               Template → ReactAgent → END
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
		InputToRag      = "InputToRag"      // 拓扑 A：提取并可选改写查询字符串
		InputToChat     = "InputToChat"     // 两种拓扑共用：构建 Prompt 变量 map
		MilvusRetriever = "MilvusRetriever" // 拓扑 A：执行向量检索
		RetrievalNode   = "RetrievalNode"   // 拓扑 B：统一检索流水线
		Template        = "Template"        // 两种拓扑共用：FString 组装消息列表
		ReactAgent      = "ReactAgent"      // 两种拓扑共用：ReAct 推理循环
	)

	maxStep := cfg.MaxStep
	if maxStep <= 0 {
		maxStep = 15
	}

	topology := "A(InputToRag+MilvusRetriever)"
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

	if cfg.SplitEnabled {
		// ── 拓扑 B：统一 RetrievalNode ─────────────────────────────────────
		// 流程：术语归一化 → 合并改写+拆分（单次 LLM）→ 并行检索去重 → Rerank（可选）
		// WithOutputKey("documents") 将结果注入 Template 的 {documents} 占位符
		_ = graph.AddLambdaNode(RetrievalNode, compose.InvokableLambda(
			func(ctx context.Context, input *UserMessage) ([]*schema.Document, error) {
				q := input.Query

				// Step1：术语归一化（进程内缓存，<1ms，无 LLM）
				q = rule.Normalize(ctx, q)

				// Step2：合并改写+拆分（单次 LLM）
				var queries []string
				if cfg.RewriteEnabled {
					_, queries = rewrite.RewriteAndSplit(ctx, q, input.History)
				} else {
					queries = split.SplitQuestions(ctx, q)
				}

				// Step3：并发检索所有子查询，聚合去重
				r := retrieval.GetRetriever()
				docs, err := retrieval.MultiRetrieve(ctx, r, queries)
				if err != nil {
					g.Log().Warningf(ctx, "[RetrievalNode] 向量检索失败: %v", err)
					return nil, err
				}
				g.Log().Debugf(ctx, "[RetrievalNode] 检索完成 | 子查询数=%d | 文档数=%d", len(queries), len(docs))

				// Step4：Rerank 精排（GetClient 返回 nil 表示未启用，直接跳过）
				if rc, _ := rerank.GetClient(ctx); rc != nil && len(docs) > 1 {
					rerankResults := rc.Rerank(ctx, input.Query, docs, 3)
					docs = make([]*schema.Document, 0, len(rerankResults))
					for _, r := range rerankResults {
						docs = append(docs, r.Doc)
					}
				}

				return docs, nil
			},
		), compose.WithOutputKey("documents"), compose.WithNodeName(RetrievalNode))

		_ = graph.AddEdge(compose.START, RetrievalNode)
		_ = graph.AddEdge(compose.START, InputToChat)

	} else {
		// ── 拓扑 A：InputToRag + MilvusRetriever 两节点 ────────────────────
		// InputToRag：提取查询字符串，可选查询重写
		_ = graph.AddLambdaNode(InputToRag, compose.InvokableLambda(
			func(ctx context.Context, input *UserMessage) (string, error) {
				query := input.Query
				// Step1：术语归一化（进程内缓存，<1ms，无 LLM）
				query = rule.Normalize(ctx, query)
				// Step2：查询改写（有对话历史时消除代词歧义）
				if cfg.RewriteEnabled && len(input.History) > 0 {
					query = rewrite.RewriteQuery(ctx, query, input.History)
				}
				return query, nil
			},
		), compose.WithNodeName(InputToRag))

		// MilvusRetriever：向量检索（内含 Redis 语义缓存，缓存命中跳过 Milvus）
		r := retrieval.GetRetriever()
		_ = graph.AddRetrieverNode(MilvusRetriever, r, compose.WithOutputKey("documents"))

		_ = graph.AddEdge(compose.START, InputToRag)
		_ = graph.AddEdge(compose.START, InputToChat)
		_ = graph.AddEdge(InputToRag, MilvusRetriever)
	}

	// ── Template：两种拓扑共用 ─────────────────────────────────────────────
	// FString 组装：SystemMessage(含 {date}/{documents}) + MessagesPlaceholder(history) + UserMessage({content})
	// fan-in：InputToChat 输出的 map 与检索节点的 "documents" key 在此汇聚
	// 注意：指向 Template 的边必须在节点加入 graph 之后才能添加
	ctp := prompt.FromMessages(schema.FString,
		schema.SystemMessage(cfg.SystemPrompt),
		schema.MessagesPlaceholder("history", false),
		schema.UserMessage("{content}"),
	)
	_ = graph.AddChatTemplateNode(Template, ctp)

	// 指向 Template 的汇聚边（两种拓扑），在 Template 节点加入后添加
	if cfg.SplitEnabled {
		_ = graph.AddEdge(RetrievalNode, Template)
		_ = graph.AddEdge(InputToChat, Template)
	} else {
		_ = graph.AddEdge(MilvusRetriever, Template)
		_ = graph.AddEdge(InputToChat, Template)
	}

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
