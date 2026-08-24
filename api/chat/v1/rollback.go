// Package v1 定义聊天 API 的请求/响应类型。
package v1

import "github.com/gogf/gf/v2/frame/g"

// RollbackReq 对话回溯请求
type RollbackReq struct {
	g.Meta      `path:"/chat/v1/rollback" method:"POST"`
	SessionId   string `json:"sessionId" v:"required"`
	TargetIndex int    `json:"targetIndex" v:"required|min:0"` // 回退到第几条消息（保留 0~targetIndex）
}

// RollbackRes 对话回溯响应
type RollbackRes struct {
	Success      bool `json:"success"`
	RolledBackTo int  `json:"rolledBackTo"` // 实际回退到的索引
	RemovedCount int  `json:"removedCount"` // 删除的消息数
}
