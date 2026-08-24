package event_analysis_pipeline

import "SentinelOps/internal/ai/agent/base"

// UserMessage 复用 base.UserMessage（type alias，与 base.UserMessage 是同一类型）。
// 保留此声明是为了向后兼容——subagents 层通过 event_analysis_pipeline.UserMessage 构造输入，无需修改。
type UserMessage = base.UserMessage
