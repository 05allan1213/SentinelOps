// SentinelOps 安全事件智能研判多智能体协同平台
// 数据流转：外部事件源 → Fetcher抓取 → Extractor提取 → Dedup去重 → MySQL存储 → Indexer向量化 → Milvus检索 → AI分析 → 用户界面
package main

import (
	"context"

	"SentinelOps/internal/ai/ops/engine"
	"SentinelOps/internal/ai/retrieval"
	"SentinelOps/internal/ai/rule"
	toolsops "SentinelOps/internal/ai/tools/ops"
	aitrace "SentinelOps/internal/ai/trace"
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
	"SentinelOps/internal/service/knowledge"
	"SentinelOps/internal/service/scheduler"
	authPkg "SentinelOps/utility/auth"
	"SentinelOps/utility/middleware"

	einocallbacks "github.com/cloudwego/eino/callbacks"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"
)

func frontendHostingEnabled(ctx context.Context) bool {
	return g.Cfg().MustGet(ctx, "server.enable_frontend_hosting", true).Bool()
}

func main() {
	ctx := gctx.New()
	if _, selected, err := appconfig.LoadDirectory("manifest/config"); err != nil {
		g.Log().Fatalf(ctx, "configuration startup check failed: %v", err)
	} else {
		g.Log().Infof(ctx, "configuration loaded: %s", selected)
	}

	// ========== 阶段1：认证与存储初始化 ==========
	// 初始化 JWT 认证（auth.jwt.enabled=true 时生效）
	if secret, err := g.Cfg().Get(ctx, "auth.jwt.secret"); err == nil {
		authPkg.Init(secret.String())
	}

	// 初始化 MySQL（事件/订阅/报告表），GORM 自动迁移，创建默认管理员
	if err := dao.Init(ctx); err != nil {
		g.Log().Warningf(ctx, "database init skipped or failed: %v", err)
	} else {
		dao.SeedAdmin(ctx)
		dao.SeedSettings(ctx)
		dao.SeedTermMappings(ctx)
		// 创建性能优化索引（幂等）
		dao.CreateIndexes(ctx)
		// 确保默认知识库存在
		knowledge.EnsureDefaultBase(ctx)
		// 注册 GORM plugin（慢查询追踪）
		if err := dao.RegisterPlugin(aitrace.NewGORMPlugin()); err != nil {
			g.Log().Warningf(ctx, "register gorm plugin failed: %v", err)
		}
	}

	// ========== 阶段2：AI组件初始化 ==========
	// 预热 Milvus Retriever 单例（向量检索 + 语义缓存）
	if err := retrieval.WarmUp(ctx); err != nil {
		g.Log().Fatalf(ctx, "retrieval warmup failed: %v", err)
	}

	// 加载术语归一化规则到进程内存
	rule.InitTermMappings(ctx)

	// 注册全链路追踪 Callback（全局生效，所有 Eino DAG 节点自动触发）
	einocallbacks.AppendGlobalHandlers(aitrace.NewCallbackHandler())

	// ========== 阶段3：HTTP服务器配置 ==========
	s := g.Server()
	if frontendHostingEnabled(ctx) {
		// 托管前端静态文件（生产构建产物）
		s.SetServerRoot("web/dist")
		// SPA fallback：未匹配到静态文件或 /api 路由时返回 index.html
		s.BindHandler("/*", func(r *ghttp.Request) {
			r.Response.ServeFile("web/dist/index.html")
		})
	}
	s.Group("/api", func(group *ghttp.RouterGroup) {
		// 中间件链：CORS → 响应格式化 → JWT认证 → 限流（限流依赖 JWT 解析的 user_id）
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Middleware(middleware.JWTMiddleware())
		group.Middleware(middleware.RateLimitMiddleware)
		// SSE 接口注册
		group.Bind(chat.NewV1())
		// 绑定其余控制器
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
	// 告警接入路由：使用 API Key 认证（外部设备无法携带 JWT）
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Middleware(middleware.IngestAPIKeyMiddleware())
		group.Bind(ingestctrl.NewV1())
	})

	// ========== 阶段4：后台任务启动 ==========
	// 注入 AI 运维 ChatOps 触发函数
	toolsops.SetTriggerFunc(engine.TriggerForEvent)

	// 启动 AI 运维补偿扫描：定时扫描历史高危/严重事件，对未触发过运维的事件补发触发
	scheduler.RunOpsCompensationScan(ctx)

	// 启动知识库文档异步索引 Worker Pool
	knowledge.StartWorkerPool(ctx)

	// 启动定时调度器：每个订阅按自身 CronExpr 独立调度 + Indexer（间隔由配置 scheduler.index_interval_minutes 决定，默认 10 分钟）
	// 数据流：订阅源 → Fetcher → Extractor → Dedup → MySQL → Indexer → Milvus
	scheduler.Run(ctx)

	s.Run()
}
