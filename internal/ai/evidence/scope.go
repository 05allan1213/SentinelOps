package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"SentinelOps/internal/ai/policy"
)

// Scope 是检索授权和缓存隔离所需的最小身份快照。
type Scope struct {
	UserID          string
	Role            string
	KnowledgeBaseID string
	AccessScope     string
	IndexedVersion  uint64
	PolicyHash      string
}

// WithScopeKey 用于把检索 Scope 放入请求 Context，供 Retriever 和 DAO 共同使用。
type scopeKey struct{}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok
}

// WithIdentityScope 用当前认证身份补齐检索 Scope；调用方 Context 已显式设置
// Scope 时保持不变（durable Attempt 自带更精确的冻结 Scope）。
// 非 durable 的 Agent 入口共用该助手，避免只设置 Collector 而让
// query_internal_docs 以 "missing retrieval scope" fail-closed。
func WithIdentityScope(ctx context.Context) context.Context {
	if ctx == nil {
		return ctx
	}
	if _, ok := ScopeFromContext(ctx); ok {
		return ctx
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return ctx
	}
	return WithScope(ctx, Scope{
		UserID: identity.UserID, Role: string(identity.Role),
		AccessScope: "user:" + identity.UserID,
	})
}

// Namespace 返回不会跨用户、角色、知识库、索引版本或 Policy 命中的缓存命名空间。
func (s Scope) Namespace() string {
	canonical := strings.Join([]string{s.UserID, s.Role, s.KnowledgeBaseID, s.AccessScope, formatUint(s.IndexedVersion), s.PolicyHash}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return "scope-v1:" + hex.EncodeToString(sum[:])
}

// Allows 判断 MySQL 与 Milvus 共用的 access_scope predicate。
func (s Scope) Allows(accessScope string) bool {
	accessScope = strings.TrimSpace(accessScope)
	if accessScope == "" {
		return false
	}
	if accessScope == "public" {
		return true
	}
	if accessScope == s.AccessScope && s.AccessScope != "" {
		return true
	}
	if strings.HasPrefix(accessScope, "user:") {
		return s.UserID != "" && strings.TrimPrefix(accessScope, "user:") == s.UserID
	}
	if strings.HasPrefix(accessScope, "role:") {
		return s.Role != "" && strings.TrimPrefix(accessScope, "role:") == s.Role
	}
	return false
}

func formatUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
