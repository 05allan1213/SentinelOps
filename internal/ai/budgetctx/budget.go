// Package budgetctx 提供 RAG 对既有 durable Budget handle 的最薄 Context 适配。
package budgetctx

import "context"

// RAGReservation 是一次 RAG 预留的 settle 句柄。
type RAGReservation interface {
	Settle(context.Context, int64, int64, bool) error
}

// Provider 只表达 RAG 需要的 reservation 维度，不复制 Budget Store 或计数器。
type Provider interface {
	ReserveRAG(context.Context, string, int64, int64) (RAGReservation, error)
}

type providerKey struct{}

// WithProvider 将 P28 适配器放入当前 durable Attempt Context。
func WithProvider(ctx context.Context, provider Provider) context.Context {
	return context.WithValue(ctx, providerKey{}, provider)
}

// ProviderFromContext 读取当前 durable Attempt 的共享 Budget 适配器。
func ProviderFromContext(ctx context.Context) (Provider, bool) {
	if ctx == nil {
		return nil, false
	}
	provider, ok := ctx.Value(providerKey{}).(Provider)
	return provider, ok && provider != nil
}
