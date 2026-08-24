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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.Run(ctx, bootstrap.Options{
		Role:      role,
		ConfigDir: "manifest/config",
	}); err != nil {
		g.Log().Fatalf(ctx, "application bootstrap failed: %v", err)
	}
}
