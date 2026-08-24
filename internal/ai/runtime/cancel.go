package runtime

import (
	"context"
	"fmt"
	"time"

	"SentinelOps/internal/ai/workflow"

	"github.com/cloudwego/eino/adk"
)

// DrainBoundary 选择仍持有 lease 时的官方 Eino safe-point。
type DrainBoundary string

const (
	DrainAfterChatModel DrainBoundary = "after_chat_model"
	DrainAfterToolCalls DrainBoundary = "after_tool_calls"
)

const maxDrainTimeout = 10 * time.Minute

// RequestDrain 使用有界 timeout 和 WithRecursive 请求 Eino safe-point cancel。
// 调用方必须再用 ConfirmSafePointHandoff 验证当前 generation 的 fenced checkpoint，不能只凭 Wait 成功交接。
func RequestDrain(cancel adk.AgentCancelFunc, boundary DrainBoundary, timeout time.Duration) (*adk.CancelHandle, error) {
	if cancel == nil {
		return nil, fmt.Errorf("agent cancel function is required")
	}
	if timeout <= 0 || timeout > maxDrainTimeout {
		return nil, fmt.Errorf("drain timeout must be between 1ns and %s", maxDrainTimeout)
	}
	var mode adk.CancelMode
	switch boundary {
	case DrainAfterChatModel:
		mode = adk.CancelAfterChatModel
	case DrainAfterToolCalls:
		mode = adk.CancelAfterToolCalls
	default:
		return nil, fmt.Errorf("unsupported drain boundary %q", boundary)
	}
	handle, contributed := cancel(
		adk.WithAgentCancelMode(mode),
		adk.WithAgentCancelTimeout(timeout),
		adk.WithRecursive(),
	)
	if !contributed || handle == nil {
		return nil, adk.ErrExecutionEnded
	}
	return handle, nil
}

// RequestLostLeaseCancel 立即 recursive cancel；P09/P10 fence 继续拒绝旧 generation 的所有写入。
func RequestLostLeaseCancel(cancel adk.AgentCancelFunc) (*adk.CancelHandle, error) {
	if cancel == nil {
		return nil, fmt.Errorf("agent cancel function is required")
	}
	handle, contributed := cancel(adk.WithAgentCancelMode(adk.CancelImmediate), adk.WithRecursive())
	if !contributed || handle == nil {
		return nil, adk.ErrExecutionEnded
	}
	return handle, nil
}

// ConfirmSafePointHandoff 只有 cancel 成功且当前 generation checkpoint 已 fenced 提交才允许交接。
func ConfirmSafePointHandoff(
	ctx context.Context,
	store *workflow.GORMStore,
	attempt *AttemptContext,
	handle *adk.CancelHandle,
) error {
	if ctx == nil || store == nil || attempt == nil || handle == nil {
		return fmt.Errorf("handoff context, store, attempt and cancel handle are required")
	}
	if err := handle.Wait(); err != nil {
		return fmt.Errorf("wait safe-point cancel: %w", err)
	}
	if err := store.RequireCommittedCheckpoint(ctx, attempt.Lease); err != nil {
		return fmt.Errorf("verify fenced safe-point checkpoint: %w", err)
	}
	return nil
}
