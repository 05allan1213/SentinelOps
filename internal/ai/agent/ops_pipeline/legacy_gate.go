package ops_pipeline

import (
	"context"
	"errors"

	airuntime "SentinelOps/internal/ai/runtime"
)

var (
	// ErrLegacyOpsWritesDisabled 表示保留的旧 Ops 直写 Artifact 没有通过兼容许可。
	ErrLegacyOpsWritesDisabled = errors.New("legacy Ops direct writes are disabled")
)

// LegacyWriteGate 是 P42 才能接入发布 Gate evaluator 的最薄兼容边界。
// P26 不提供生产开启实现；nil 为默认关闭，durable_v1 永远不能通过。
type LegacyWriteGate interface {
	AllowLegacyOpsWrites(context.Context) bool
}

// RequireLegacyOpsWrites 是保留的 legacy Ops Artifact 唯一兼容判定。
func RequireLegacyOpsWrites(ctx context.Context, gate LegacyWriteGate) error {
	if ctx == nil || airuntime.IsDurableV1Context(ctx) || gate == nil || !gate.AllowLegacyOpsWrites(ctx) {
		return ErrLegacyOpsWritesDisabled
	}
	return nil
}
