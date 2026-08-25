package middleware

import (
	"context"
	"net/http"
	"strings"

	"SentinelOps/internal/ai/policy"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// AuthDisabledWriteGuard 让独立机器鉴权写入口也遵守 auth-disabled 全局只读契约。
func AuthDisabledWriteGuard() ghttp.HandlerFunc {
	return authDisabledWriteGuard(func(ctx context.Context) bool {
		enabled, _ := g.Cfg().Get(ctx, "auth.jwt.enabled")
		return enabled.Bool()
	})
}

func authDisabledWriteGuard(authEnabled func(context.Context) bool) ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		if !authEnabled(r.Context()) {
			r.Response.WriteStatus(http.StatusForbidden)
			r.Response.WriteJson(g.Map{"message": "authentication disabled: writes are read-only"})
			return
		}
		r.Middleware.Next()
	}
}

// AuthorizationMiddleware 对 HTTP 路由执行粗粒度 RBAC；资源 Scope 仍由 Service 复核。
func AuthorizationMiddleware() ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		permission, public := requestPermission(r.Method, r.URL.Path)
		if public {
			r.Middleware.Next()
			return
		}
		if err := policy.Authorize(r.Context(), permission, policy.Resource{}); err != nil {
			r.Response.WriteStatus(http.StatusForbidden)
			r.Response.WriteJson(g.Map{"message": "insufficient permission"})
			return
		}
		r.Middleware.Next()
	}
}

func requestPermission(method, path string) (policy.Permission, bool) {
	method = strings.ToUpper(method)
	path = strings.ToLower(path)
	if method == http.MethodOptions || path == "/api/auth/v1/login" {
		return "", true
	}
	if method == http.MethodGet || method == http.MethodHead {
		if strings.Contains(path, "/settings/v1/ingest_key") || isManagementPath(path) {
			return policy.PermissionManageUsersPolicyGates, false
		}
		return policy.PermissionViewScoped, false
	}
	if path == "/api/chat/v1/chat" || path == "/api/event/v1/analyze/stream" || path == "/api/event/v1/pipeline/stream" {
		return policy.PermissionCreateReadOnlyRun, false
	}
	if path == "/api/knowledge/v1/search" {
		return policy.PermissionViewScoped, false
	}
	if path == "/api/rageval/v1/feedback" {
		return policy.PermissionWriteOwnFeedback, false
	}
	if strings.Contains(path, "/approval/") || strings.Contains(path, "/approvals/") {
		return policy.PermissionDecideProposal, false
	}
	if isManagementPath(path) {
		return policy.PermissionManageUsersPolicyGates, false
	}
	return policy.PermissionBusinessWrite, false
}

func isManagementPath(path string) bool {
	return strings.Contains(path, "/auth/v1/register") ||
		strings.Contains(path, "/settings/") ||
		strings.Contains(path, "/ops/v1/playbooks") ||
		strings.Contains(path, "/users/") ||
		strings.Contains(path, "/policy/") ||
		strings.Contains(path, "/gate/")
}
