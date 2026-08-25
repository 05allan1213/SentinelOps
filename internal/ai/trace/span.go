package trace

import (
	"context"
	"encoding/json"
	"time"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/stringutil"

	"github.com/google/uuid"
)

// ── 手动 Span：非 Eino 组件的埋点 API ─────────────────────────────────────────
//
// 背景：Eino callbacks 只能自动追踪 DAG 内的标准组件（LLM / Tool / Retriever / Embedding）。
// 以下操作 Eino 感知不到，需要手动埋点：
//
//  1. SubAgent 调度（AGENT span）
//     Executor 通过 Worker 工具调用各个 SubAgent 时，每个 Worker 工具的 Invoke 函数
//     对应一个 Eino TOOL 节点（名称如 "event_analysis_agent"）。
//     若不额外埋点，SubAgent 内部的 LLM/Tool 节点会直接挂到 TOOL 节点下，
//     在 Traces 页面无法区分"哪个 Agent"执行了任务。
//     修复（agent_worker.go）：在 runner.Stream 前调用 StartSpan，将 spanCtx 传入 Agent：
//       spanCtx, spanID := trace.StartSpan(ctx, trace.NodeTypeAgent, "EventAnalysisAgent")
//       stream, _ := runner.Stream(spanCtx, msg)  // Agent 内部节点以 AGENT span 为父
//       result, err := workerStream(stream)
//       trace.FinishSpan(spanCtx, spanID, err, nil)
//
//     结果树形结构：
//       TOOL(event_analysis_agent)            ← Eino 自动创建
//         AGENT(EventAnalysisAgent)           ← StartSpan 手动创建
//           LLM(ReAct step1)              ← Eino 回调自动归属到 AGENT
//           TOOL(query_events)                ← Eino 回调自动归属到 AGENT
//           RETRIEVER(Milvus)                 ← Eino 回调自动归属到 AGENT
//
//  2. Redis 操作（CACHE span）
//     语义缓存（semantic.go）的 get/set 和会话记忆（session.go）的 load/save
//     通过 StartSpan/FinishSpan 包裹，记录 Redis 命中/未命中、key 等信息。
//
//  3. GORM Plugin（DB span，需 record_db_spans=true）
//     db_plugin.go 内部也使用 StartSpan/FinishSpan，由 GORM callback 自动触发。
//
// 父子关系维护：
//   StartSpan 调用时将新 nodeID 压栈，FinishSpan 时出栈。
//   在 span 执行期间触发的 Eino callbacks（如内部的 LLM 调用）会通过 Stack.Top()
//   找到此手动 span 作为父节点，形成正确的树形结构。

// spanNodeIDKey 在 context 中存储手动 span 的 nodeID。
// 与 callback.go 的 nodeIDKey{} 分开定义，避免两套机制互相覆盖 ctx value。
type spanNodeIDKey struct{}

// StartSpan 在当前链路中开启一个手动 span（TraceNode），并将此 span 压栈，
// 使得 span 执行期间的所有子节点（Eino callbacks 或嵌套 StartSpan）都归属于它。
//
// 返回值：
//   - newCtx：携带更新后 SpanStack 的新 context，必须传入子操作以建立正确的父子关系
//   - nodeID：此 span 的唯一标识，调用 FinishSpan 时传回
//
// trace 未激活时（ctx 中无 ActiveTrace）：返回原始 ctx 和 ""，FinishSpan 会自动忽略。
func StartSpan(ctx context.Context, nodeType, nodeName string) (context.Context, string) {
	at := Extract(ctx)
	if at == nil {
		return ctx, ""
	}
	nodeID := uuid.New().String()
	parentID := at.Stack.Top()
	depth := at.Stack.Depth()
	now := time.Now()

	at.SetNodeStartTime(nodeID, now)
	modelMetadata := at.resolveModel(modelKindForNodeType(nodeType), nodeName)
	metadata, _ := at.nodeMetadata(modelMetadata)

	asyncInsertNode(at, &dao.TraceNode{
		TraceID:      at.TraceID,
		NodeID:       nodeID,
		ParentNodeID: parentID,
		Depth:        depth,
		NodeType:     nodeType,
		NodeName:     nodeName,
		Status:       StatusRunning,
		StartTime:    now,
		Metadata:     metadata,
	})
	at.TrackNode(nodeID)

	// 压栈：之后在 newCtx 上触发的 Eino callback / 嵌套 StartSpan 会通过 Stack.Top()
	// 找到此 span 作为父节点
	at.Stack.Push(nodeID)

	// 将 nodeID 注入 context，供 db_plugin.go 等同包代码在 FinishSpan 前取回
	return context.WithValue(ctx, spanNodeIDKey{}, nodeID), nodeID
}

// FinishSpan 关闭一个手动 span，出栈并异步写入终态（耗时、错误、元数据）。
//
// 参数：
//   - ctx    : 与 StartSpan 相同的 ctx（用于提取 ActiveTrace）
//   - nodeID : StartSpan 返回的节点 ID；为 "" 时无操作（trace 未激活场景）
//   - err    : 执行过程中发生的错误，nil 表示成功
//   - meta   : 额外元数据（如 cache_hit、redis_key 等），序列化为 JSON 写入 metadata 字段
func FinishSpan(ctx context.Context, nodeID string, err error, meta map[string]any) {
	if nodeID == "" {
		return
	}
	at := Extract(ctx)
	if at == nil {
		return
	}

	// 出栈：恢复调用方的父节点指针，保证后续操作不再归属于此 span
	at.Stack.Pop()

	endTime := time.Now()
	nodeStartTime := at.GetNodeStartTime(nodeID)

	var update *NodeUpdate
	if len(meta) > 0 {
		update = &NodeUpdate{}
		// 提取 cache_hit 到独立字段
		if hit, ok := meta["hit"].(bool); ok {
			update.CacheHit = hit
		}
		redacted, redactErr := redactTraceValue(meta)
		if b, e := json.Marshal(redacted); redactErr == nil && e == nil {
			update.Metadata = string(b)
		}
	}

	status := StatusSuccess
	errMsg, errCode, errType := "", "", ""
	if err != nil {
		status = StatusError
		errMsg = policy.NewRedactor().RedactText(stringutil.TruncateError(err, GetConfig().MaxErrorLength))
		errCode, errType = classifyError(err)
	}

	// 将 UntrackNode 推迟到 asyncFinishNode goroutine 的 UPDATE 完成后，
	// 避免 INSERT/UPDATE 竞态下节点提前从 pendingNodeIDs 移除。
	if update == nil {
		update = &NodeUpdate{}
	}
	update.Metadata = at.mergeNodeMetadata(update.Metadata, ModelMetadata{})
	asyncFinishNode(at, nodeID, status, errMsg, errCode, errType, nodeStartTime, endTime, update,
		func() { at.UntrackNode(nodeID) })
}

// FinishSpanWithCost 关闭手动 span 并记录模型调用的 Token 消耗与成本（CNY）。
// 专为 Eino 无法自动感知的外部模型 API（如 Rerank）设计，会同步累加到
// ActiveTrace 的全链路 Token 和成本统计中，确保 TraceRun.estimated_cost_cny 正确汇总。
//
// inputTokens：API 消耗的输入 Token（rerank 模型只有 total tokens，统一算作 input）
// outputTokens：API 消耗的输出 Token（rerank 为 0）
func FinishSpanWithCost(ctx context.Context, nodeID, modelName string, inputTokens, outputTokens int, err error) {
	if nodeID == "" {
		return
	}
	at := Extract(ctx)
	if at == nil {
		return
	}
	at.Stack.Pop()

	endTime := time.Now()
	nodeStartTime := at.GetNodeStartTime(nodeID)

	// 累加到全链路 Token 和成本统计
	if inputTokens > 0 || outputTokens > 0 {
		at.AddTokensWithModel(inputTokens, outputTokens, modelName)
	}
	modelMetadata := at.resolveModel("", modelName)
	costCNY := modelMetadata.cost(int64(inputTokens), 0, int64(outputTokens), 0)
	if !modelMetadata.valid() {
		costCNY = estimateCost(ctx, modelName, int64(inputTokens), int64(outputTokens))
	}
	if costCNY > 0 {
		at.AddCost(costCNY)
	}

	update := &NodeUpdate{
		ModelName:    modelName,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CostCNY:      costCNY,
		Metadata:     at.mergeNodeMetadata("", modelMetadata),
	}

	status := StatusSuccess
	errMsg, errCode, errType := "", "", ""
	if err != nil {
		status = StatusError
		errMsg = policy.NewRedactor().RedactText(stringutil.TruncateError(err, GetConfig().MaxErrorLength))
		errCode, errType = classifyError(err)
	}

	asyncFinishNode(at, nodeID, status, errMsg, errCode, errType, nodeStartTime, endTime, update,
		func() { at.UntrackNode(nodeID) })
}

func modelKindForNodeType(nodeType string) string {
	switch nodeType {
	case NodeTypeLLM:
		return "chat"
	case NodeTypeEmbedding:
		return "embedding"
	case NodeTypeRerank:
		return "rerank"
	default:
		return ""
	}
}
