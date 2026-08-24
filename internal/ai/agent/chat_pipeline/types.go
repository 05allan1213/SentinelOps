package chat_pipeline

import "SentinelOps/internal/ai/agent/base"

// UserMessage 复用 base.UserMessage（type alias，与 base.UserMessage 是同一类型）。
// chat_pipeline 保留自己的 orchestration/lambda 逻辑（含 Token 感知压缩），
// 但输入类型统一为 base.UserMessage，与其他 Agent 对齐。
type UserMessage = base.UserMessage
