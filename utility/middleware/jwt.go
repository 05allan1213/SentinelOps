package middleware

import (
	"context"
	"strings"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/utility/auth"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// JWTMiddleware JWT 认证中间件。认证关闭时注入只读 viewer；登录接口始终放行。
// allowedRoles 为空时不做角色校验，非空时要求规范化后的角色在列表内。
func JWTMiddleware(allowedRoles ...string) ghttp.HandlerFunc {
	return jwtMiddleware(func(ctx context.Context) bool {
		enabled, _ := g.Cfg().Get(ctx, "auth.jwt.enabled")
		return enabled.Bool()
	}, allowedRoles...)
}

func jwtMiddleware(authEnabled func(context.Context) bool, allowedRoles ...string) ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		ctx := r.Context()
		if !authEnabled(ctx) {
			setRequestIdentity(r, policy.DisabledIdentity())
			r.Middleware.Next()
			return
		}
		// 登录接口是唯一无需既有身份的用户端入口；注册属于用户管理写入。
		if strings.Contains(r.URL.Path, "/auth/v1/login") {
			r.Middleware.Next()
			return
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			r.Response.WriteHeader(401)
			r.Response.WriteJson(g.Map{"message": "missing Authorization header"})
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			r.Response.WriteHeader(401)
			r.Response.WriteJson(g.Map{"message": "invalid Authorization format"})
			return
		}
		claims, err := auth.Parse(parts[1])
		if err != nil {
			r.Response.WriteHeader(401)
			r.Response.WriteJson(g.Map{"message": "invalid token"})
			return
		}
		identity, err := policy.NewIdentity(claims.UserID, claims.Username, claims.Role)
		if err != nil {
			r.Response.WriteHeader(403)
			r.Response.WriteJson(g.Map{"message": "invalid identity"})
			return
		}
		if len(allowedRoles) > 0 {
			allowed := false
			for _, role := range allowedRoles {
				normalized, normalizeErr := policy.NormalizeRole(role)
				if normalizeErr == nil && identity.Role == normalized {
					allowed = true
					break
				}
			}
			if !allowed {
				r.Response.WriteHeader(403)
				r.Response.WriteJson(g.Map{"message": "insufficient permission"})
				return
			}
		}
		setRequestIdentity(r, identity)
		r.Middleware.Next()
	}
}

func setRequestIdentity(r *ghttp.Request, identity policy.Identity) {
	r.SetCtx(policy.WithIdentity(r.Context(), identity))
	r.SetCtxVar("auth_user_id", identity.UserID)
	r.SetCtxVar("auth_username", identity.Username)
	r.SetCtxVar("auth_role", string(identity.Role))
}
