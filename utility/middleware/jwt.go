package middleware

import (
	"strings"

	"SentinelOps/utility/auth"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// JWTMiddleware JWT 认证中间件，未配置或未启用时放行。登录接口 /auth/v1/login 始终放行。
// allowedRoles 为空时不做角色校验，非空时要求 token 携带的 role 在列表内。
func JWTMiddleware(allowedRoles ...string) ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		ctx := r.Context()
		enabled, _ := g.Cfg().Get(ctx, "auth.jwt.enabled")
		if !enabled.Bool() {
			r.Middleware.Next()
			return
		}
		// 登录/注册接口不校验 JWT
		if strings.Contains(r.URL.Path, "/auth/v1/login") || strings.Contains(r.URL.Path, "/auth/v1/register") {
			r.Middleware.Next()
			return
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			r.Response.WriteStatus(401)
			r.Response.WriteJson(g.Map{"message": "missing Authorization header"})
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			r.Response.WriteStatus(401)
			r.Response.WriteJson(g.Map{"message": "invalid Authorization format"})
			return
		}
		claims, err := auth.Parse(parts[1])
		if err != nil {
			r.Response.WriteStatus(401)
			r.Response.WriteJson(g.Map{"message": "invalid token"})
			return
		}
		if len(allowedRoles) > 0 {
			allowed := false
			for _, role := range allowedRoles {
				if claims.Role == role {
					allowed = true
					break
				}
			}
			if !allowed {
				r.Response.WriteStatus(403)
				r.Response.WriteJson(g.Map{"message": "insufficient permission"})
				return
			}
		}
		r.SetCtxVar("auth_user_id", claims.UserID)
		r.SetCtxVar("auth_username", claims.Username)
		r.SetCtxVar("auth_role", claims.Role)
		r.Middleware.Next()
	}
}
