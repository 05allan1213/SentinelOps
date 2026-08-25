package trace

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"SentinelOps/internal/ai/evidence"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/stringutil"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// ── Eino Callback 机制与集成原理 ──────────────────────────────────────────────
//
// Eino 的 callbacks 系统是其可观测性扩展点：框架在每个 DAG 节点执行的生命周期事件
//（OnStart / OnEnd / OnError / OnEndWithStreamOutput）上调用注册的 Handler，
// 无需修改 Agent / LLM / Tool 的实现代码，即可实现全链路追踪。
//
// 注册方式（main.go）：
//
//	ghttp.GetServer().AppendGlobalHandlers(trace.NewCallbackHandler())
//
// GoFrame 的 middleware 机制会将此 Handler 注入到每个请求的 ctx 中，
// Eino Graph 执行时自动从 ctx 取出并调用。
//
// ── 四个生命周期事件 ──────────────────────────────────────────────────────────
//
//  1. OnStart：节点开始执行时触发（同步）
//     - 创建 TraceNode，记录 parent_node_id（来自 SpanStack.Top()）和 depth
//     - TOOL 节点在此捕获输入参数 JSON（OnEnd 时才能写 metadata，需跨回调传递）
//     - 将 nodeID 压栈（Stack.Push）并注入 context（供 OnEnd 取回）
//
//  2. OnEnd：非流式节点成功完成时触发（同步）
//     - LLM 节点：提取 TokenUsage、模型名，累加到 ActiveTrace
//     - TOOL 节点：合并 OnStart 捕获的输入 + 本次输出，写入 metadata JSON
//     - RETRIEVER 节点：记录检索结果（top-3 序列化）和 final_top_k
//     - 出栈（Stack.Pop），更新 TraceNode 为 success 终态
//
//  3. OnError：节点执行失败时触发（同步）
//     - 分类错误码（RateLimit / Timeout / Canceled 等），写入 error_message
//     - 出栈（Stack.Pop），更新 TraceNode 为 error 终态
//
//  4. OnEndWithStreamOutput：流式节点成功完成时触发（异步）
//     - 适用于：LLM streaming（ChatModel 流式输出）以及 Eino 内部的 Lambda/ToolsNode
//     - 问题：Eino 对流式节点不调用 OnEnd，若不注册此回调则节点永远停留 running
//     - 方案：立即出栈（保证后续节点父子关系不错位），后台 goroutine 排空流读取
//       最后一个 chunk（含 TokenUsage），写入 TraceNode 终态
//     - 注意：排空流的 goroutine 不阻塞主调用链，与 SSE 推流并发执行
//
// ── SpanStack 与父子关系 ──────────────────────────────────────────────────────
//
// Eino Graph 串行执行路径（Router → Executor → SubAgent → LLM）：
//   Start(Router) → Push(R) → Start(Executor) → Push(E) → End(Router) → Pop → ...
// 此处有个微妙的顺序：Eino 的 OnStart 在节点进入前触发，OnEnd 在节点退出后触发，
// 但对于嵌套节点（如 Lambda 内调用 LLM），内部节点的 Start 在外部节点 End 之前触发，
// 形成天然的入栈/出栈配对，SpanStack 能正确维护父子关系。

// NewCallbackHandler 创建并注册 Eino 全局 callbacks.Handler。
// 注册为全局 Handler（AppendGlobalHandlers）后，所有请求的所有 Eino 节点均自动追踪。
func NewCallbackHandler() callbacks.Handler {
	builder := callbacks.NewHandlerBuilder()

	// ── OnStart：节点开始 ─────────────────────────────────────────────────────
	// 在此处确定节点的 parent_node_id 和 depth，因为 SpanStack 记录了当前调用深度。
	// 必须在 Push 之前读 Top/Depth，Push 之后新节点才成为下一层的父节点。
	builder.OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
		at := Extract(ctx)
		if at == nil || info == nil {
			return ctx
		}
		nodeID := uuid.New().String()
		parentID := at.Stack.Top() // 读栈顶作为父节点（Push 前读，保证不是自身）
		depth := at.Stack.Depth()  // 当前深度即为新节点的 depth
		nodeStartTime := time.Now()

		at.SetNodeStartTime(nodeID, nodeStartTime)

		asyncInsertNode(&dao.TraceNode{
			TraceID:      at.TraceID,
			NodeID:       nodeID,
			ParentNodeID: parentID,
			Depth:        depth,
			NodeType:     resolveNodeType(info),
			NodeName:     resolveNodeName(info),
			Status:       StatusRunning,
			StartTime:    nodeStartTime,
		})
		at.TrackNode(nodeID)

		// TOOL 节点：在 OnStart 捕获输入参数（OnEnd 时输入已不可见）
		if info.Component == components.ComponentOfTool {
			if toolIn := tool.ConvCallbackInput(input); toolIn != nil && toolIn.ArgumentsInJSON != "" {
				at.SetToolInput(nodeID, stringutil.TruncateRunes(toolIn.ArgumentsInJSON, 1000))
			}
		}

		at.Stack.Push(nodeID)
		// 将 nodeID 注入 ctx，供同一节点的 OnEnd/OnError 通过 ctx.Value(nodeIDKey{}) 取回
		return context.WithValue(ctx, nodeIDKey{}, nodeID)
	})

	// ── OnEnd：非流式节点成功 ──────────────────────────────────────────────────
	// 从 ctx 取回 nodeID（而非 Stack.Top()），原因：节点 End 时其子节点可能已入栈，
	// Stack.Top() 此时是最后一个子节点而非当前节点，会导致 Pop 错误。
	builder.OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
		at := Extract(ctx)
		if at == nil {
			return ctx
		}
		nodeID, _ := ctx.Value(nodeIDKey{}).(string)
		if nodeID == "" {
			return ctx
		}
		endTime := time.Now()
		nodeStartTime := at.GetNodeStartTime(nodeID)
		update := buildNodeUpdate(ctx, info, output, at, nodeID)
		// 将 UntrackNode 推迟到 asyncFinishNode goroutine 的 UPDATE 完成后，
		// 避免 INSERT/UPDATE 竞态下节点提前从 pendingNodeIDs 移除。
		asyncFinishNode(nodeID, StatusSuccess, "", "", "", nodeStartTime, endTime, update,
			func() { at.UntrackNode(nodeID) })
		at.Stack.Pop()
		return ctx
	})

	// ── OnError：节点失败 ─────────────────────────────────────────────────────
	builder.OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
		at := Extract(ctx)
		if at == nil {
			return ctx
		}
		nodeID, _ := ctx.Value(nodeIDKey{}).(string)
		if nodeID == "" {
			return ctx
		}
		errCode, errType := classifyError(err)
		endTime := time.Now()
		nodeStartTime := at.GetNodeStartTime(nodeID)
		asyncFinishNode(nodeID, StatusError, stringutil.TruncateError(err, GetConfig().MaxErrorLength), errCode, errType, nodeStartTime, endTime, nil,
			func() { at.UntrackNode(nodeID) })
		at.Stack.Pop()
		return ctx
	})

	// ── OnEndWithStreamOutput：流式节点成功 ────────────────────────────────────
	// Eino 对 ChatModel streaming 和部分 Lambda 节点调用此回调而非 OnEnd。
	// 关键点：
	//  1. 立即出栈（同步），保证后续节点（如下一个 ReAct 步骤）能拿到正确的父节点
	//  2. 后台 goroutine 排空 StreamReader 获取最终 chunk（含 TokenUsage），再写终态
	//  3. 调用方（Eino Graph）持有 StreamReader 的另一端，两端并发读写，互不阻塞
	builder.OnEndWithStreamOutputFn(func(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
		at := Extract(ctx)
		if at == nil {
			output.Close()
			return ctx
		}
		nodeID, _ := ctx.Value(nodeIDKey{}).(string)
		// 立即出栈：此处与 OnEnd 不同，必须在 goroutine 启动前出栈，
		// 否则后续同步节点的 OnStart 会在后台 goroutine Pop 前先 Push，导致栈错乱
		at.Stack.Pop()
		if nodeID == "" {
			output.Close()
			return ctx
		}
		nodeStartTime := at.GetNodeStartTime(nodeID)
		// 后台排空流：读取所有 chunk，取最后一个（含完整 TokenUsage）写终态。
		// Add(1) 在 goroutine 外调用，避免 asyncFinishRun 在 goroutine 启动前就 Wait() 通过的竞态。
		at.StreamWg.Add(1)
		go func() {
			defer at.StreamWg.Done()
			defer output.Close()
			var lastChunk callbacks.CallbackOutput
			for {
				chunk, err := output.Recv()
				if err != nil {
					if err != io.EOF {
						// 流异常（网络断开、LLM 报错等）：标记为错误
						errCode, errType := classifyError(err)
						asyncFinishNode(nodeID, StatusError, stringutil.TruncateError(err, GetConfig().MaxErrorLength), errCode, errType, nodeStartTime, time.Now(), nil,
							func() { at.UntrackNode(nodeID) })
					} else {
						// 流正常结束：使用最后一个 chunk 提取 TokenUsage
						var update *NodeUpdate
						if lastChunk != nil {
							update = buildNodeUpdate(ctx, info, lastChunk, at, nodeID)
						}
						asyncFinishNode(nodeID, StatusSuccess, "", "", "", nodeStartTime, time.Now(), update,
							func() { at.UntrackNode(nodeID) })
					}
					return
				}
				lastChunk = chunk
			}
		}()
		return ctx
	})

	return builder.Build()
}

// resolveNodeType 根据 Eino RunInfo.Component 字段映射到 trace 节点类型常量。
// 未匹配的组件（Lambda、Prompt、Indexer 等）统一归为 LAMBDA 类型，
// 前端以统一样式展示，避免节点类型枚举膨胀。
func resolveNodeType(info *callbacks.RunInfo) string {
	switch info.Component {
	case components.ComponentOfChatModel:
		return NodeTypeLLM
	case components.ComponentOfTool:
		return NodeTypeTool
	case components.ComponentOfRetriever:
		return NodeTypeRetriever
	case components.ComponentOfEmbedding:
		return NodeTypeEmbedding
	default:
		return NodeTypeLambda
	}
}

// resolveNodeName 从 Eino RunInfo 提取可读的节点名称，优先级从高到低：
//
//  1. RunInfo.Name：通过 compose.WithNodeName("InputToRag") 显式设置的语义名称（最优）
//  2. RunInfo.Type：Eino 框架自动推断的实现类型名（如 "OpenAI"、"MilvusRetriever"）
//  3. 组件类别兜底（最低优先级，仅在两者均为空时使用）
//
// Lambda 节点通过 builder.go 中的 WithNodeName 显式设置名称，确保显示具体节点名而非通用 "Lambda"。
func resolveNodeName(info *callbacks.RunInfo) string {
	if info.Name != "" {
		return info.Name
	}
	if info.Type != "" {
		return info.Type
	}
	switch info.Component {
	case components.ComponentOfChatModel:
		return "ChatModel"
	case components.ComponentOfTool:
		return "Tool"
	case components.ComponentOfRetriever:
		return "Retriever"
	case components.ComponentOfEmbedding:
		return "Embedding"
	case components.ComponentOfIndexer:
		return "Indexer"
	case components.ComponentOfPrompt:
		return "Prompt"
	default:
		if info.Component != "" {
			return string(info.Component)
		}
		return "Lambda"
	}
}

// buildNodeUpdate 根据组件类型断言 output，提取结构化信息填充 NodeUpdate。
// 每种组件类型的输出结构不同，需分别处理：
//   - LLM（ChatModel）：提取 TokenUsage + 模型名 + 可选 Completion 文本，并计算节点成本
//   - TOOL：将 OnStart 捕获的输入参数 + 本次输出合并为 metadata JSON
//   - RETRIEVER：序列化检索结果（top-3，每条截断 500 字符）+ final_top_k
func buildNodeUpdate(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput, at *ActiveTrace, nodeID string) *NodeUpdate {
	if info == nil || output == nil {
		return nil
	}

	// 调试日志：记录所有节点的组件类型
	g.Log().Debugf(ctx, "[buildNodeUpdate] 处理节点 | component=%s | type=%s | name=%s",
		info.Component, info.Type, info.Name)

	update := &NodeUpdate{}
	switch info.Component {
	case components.ComponentOfChatModel:
		modelOut := model.ConvCallbackOutput(output)
		if modelOut == nil {
			return nil
		}
		if modelOut.Config != nil {
			update.ModelName = modelOut.Config.Model
		}
		if modelOut.TokenUsage != nil {
			usage := usageFromModel(modelOut.TokenUsage, modelOut.Extra)
			if modelOut.Message != nil {
				usage = mergeUsage(usage, parseUsageBreakdown(modelOut.Message.Extra))
			}
			update.InputTokens = usage.InputTokens
			update.CachedInputTokens = usage.CachedInputTokens
			update.OutputTokens = usage.OutputTokens
			update.ReasoningTokens = usage.ReasoningTokens
			// 累加到链路级别的 Token 累加器（携带模型名以追踪主模型）
			at.AddUsageWithModel(update.InputTokens, update.CachedInputTokens, update.OutputTokens, update.ReasoningTokens, update.ModelName)
			// 计算节点级别成本并累加到链路总成本
			nodeCost := estimateCostWithBreakdown(ctx, update.ModelName,
				int64(update.InputTokens), int64(update.CachedInputTokens), int64(update.OutputTokens), int64(update.ReasoningTokens))
			update.CostCNY = nodeCost
			at.AddCost(nodeCost)
		}
		// record_prompt=true 时记录 Completion 文本（含 PII 风险，默认关闭）
		if GetConfig().RecordPrompt && modelOut.Message != nil {
			update.CompletionText = stringutil.TruncateRunes(modelOut.Message.Content, 5000)
		}

	case components.ComponentOfTool:
		// 将 tool_name、OnStart 捕获的输入参数、本次输出合并为 metadata JSON
		meta := map[string]any{"tool_name": info.Name}
		if toolInput := at.GetToolInput(nodeID); toolInput != "" {
			meta["tool_input"] = toolInput
		}
		if toolOut := tool.ConvCallbackOutput(output); toolOut != nil && toolOut.Response != "" {
			meta["tool_output"] = stringutil.TruncateRunes(toolOut.Response, 2000)
		}
		if b, err := json.Marshal(meta); err == nil {
			update.Metadata = string(b)
		}

	case components.ComponentOfRetriever:
		retrieverOut := retriever.ConvCallbackOutput(output)
		if retrieverOut == nil {
			// Retriever 输出为空时仍需记录 FinalTopK=0
			update.FinalTopK = 0
			update.DocCount = 0
			update.MaxVectorScore = 0
			return update
		}
		update.FinalTopK = len(retrieverOut.Docs)
		update.DocCount = len(retrieverOut.Docs)

		// 计算最高相似度分数
		var maxScore float64
		for _, doc := range retrieverOut.Docs {
			if doc.MetaData != nil {
				if scoreVal, ok := doc.MetaData["score"]; ok {
					if score, ok2 := scoreVal.(float64); ok2 && score > maxScore {
						maxScore = score
					}
				}
			}
		}
		update.MaxVectorScore = maxScore

		docs := buildEvidenceTraceSummaries(retrieverOut.Docs)
		if b, err := json.Marshal(docs); err == nil {
			update.RetrievedDocs = string(b)
		}

	case components.ComponentOfEmbedding:
		// Embedding 组件：提取 TokenUsage + 模型名，计算节点成本
		// 嵌入模型只有输入 Token 成本，无输出 Token。
		modelOut := model.ConvCallbackOutput(output)
		if modelOut == nil {
			g.Log().Warningf(ctx, "[Embedding] modelOut 为 nil")
			return nil
		}

		var modelName string
		if modelOut.Config != nil {
			modelName = modelOut.Config.Model
			update.ModelName = modelName
		} else {
			g.Log().Warningf(ctx, "[Embedding] Config 为 nil，无法获取模型名")
		}

		if modelOut.TokenUsage != nil {
			usage := usageFromModel(modelOut.TokenUsage, modelOut.Extra)
			update.InputTokens = usage.InputTokens
			update.CachedInputTokens = usage.CachedInputTokens
			update.OutputTokens = 0 // Embedding 无输出 token
			// 累加到链路级别的 Token 累加器
			at.AddUsageWithModel(update.InputTokens, update.CachedInputTokens, 0, 0, modelName)
			// 计算节点级别成本并累加到链路总成本
			nodeCost := estimateCostWithBreakdown(ctx, modelName,
				int64(update.InputTokens), int64(update.CachedInputTokens), 0, 0)
			update.CostCNY = nodeCost
			at.AddCost(nodeCost)
			// 添加调试日志
			g.Log().Debugf(ctx, "[Embedding] 成本计算 | model=%s | inputTokens=%d | cost=¥%.6f",
				modelName, update.InputTokens, nodeCost)
		} else {
			g.Log().Warningf(ctx, "[Embedding] TokenUsage 为 nil")
		}
	}
	return update
}

type evidenceTraceSummary struct {
	EvidenceID string  `json:"evidence_id"`
	SourceID   string  `json:"source_id"`
	Summary    string  `json:"summary,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

// buildEvidenceTraceSummaries 只保留可回查 ID、来源和有界摘要，禁止把 Evidence 全文写入 Trace。
func buildEvidenceTraceSummaries(docs []*schema.Document) []evidenceTraceSummary {
	result := make([]evidenceTraceSummary, 0, min(3, len(docs)))
	for i, doc := range docs {
		if i >= 3 {
			break
		}
		if doc == nil {
			continue
		}
		ref := evidence.EvidenceRefFromDocument(doc, time.Now().UTC())
		item := evidenceTraceSummary{EvidenceID: ref.EvidenceID, SourceID: ref.SourceID, Summary: stringutil.TruncateRunes(doc.Content, 160)}
		if doc.MetaData != nil {
			if score, ok := doc.MetaData["score"].(float64); ok {
				item.Score = score
			}
		}
		result = append(result, item)
	}
	return result
}
