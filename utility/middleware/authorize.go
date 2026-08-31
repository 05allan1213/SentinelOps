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
		permission, public := requestPermission(r.Method, effectiveRouterPath(r))
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

// effectiveRouterPath 与 GoFrame searchRouter 使用同一优先级，避免路由与授权读取不同路径。
func effectiveRouterPath(r *ghttp.Request) string {
	path := r.URL.Path
	if r.URL.RawPath != "" {
		path = r.URL.RawPath
	}
	if xURLPath := r.Header.Get(ghttp.HeaderXUrlPath); xURLPath != "" {
		path = xURLPath
	}
	return normalizeRouterPath(path)
}

// normalizeRouterPath 复刻 GoFrame searchHandlers 对连续斜杠的归一化规则。
func normalizeRouterPath(path string) string {
	previousIsSeparator := false
	for index := 0; index < len(path); {
		if path[index] == '/' {
			if previousIsSeparator {
				path = path[:index] + path[index+1:]
				continue
			}
			previousIsSeparator = true
		} else {
			previousIsSeparator = false
		}
		index++
	}
	return path
}

func requestPermission(method, path string) (policy.Permission, bool) {
	method = strings.ToUpper(method)
	path = strings.ToLower(path)
	if method == http.MethodOptions || path == "/api/auth/v1/login" {
		return "", true
	}
	if method == http.MethodPost && isRuntimeRecoveryPath(path) {
		return policy.PermissionRecoverRuntime, false
	}
	if method == http.MethodGet && isRuntimeV1Path(path) {
		return policy.PermissionViewScoped, false
	}
	if method == http.MethodGet || method == http.MethodHead {
		if strings.Contains(path, "/settings/v1/ingest_key") || isManagementPath(path) {
			return policy.PermissionManageUsersPolicyGates, false
		}
		return policy.PermissionViewScoped, false
	}
	if path == "/api/chat/v1/chat" || path == "/api/chat/v2/runs" || path == "/api/event/v1/analyze/stream" || path == "/api/event/v1/pipeline/stream" {
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

func isRuntimeV1Path(path string) bool {
	return path == "/api/runtime/v1" || strings.HasPrefix(path, "/api/runtime/v1/")
}

func isRuntimeRecoveryPath(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	return len(parts) == 6 &&
		parts[0] == "api" &&
		parts[1] == "runtime" &&
		parts[2] == "v1" &&
		parts[3] == "runs" &&
		parts[4] != "" && parts[4] != "." && parts[4] != ".." &&
		parts[5] == "recovery"
}

func isManagementPath(path string) bool {
	return strings.Contains(path, "/auth/v1/register") ||
		strings.Contains(path, "/settings/") ||
		strings.Contains(path, "/ops/v1/playbooks") ||
		strings.Contains(path, "/users/") ||
		strings.Contains(path, "/policy/") ||
		strings.Contains(path, "/gate/")
}
