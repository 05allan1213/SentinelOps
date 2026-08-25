package effects

import (
	"context"
	"errors"
)

var (
	// ErrMutationRouteRequired 表示 Agent Mutation endpoint 绕过了 Effect Runtime 或 gated legacy 兼容入口。
	ErrMutationRouteRequired = errors.New("agent mutation requires Effect runtime")
)

type legacyMutationContextKey struct{}

// WithLegacyMutationContext 只能在 legacy 兼容 Gate 已通过后调用。
// 它保留 P42/P43 前的回滚 Artifact，但不允许 durable_v1 伪装 legacy。
func WithLegacyMutationContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, legacyMutationContextKey{}, true)
}

// RequireMutationRoute 在任何业务写入前验证统一 Effect callback 或 gated legacy permit。
func RequireMutationRoute(ctx context.Context) error {
	if _, err := ExecutionMetadataFromContext(ctx); err == nil {
		return nil
	}
	if ctx != nil {
		if allowed, _ := ctx.Value(legacyMutationContextKey{}).(bool); allowed {
			return nil
		}
	}
	return ErrMutationRouteRequired
}
