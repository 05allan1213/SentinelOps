package main

import (
	"context"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func TestFrontendHostingEnabledDefaultsToTrue(t *testing.T) {
	ctx := context.Background()
	adapter, err := gcfg.NewAdapterContent(`{}`)
	if err != nil {
		t.Fatalf("创建测试配置失败: %v", err)
	}
	oldAdapter := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(oldAdapter)

	if !frontendHostingEnabled(ctx) {
		t.Fatal("默认应启用前端静态托管")
	}
}

func TestFrontendHostingEnabledReadsServerConfig(t *testing.T) {
	ctx := context.Background()
	adapter, err := gcfg.NewAdapterContent(`{
		"server": {
			"enable_frontend_hosting": false
		}
	}`)
	if err != nil {
		t.Fatalf("创建测试配置失败: %v", err)
	}
	oldAdapter := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(oldAdapter)

	if frontendHostingEnabled(ctx) {
		t.Fatal("server.enable_frontend_hosting=false 时不应启用前端静态托管")
	}
}
