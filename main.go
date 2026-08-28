// SentinelOps 安全事件智能研判多智能体协同平台。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"SentinelOps/internal/bootstrap"

	"github.com/gogf/gf/v2/frame/g"
)

func frontendHostingEnabled(ctx context.Context) bool {
	return bootstrap.FrontendHostingEnabled(ctx)
}

func main() {
	role, err := bootstrap.ParseRole(os.Args[1:], os.LookupEnv)
	if err != nil {
		g.Log().Fatalf(context.Background(), "bootstrap role check failed: %v", err)
	}
	// 允许通过环境变量选择配置目录：仓库内 config.local.yaml 保持 fail-closed
	// 本地替换契约，phase43 等隔离验证可用独立目录显式打开测试 Gate。
	configDir := os.Getenv("SENTINELOPS_CONFIG_DIR")
	if configDir == "" {
		configDir = "manifest/config"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.Run(ctx, bootstrap.Options{
		Role:      role,
		ConfigDir: configDir,
	}); err != nil {
		g.Log().Fatalf(ctx, "application bootstrap failed: %v", err)
	}
}
