package summary_pipeline

import (
	"github.com/cloudwego/eino/schema"
)

// SummaryInput 摘要 Agent 的输入
//
// 字段说明：
//   - SessionID：会话唯一标识，用于日志追踪和错误定位
//   - Messages：需要总结的消息列表（通常是前 10 条消息，即 5 轮对话）
//
// 使用场景：
//
//	当短期记忆超过 30 条消息时，memory_cache 会提取前 10 条消息传入摘要 Agent
type SummaryInput struct {
	SessionID string            `json:"session_id"` // 会话 ID（用于日志追踪）
	Messages  []*schema.Message `json:"messages"`   // 需要总结的消息列表（5 轮对话）
}

// SummaryOutput 摘要 Agent 的输出
//
// 字段说明：
//   - Summary：总结文本，由模型根据对话内容自主控制长度，一般在 100-300 字之间
//
// 输出示例：
//
//	Summary: "用户是 Go 后端开发者，使用 GoFrame 框架开发微服务。讨论了如何集成日志系统和告警功能，决定使用 ELK 技术栈。用户偏好命令行工具和自动化脚本。"
type SummaryOutput struct {
	Summary string `json:"summary"` // 总结文本
}
