package bootstrap

import (
	"context"

	"SentinelOps/internal/ai/agent/skill_pipeline"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
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
	dao "SentinelOps/internal/dao/mysql"
	chatsvc "SentinelOps/internal/service/chat"
	knowledgesvc "SentinelOps/internal/service/knowledge"
	"SentinelOps/utility/middleware"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// FrontendHostingEnabled 返回 API 是否托管前端静态文件。
func FrontendHostingEnabled(ctx context.Context) bool {
	return g.Cfg().MustGet(ctx, "server.enable_frontend_hosting", true).Bool()
}

func bindAPI(ctx context.Context) error {
	config, err := appconfig.Current()
	if err != nil {
		return err
	}
	evaluator, err := airuntime.NewGateEvaluator(airuntime.StaticGateCaps(config), dao.GetSettings)
	if err != nil {
		return err
	}
	durableService, err := newDurableAPIService(ctx, config, evaluator)
	if err != nil {
		return err
	}
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
		group.Middleware(middleware.AuthorizationMiddleware())
		group.Middleware(middleware.RateLimitMiddleware)
		group.Bind(chat.NewV1(durableService))
		group.Bind(chat.NewV2(durableService))
		group.Bind(auth.NewV1())
		group.Bind(event.NewV1(evaluator))
		group.Bind(subscription.NewV1())
		group.Bind(report.NewV1())
		group.Bind(settingsctrl.NewV1())
		group.Bind(termmapping.NewV1())
		group.Bind(tracectrl.NewV1())
		group.Bind(knowledgectrl.NewV1())
		group.Bind(ragevalctrl.NewV1())
		group.Bind(opsctrl.NewV1(evaluator))
	})
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Middleware(middleware.AuthDisabledWriteGuard())
		group.Middleware(middleware.IngestAPIKeyMiddleware())
		group.Bind(ingestctrl.NewV1())
	})
	return nil
}

func newDurableAPIService(ctx context.Context, config *appconfig.Config, evaluator *airuntime.GateEvaluator) (*chatsvc.DurableService, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}
	return chatsvc.NewDurableService(chatsvc.DurableServiceConfig{
		Store: workflow.NewGORMStore(db),
		SnapshotLoader: func(loadCtx context.Context) (airuntime.FrozenRuntimeSnapshot, error) {
			gates, loadErr := evaluator.Current(loadCtx)
			if loadErr != nil {
				return airuntime.FrozenRuntimeSnapshot{}, loadErr
			}
			if !gates.Enabled(airuntime.GateAgentRuntimeEnabled) || !gates.Enabled(airuntime.GateAgentRuntimeAcceptNewRuns) {
				return airuntime.FrozenRuntimeSnapshot{}, chatsvc.ErrDurableRunGateClosed
			}
			var skillSnapshots []airuntime.SkillSnapshot
			if gates.Enabled(airuntime.GateSkillEnabled) {
				skillSnapshots, loadErr = skill_pipeline.BuildConfiguredSkillSnapshots(loadCtx, config)
				if loadErr != nil {
					return airuntime.FrozenRuntimeSnapshot{}, loadErr
				}
			}
			return airuntime.BuildDurableRuntimeSnapshotWithSkillsAndGates(config, skillSnapshots, gates)
		},
	})
}

func serveAPI(ctx context.Context) error {
	// API 角色也必须启动知识索引队列：知识库上传通过 HTTP 进入本进程的
	// 全局队列，若只由 Worker 角色启动，API 上传的文档会永远停在 pending。
	knowledgesvc.StartWorkerPool(ctx)
	g.Server().Run()
	return nil
}
