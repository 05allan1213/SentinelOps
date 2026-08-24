package actions

import (
	"context"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func TestNewNginxBlocklistManagerReadsSoarAIopsConfig(t *testing.T) {
	ctx := context.Background()
	cfgContent := `{
		"soar": {
			"ai_ops": {
				"block_ip_backend": "nginx",
				"nginx_blocklist_path": "/tmp/blocked_ips.conf",
				"nginx_container": "nginx"
			}
		}
	}`

	adapter, err := gcfg.NewAdapterContent(cfgContent)
	if err != nil {
		t.Fatalf("创建测试配置失败: %v", err)
	}
	oldAdapter := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(oldAdapter)

	mgr := NewNginxBlocklistManager(ctx)
	if mgr.backend != "nginx" {
		t.Fatalf("backend 读取错误，got=%q want=%q", mgr.backend, "nginx")
	}
	if mgr.blocklistPath != "/tmp/blocked_ips.conf" {
		t.Fatalf("blocklistPath 读取错误，got=%q", mgr.blocklistPath)
	}
	if mgr.containerName != "nginx" {
		t.Fatalf("containerName 读取错误，got=%q", mgr.containerName)
	}
}
