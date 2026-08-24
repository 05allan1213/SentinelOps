package workflow

import "time"

// ── Workflow Run 状态常量 ─────────────────────────────────────────────────────
//
// pending：运行已创建，等待调度执行
// running：运行正在执行
// success：旧 Runtime 已正常完成（兼容读取；新 Runtime 使用 succeeded）
// waiting_approval：运行等待人工审批，继续占用 Session
// retryable_failed：运行可重试失败，继续占用 Session
// parked：运行因恢复依赖或安全原因暂停，继续占用 Session
// reconciling：运行正在核对未知 Effect，继续占用 Session
// succeeded：新 Runtime 已正常完成
// failed：运行执行失败
// canceled：运行被用户或系统取消
const (
	RunStatusPending         = "pending"
	RunStatusRunning         = "running"
	RunStatusWaitingApproval = "waiting_approval"
	RunStatusRetryableFailed = "retryable_failed"
	RunStatusParked          = "parked"
	RunStatusReconciling     = "reconciling"
	RunStatusSuccess         = "success"
	RunStatusSucceeded       = "succeeded"
	RunStatusFailed          = "failed"
	RunStatusCanceled        = "canceled"
)

// RunOccupiesSession 返回给定状态是否必须继续持有 active_session_key。
// 未知状态按非终态处理，避免未来状态在未审计时意外释放 Session。
func RunOccupiesSession(status string) bool {
	switch status {
	case RunStatusSuccess, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
		return false
	default:
		return true
	}
}

// BranchDelta 表示一个并行分支对工作流状态产生的增量变更。
type BranchDelta struct {
	BranchID string         `json:"branchId,omitempty"`
	Values   map[string]any `json:"values,omitempty"`
}

// MergeConflict 表示多个分支写入同一字段且无法自动合并时的冲突记录。
type MergeConflict struct {
	Field    string `json:"field"`
	BranchID string `json:"branchId,omitempty"`
	Existing any    `json:"existing,omitempty"`
	Incoming any    `json:"incoming,omitempty"`
}

// MergedState 表示分支增量合并后的工作流状态与冲突列表。
type MergedState struct {
	Values    map[string]any  `json:"values,omitempty"`
	Conflicts []MergeConflict `json:"conflicts,omitempty"`
}

// StreamEvent 表示工作流执行过程中可持久化和可恢复推送的流式事件。
type StreamEvent struct {
	ID        int64     `json:"id"`
	RunID     string    `json:"runId,omitempty"`
	Type      string    `json:"type"`
	Payload   any       `json:"payload,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// CheckpointSnapshot 表示工作流在关键节点保存的可恢复状态快照。
type CheckpointSnapshot struct {
	RunID        string         `json:"runId,omitempty"`
	CheckpointID string         `json:"checkpointId,omitempty"`
	Step         string         `json:"step,omitempty"`
	State        map[string]any `json:"state,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
}
