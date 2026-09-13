package middleware

import (
	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/net/ghttp"
)

// IngestAPIKeyMiddleware 校验 X-API-Key header，用于外部设备接入端点。
// API Key 存储在 settings 表 key=ingest.api_key，未配置时拒绝所有请求。
func IngestAPIKeyMiddleware() ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		ctx := r.Context()
		key := r.Header.Get("X-API-Key")
		if key == "" {
			r.Response.WriteHeader(401)
			r.Response.WriteJson(map[string]string{"message": "missing X-API-Key header"})
			return
		}
		stored, err := dao.GetSetting(ctx, "ingest.api_key")
		if err != nil || stored == "" || stored != key {
			r.Response.WriteHeader(401)
			r.Response.WriteJson(map[string]string{"message": "invalid API key"})
			return
		}
		r.Middleware.Next()
	}
}
