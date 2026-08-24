// Package intent 实现基于 LLM 意图识别的多 Agent 调度系统。
//
// 架构原理：用户查询先经 Router 节点（调用默认 Chat Profile 做意图识别），识别出目标 IntentType，
// 再由 Executor 节点按 IntentType 从 registry 取出对应 SubAgent 执行，最终结果经 StreamCallback
// 流式回传给调用方。整体以 Eino Graph 编排为 DAG：START → Router → Executor → END。
//
//	core ← 公共类型、注册表、共享记忆（无内部依赖）
//	subagents ← core + 各 pipeline（子 Agent 适配器）
//	intent ← core + blank import subagents（编排层）
package intent

import "SentinelOps/internal/ai/intent/core"

type (
	IntentType     = core.IntentType
	Task           = core.Task
	Result         = core.Result
	StreamCallback = core.StreamCallback
	SubAgent       = core.SubAgent
	IntentInput    = core.IntentInput
	IntentOutput   = core.IntentOutput
)

// 意图常量别名（标准路由可用的 5 类意图 + 通用状态类型）
// 注：IntentPlan / IntentPlanStep 属于深度思考模式专用，由 service/chat/intent.go 直接引用 core 包使用，
// 不在此处重导出，避免与标准路由的意图集合混淆。
const (
	IntentChat   = core.IntentChat
	IntentEvent  = core.IntentEvent
	IntentReport = core.IntentReport
	IntentRisk   = core.IntentRisk
	IntentSolve  = core.IntentSolve
	IntentIntel  = core.IntentIntel
	IntentOps    = core.IntentOps
	IntentStatus = core.IntentStatus
)
