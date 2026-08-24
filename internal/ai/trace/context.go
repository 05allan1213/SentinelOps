package trace

import (
	"context"
	"sync"
	"time"
)

// ── Context 传播设计 ───────────────────────────────────────────────────────────
//
// 问题：trace 系统需要在一次 HTTP 请求的完整生命周期内（跨多个 goroutine、多层调用栈）
// 共享同一个 ActiveTrace 对象，以便：
//  1. 所有 Eino callbacks 拿到同一个 TraceID，节点记录才能归属到同一条链路
//  2. SpanStack 在父子节点间正确传递，保证 parent_node_id 关系正确
//  3. Token 累加器在多个并行 LLM 节点（如 Plan Agent 的 Executor）之间线程安全地汇总
//
// 方案：复用 Go 标准库 context.WithValue 机制，将 ActiveTrace 随 ctx 在调用链中传播。
//
// 关键约束：ctx 必须通过函数参数显式传递。
// 原因是 Eino 的 Graph 会将 ctx 传入每个 Lambda/LLM/Tool 节点，天然支持此模式。

// traceCtxKey 是存储 ActiveTrace 的 context key，使用私有类型防止外部包意外覆盖。
type traceCtxKey struct{}

// nodeIDKey 是存储当前 Eino 节点 ID 的 context key。
// Eino callback 的 OnStart 注入，OnEnd/OnError 读取，用于精确匹配开始/结束的节点。
// 与 SpanStack 分离：Stack 维护父子关系，nodeIDKey 只标识"当前正在执行的节点"。
type nodeIDKey struct{}

// ── SpanStack ─────────────────────────────────────────────────────────────────
//
// SpanStack 维护当前请求内"正在执行的节点 ID"调用栈，栈顶即为最近启动、尚未结束的节点。
//
// 作用：为每个新节点确定其 parent_node_id。
//   - 节点 OnStart 时：读 Stack.Top() 作为 parent，然后 Push 自身 ID
//   - 节点 OnEnd/OnError 时：Pop 自身 ID，恢复父节点指针
//
// 并发安全：Eino Graph 可能在多个 goroutine 中并发执行节点（如 ToolsNode 并行调用多工具），
// 因此 SpanStack 所有操作均加 sync.Mutex 保护。
//
// 注意：SpanStack 不区分不同 goroutine。Eino 的并行执行通过 context 副本隔离父子关系，SpanStack 仅在串行路径上精确工作。
// 并行路径（如 ToolsNode）的父节点通过 Eino 在 OnStart 前注入的 ctx 中的 nodeIDKey 确定。
type SpanStack struct {
	mu    sync.Mutex
	items []string
}

// Push 入栈一个节点 ID（OnStart 时调用）
func (s *SpanStack) Push(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, id)
}

// Pop 出栈当前节点 ID（OnEnd / OnError 时调用）
func (s *SpanStack) Pop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) > 0 {
		s.items = s.items[:len(s.items)-1]
	}
}

// Top 返回栈顶节点 ID（即当前父节点），栈为空时返回 ""（表示此节点为根节点）
func (s *SpanStack) Top() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return ""
	}
	return s.items[len(s.items)-1]
}

// Depth 返回当前栈深度，即新节点应存入的 depth 字段值（根节点 depth=0）
func (s *SpanStack) Depth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// ── ActiveTrace ───────────────────────────────────────────────────────────────
//
// ActiveTrace 是一次 HTTP 请求的完整追踪上下文，通过 context.Value 在整个调用链中传播。
// 设计为 goroutine 安全：多个并行节点（LLM streaming、工具并行调用）可以同时读写。
//
// 字段分组：
//  1. 身份信息（TraceID、StartTime）：不可变，创建后只读
//  2. 拓扑信息（Stack）：节点进出栈时修改，内部加锁
//  3. 时间索引（nodeStartTimes）：OnStart 写入、OnEnd 读取，需加锁
//  4. Token 累加器（Total*）：多个并行 LLM 节点汇总，需加锁
//  5. 工具输入缓存（toolInputs）：OnStart 写入参数，OnEnd 写入 metadata
type ActiveTrace struct {
	TraceID   string
	StartTime time.Time
	Stack     *SpanStack

	// nodeStartTimes：记录每个节点的 startTime（nodeID → time.Time）。
	// 原因：Eino callback 的 OnStart 和 OnEnd 是两次独立的函数调用，
	// 耗时（duration_ms）= OnEnd.time - OnStart.time，必须在 ActiveTrace 中跨调用传递。
	nodeStartTimes map[string]time.Time

	// Token 累加器：一次请求可能调用多个 LLM 节点（Router + SubAgent 内多轮 ReAct），
	// 各节点的 Token 消耗由 buildNodeUpdate() 提取后通过 AddTokens() 累加到此处，
	// 最终在 FinishRun 时一次性写入 TraceRun.total_*_tokens。
	mu                     sync.Mutex
	TotalInputTokens       int64
	TotalCachedInputTokens int64
	TotalOutputTokens      int64
	TotalReasoningTokens   int64
	// PrimaryModelName 记录本次请求最多 Token 的 LLM 模型名（用于成本估算）
	PrimaryModelName   string
	primaryModelTokens int64   // 内部记录：最大 token 数对应的模型
	TotalCostCNY       float64 // 所有 LLM 节点成本之和（CNY），由 AddCost() 累加

	// toolInputs：TOOL 节点的输入参数缓存（nodeID → 截断后的 JSON 字符串）。
	// 工具调用的参数在 OnStart（input）中可见，输出在 OnEnd（output）中可见，
	// 但写 metadata 的逻辑在 OnEnd 的 buildNodeUpdate() 中，因此需要将输入跨回调传递。
	toolInputs map[string]string

	// pendingNodeIDs：已 INSERT 但尚未 UPDATE 为终态的节点 ID 集合。
	// OnStart / StartSpan 时加入，OnEnd / OnError / FinishSpan 时移除。
	// asyncFinishRun 的兜底清理使用此集合（WHERE node_id IN (...)），
	// 替代原来的范围扫描（WHERE trace_id = ? AND status = 'running'），
	// 从根本上避免 InnoDB Next-Key Lock 与并发点更新之间的死锁。
	pendingNodeIDs map[string]struct{}

	// StreamWg 跟踪所有 OnEndWithStreamOutput 启动的后台排空 goroutine。
	// asyncFinishRun 通过 WaitGroup + 超时等待这些 goroutine 完成，
	StreamWg sync.WaitGroup
}

// AddTokens 线程安全地向 ActiveTrace 累加 Token 数量。
// modelName 用于追踪"主模型"（Token 消耗最多的模型），供成本估算使用。
// 由 buildNodeUpdate() 在每个 LLM 节点的 OnEnd/OnEndWithStream 回调中调用。
func (t *ActiveTrace) AddTokens(in, out int) {
	t.AddTokensWithModel(in, out, "")
}

// AddTokensWithModel 带模型名称的 Token 累加，追踪 Token 消耗最多的模型。
func (t *ActiveTrace) AddTokensWithModel(in, out int, modelName string) {
	t.AddUsageWithModel(in, 0, out, 0, modelName)
}

// AddUsageWithModel accumulates totals and non-additive usage breakdowns.
func (t *ActiveTrace) AddUsageWithModel(in, cachedIn, out, reasoning int, modelName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if in < 0 {
		in = 0
	}
	if out < 0 {
		out = 0
	}
	if cachedIn < 0 {
		cachedIn = 0
	}
	if cachedIn > in {
		cachedIn = in
	}
	if reasoning < 0 {
		reasoning = 0
	}
	if reasoning > out {
		reasoning = out
	}
	t.TotalInputTokens += int64(in)
	t.TotalCachedInputTokens += int64(cachedIn)
	t.TotalOutputTokens += int64(out)
	t.TotalReasoningTokens += int64(reasoning)
	// 用 in+out 判断主模型（Token 最多的那个）
	nodeTokens := int64(in + out)
	if modelName != "" && nodeTokens > t.primaryModelTokens {
		t.primaryModelTokens = nodeTokens
		t.PrimaryModelName = modelName
	}
}

// AddCost 线程安全地向 ActiveTrace 累加节点级别成本（CNY）。
// 由 buildNodeUpdate() 在每个 LLM 节点的 OnEnd/OnEndWithStream 回调中调用。
func (t *ActiveTrace) AddCost(cost float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.TotalCostCNY += cost
}

// SetNodeStartTime 记录节点开始时间，供同一节点的 OnEnd/OnError 回调计算耗时。
// map 在 ActiveTrace 生命周期内只增不删，键为 nodeID（UUID），无内存泄漏风险。
func (t *ActiveTrace) SetNodeStartTime(nodeID string, startTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.nodeStartTimes == nil {
		t.nodeStartTimes = make(map[string]time.Time)
	}
	t.nodeStartTimes[nodeID] = startTime
}

// GetNodeStartTime 获取节点开始时间；若未记录（极少数情况下 OnStart 被跳过），
// 降级返回 trace 开始时间，保证 duration_ms 不为负数。
func (t *ActiveTrace) GetNodeStartTime(nodeID string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.nodeStartTimes == nil {
		return t.StartTime
	}
	if st, ok := t.nodeStartTimes[nodeID]; ok {
		return st
	}
	return t.StartTime
}

// SetToolInput 在 TOOL 节点的 OnStart 回调中缓存工具输入参数 JSON。
// buildNodeUpdate() 在 OnEnd 时通过 GetToolInput() 读取，写入 metadata.tool_input 字段。
func (t *ActiveTrace) SetToolInput(nodeID, input string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.toolInputs == nil {
		t.toolInputs = make(map[string]string)
	}
	t.toolInputs[nodeID] = input
}

// GetToolInput 获取 TOOL 节点缓存的输入参数，未记录时返回 ""。
func (t *ActiveTrace) GetToolInput(nodeID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.toolInputs == nil {
		return ""
	}
	return t.toolInputs[nodeID]
}

// TrackNode 将节点 ID 加入 pending 集合（节点 INSERT 时调用）。
func (t *ActiveTrace) TrackNode(nodeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pendingNodeIDs == nil {
		t.pendingNodeIDs = make(map[string]struct{})
	}
	t.pendingNodeIDs[nodeID] = struct{}{}
}

// UntrackNode 从 pending 集合移除节点 ID（节点即将写终态时调用）。
func (t *ActiveTrace) UntrackNode(nodeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pendingNodeIDs, nodeID)
}

// DrainPendingNodeIDs 返回当前 pending 集合的快照并清空它。
// 供 asyncFinishRun 兜底清理使用：只更新真正未完成的节点，避免范围扫描引起死锁。
func (t *ActiveTrace) DrainPendingNodeIDs() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pendingNodeIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(t.pendingNodeIDs))
	for id := range t.pendingNodeIDs {
		ids = append(ids, id)
	}
	t.pendingNodeIDs = nil
	return ids
}

// Inject 将 ActiveTrace 注入到 context，返回携带 trace 信息的新 ctx。
// 由 StartRun（请求入口）调用一次，之后所有子调用只需传递 ctx 即可访问 trace。
func Inject(ctx context.Context, t *ActiveTrace) context.Context {
	return context.WithValue(ctx, traceCtxKey{}, t)
}

// Extract 从 context 中提取 ActiveTrace。
//   - 返回 nil 表示 trace 未启用或此 ctx 不在 trace 链路中，调用方应直接跳过 trace 逻辑。
//   - 所有 callback / span / db_plugin 代码均以 `if at == nil { return }` 作为快速路径，
//     保证 trace 禁用时零开销。
func Extract(ctx context.Context) *ActiveTrace {
	v := ctx.Value(traceCtxKey{})
	if v == nil {
		return nil
	}
	at, _ := v.(*ActiveTrace)
	return at
}
