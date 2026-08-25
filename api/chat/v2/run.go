package v2

import "github.com/gogf/gf/v2/frame/g"

// CreateRunReq 创建一个等待独立 Worker 执行的 durable L0 Run。
type CreateRunReq struct {
	g.Meta    `path:"/chat/v2/runs" method:"post" summary:"创建 durable Agent Run"`
	SessionID string `json:"session_id" v:"required"`
	Query     string `json:"query" v:"required"`
	Agent     string `json:"agent"`
}

// CreateRunRes 返回持久化 Run 身份，不包含 Agent 输出。
type CreateRunRes struct {
	RunID     string `json:"run_id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// RunEventsReq 从 MySQL seq 游标重放并 tail durable Event。
type RunEventsReq struct {
	g.Meta   `path:"/chat/v2/runs/{run_id}/events" method:"get" summary:"订阅 durable Run 事件"`
	RunID    string `json:"run_id" in:"path" v:"required"`
	AfterSeq int64  `json:"after_seq" in:"query" v:"min:0"`
}

// RunEventsRes 的响应体由 SSE 直接写入。
type RunEventsRes struct{}
