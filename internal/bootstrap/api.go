package bootstrap

import (
	"context"

	"SentinelOps/internal/controller/auth"
	"SentinelOps/internal/controller/chat"
	"SentinelOps/internal/controller/event"
	ingestctrl "SentinelOps/internal/controller/ingest"
	knowledgectrl "SentinelOps/internal/controller/knowledge"
	opsctrl "SentinelOps/internal/controller/ops"
	ragevalctrl "SentinelOps/internal/controller/rageval"
	"SentinelOps/internal/controller/report"
	settingsctrl "SentinelOps/internal/controller/settings"
	"SentinelOps/internal/controller/subscription"
	termmapping "SentinelOps/internal/controller/term_mapping"
	tracectrl "SentinelOps/internal/controller/trace"
	"SentinelOps/utility/middleware"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// FrontendHostingEnabled 返回 API 是否托管前端静态文件。
func FrontendHostingEnabled(ctx context.Context) bool {
	return g.Cfg().MustGet(ctx, "server.enable_frontend_hosting", true).Bool()
}

func bindAPI(ctx context.Context) error {
	s := g.Server()
	if FrontendHostingEnabled(ctx) {
		s.SetServerRoot("web/dist")
		s.BindHandler("/*", func(r *ghttp.Request) {
			r.Response.ServeFile("web/dist/index.html")
		})
	}
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Middleware(middleware.JWTMiddleware())
		group.Middleware(middleware.RateLimitMiddleware)
		group.Bind(chat.NewV1())
		group.Bind(auth.NewV1())
		group.Bind(event.NewV1())
		group.Bind(subscription.NewV1())
		group.Bind(report.NewV1())
		group.Bind(settingsctrl.NewV1())
		group.Bind(termmapping.NewV1())
		group.Bind(tracectrl.NewV1())
		group.Bind(knowledgectrl.NewV1())
		group.Bind(ragevalctrl.NewV1())
		group.Bind(opsctrl.NewV1())
	})
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Middleware(middleware.IngestAPIKeyMiddleware())
		group.Bind(ingestctrl.NewV1())
	})
	return nil
}

func serveAPI(context.Context) error {
	g.Server().Run()
	return nil
}
