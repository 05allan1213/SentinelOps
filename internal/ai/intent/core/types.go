// Package core 定义意图路由系统的公共类型、接口与常量。
// subagents 包和 intent 包均依赖此包，避免两者之间的循环依赖。
package core

import "context"

// IntentType 意图类型标识，使用 string 类型以兼容 LLM 返回的 JSON 解析（{"intent": "xxx"}）。
type IntentType string

// 预定义的意图类型，分为两组：
//
// 【标准路由意图（deep_thinking=false）】由 Router LLM 识别，经 Executor 分发给对应 SubAgent：
//
//	IntentChat / IntentEvent / IntentReport / IntentRisk / IntentSolve
//
// 【深度思考专用（deep_thinking=true）】不经过 Router/Executor，由 service/chat.ExecuteDeepThink 直接使用：
//
//	IntentPlan / IntentPlanStep
//
// IntentStatus 为通用状态推送类型，两种模式均可使用。
const (
	// ── 标准路由意图 ────────────────────────────────────────────────────────
	IntentChat   IntentType = "chat"   // 通用对话、安全咨询、知识问答、日志查询、订阅管理（默认降级目标）
	IntentEvent  IntentType = "event"  // 安全事件查询、事件分析、告警关联、事件处置建议
	IntentReport IntentType = "report" // 报告生成、查看报告、报告数据分析
	IntentRisk   IntentType = "risk"   // 风险评估、威胁分析、漏洞评估、安全评分、CVE 分析
	IntentSolve  IntentType = "solve"  // 单条安全事件应急响应方案生成（聚焦处置步骤与修复措施）
	IntentIntel  IntentType = "intel"  // 联网威胁情报检索：CVE 详情、漏洞公告、威胁组织、恶意 IP/域名
	IntentOps    IntentType = "ops"    // AI 智能运维：触发指定事件的自动化响应（封禁/通知/状态更新）

	// ── 深度思考模式专用 ──────────────────────────────────────────────────────
	IntentPlan     IntentType = "plan"      // Plan Agent 最终答案（展示在主内容气泡）
	IntentPlanStep IntentType = "plan_step" // Plan Agent 规划执行中间步骤（展示在规划过程折叠块中）

	// ── 通用状态类型 ─────────────────────────────────────────────────────────
	IntentStatus IntentType = "status" // 路由/处理状态通知，不属于内容流
)

// Task 子 Agent 执行任务载体，由 Executor 按 RouterOutput 构建后传递给 SubAgent.Execute。
type Task struct {
	ID        string         // 任务唯一标识，用于追踪
	Query     string         // 用户原始查询
	Intent    IntentType     // 意图识别结果
	SessionId string         // 会话 ID，透传到 SubAgent，用于按会话加载记忆
	Params    map[string]any // 扩展参数（可选）
}

// Result 子 Agent 执行结果。
// Error 不向 Graph 上抛，而是封装在此结构中返回，保证 Graph 始终正常结束。
type Result struct {
	TaskID  string     // 任务 ID
	Intent  IntentType // 实际执行的意图类型
	Content string     // 输出内容
	Error   error      // 执行错误（若有）
}

// StreamCallback 流式回调函数，用于 SSE 逐 chunk 推送。
type StreamCallback func(intent IntentType, chunk string)

// IntentInput Graph 入口输入，由 Intent.Execute 构建后注入 DAG。
type IntentInput struct {
	Query     string         // 用户输入问题
	SessionId string         // 会话 ID，由 controller 注入，经 Executor 透传到 Task
	Callback  StreamCallback // 实时推送中间结果的回调
}

// IntentOutput Graph 出口输出，由 Executor Lambda 产生。
type IntentOutput struct {
	TaskID  string     // 任务 ID
	Intent  IntentType // 实际执行的意图类型
	Content string     // 最终回复内容
	Error   error      // 错误信息（若有）
}

// SubAgent 子 Agent 接口，各专业 Agent（Chat/Event/Report/Risk/Plan）均实现此接口。
type SubAgent interface {
	Name() IntentType
	Execute(ctx context.Context, task *Task, callback StreamCallback) (*Result, error)
}
