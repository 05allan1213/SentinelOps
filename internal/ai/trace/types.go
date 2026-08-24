// Package trace 实现 SentinelOps 的全链路可观测性系统。
//
// # 背景与动机
//
// 多 Agent 系统由于调用链深、组件多（LLM / 工具 / 检索 / 缓存），
// 线上问题排查极其困难：
//   - 一次对话请求可能经历 Router LLM → SubAgent → RAG 检索 → 工具调用 → Redis 等十余个节点
//   - 各节点耗时、Token 消耗、命中缓存与否等信息分散在日志中，难以关联
//   - LLM 费用无法按请求粒度统计，成本优化缺乏数据支撑
//
// # 设计目标
//
//  1. 零侵入：业务代码不需要手动调用 trace API，Eino callbacks 自动拦截所有 LLM/Tool/Retriever 节点；
//     仅对 Eino 感知不到的层（SubAgent 调度、Redis）提供轻量手动埋点 API。
//  2. 性能无损：所有写库操作均为异步 goroutine，不在请求主路径上等待 I/O。
//
// # 数据模型
//
// 每次 HTTP 请求对应一条 TraceRun（一次"链路"），其下挂若干 TraceNode（各执行节点）。
// 节点以树形组织（parent_node_id），深度由 SpanStack 在请求生命周期内动态维护。
//
//	TraceRun  ── 链路根：记录请求入口、sessionId、总耗时、总 Token、总费用
//	  └─ TraceNode[LLM]            Router 意图识别（当前 Chat Profile）
//	       └─ TraceNode[TOOL]      event_analysis_agent（Executor 调用 Worker 工具）
//	            └─ TraceNode[AGENT] EventAnalysisAgent（手动 span，包裹整个 Worker 执行）
//	                 ├─ TraceNode[LLM]       ReAct 推理步骤
//	                 ├─ TraceNode[RETRIEVER] Milvus 向量检索
//	                 └─ TraceNode[TOOL]      query_events 工具调用

package trace

// ── 链路状态常量 ───────────────────────────────────────────────────────────────
//
//	running：链路/节点已启动，尚未收到结束信号
//	success：正常完成
//	error：发生错误（包含 LLM 报错、context canceled、工具调用失败等）
const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusError   = "error"
)

// ── 节点类型常量 ───────────────────────────────────────────────────────────────
//
// 对齐 OpenInference / OpenTelemetry GenAI 语义约定，同时扩展了两种手动埋点类型：
//
//	Eino 自动捕获的组件（callback.go 通过 RunInfo.Component 自动映射）：
//	  LLM       — ChatModel 节点（当前 Routing 选择的模型调用）
//	  TOOL      — 工具节点（query_events / search_similar_events 等工具）
//	  RETRIEVER — Milvus 向量检索节点
//	  EMBEDDING — 文本嵌入节点
//	  LAMBDA    — 自定义 Lambda 节点（显示具体名称：InputToRag、InputToChat、RetrievalNode 等）
//
//	手动埋点类型（Eino 感知不到，通过 span.go 的 StartSpan/FinishSpan 埋点）：
//	  AGENT     — Worker 工具内包裹整个 SubAgent 执行时开启（agent_worker.go），
//	              保证内部 LLM/Tool/Retriever 节点归属到具名 Agent 下而非外层 TOOL 节点
//	  CACHE     — Redis 操作（语义缓存 get/set、会话记忆 load/save）
const (
	NodeTypeLLM       = "LLM"
	NodeTypeTool      = "TOOL"
	NodeTypeRetriever = "RETRIEVER"
	NodeTypeEmbedding = "EMBEDDING"
	NodeTypeLambda    = "LAMBDA"
	NodeTypeAgent     = "AGENT"  // 被 Executor 调度的子 Agent（手动埋点）
	NodeTypeCache     = "CACHE"  // Redis 操作（session/semantic cache 手动埋点）
	NodeTypeDB        = "DB"     // MySQL 慢查询（>100ms，GORM plugin 埋点）
	NodeTypeRerank    = "RERANK" // Rerank 模型调用（手动埋点）
)

// ── 标准化错误码 ───────────────────────────────────────────────────────────────
//
// 将原始错误消息归类为有限的错误类型，便于前端按错误码过滤、统计与告警。
// classifyError()（store.go）负责从 error.Error() 字符串匹配并返回对应错误码。
//
//	RATE_LIMIT    — LLM 限流（HTTP 429 / "rate limit"）
//	TIMEOUT       — 超时（"timeout" / "deadline exceeded"）
//	CANCELED      — 请求被中断（context.Canceled，如用户切换页面、AbortController）
//	INVALID_PARAM — 参数校验失败（HTTP 400 / "invalid" / "bad request"）
//	INTERNAL      — 后端内部错误（未命中上述规则的兜底分类）
//	UNKNOWN       — err == nil 但仍调用了错误路径（防御性兜底）
const (
	ErrCodeRateLimit    = "RATE_LIMIT"
	ErrCodeTimeout      = "TIMEOUT"
	ErrCodeCanceled     = "CANCELED"
	ErrCodeInvalidParam = "INVALID_PARAM"
	ErrCodeInternal     = "INTERNAL"
	ErrCodeUnknown      = "UNKNOWN"
)
