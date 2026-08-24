package middleware

import "github.com/gogf/gf/v2/net/ghttp"

// CORSMiddleware 处理跨域请求，允许所有来源（开发/内网环境）。
func CORSMiddleware(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}
