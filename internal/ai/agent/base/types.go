// Package base 提供所有 RAG+ReAct 型 Agent 共用的基础类型和 DAG 构建器。
// 当前被 chat_pipeline、event_analysis_pipeline、report_pipeline、risk_pipeline 共同引用。
package base

import "github.com/cloudwego/eino/schema"

// UserMessage 是标准 RAG+ReAct Agent 的统一输入结构。
// 四个对话型 Agent（chat / event / report / risk）共用此类型，
// 由意图路由层的 SubAgent 适配器负责填充字段后传入对应 Agent。
type UserMessage struct {
	ID      string            `json:"id"`      // 会话唯一标识符，用于追踪和日志记录
	Query   string            `json:"query"`   // 用户的查询内容
	History []*schema.Message `json:"history"` // 历史对话消息列表，用于保持上下文连贯性
}
