package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func TestGithubGetAddsAuthorizationHeaderWhenTokenConfigured(t *testing.T) {
	ctx := context.Background()
	adapter, err := gcfg.NewAdapterContent(`{
		"tools": {
			"github": {
				"token": "test-github-token"
			}
		}
	}`)
	if err != nil {
		t.Fatalf("创建测试配置失败: %v", err)
	}
	oldAdapter := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(oldAdapter)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-github-token" {
			t.Fatalf("Authorization 头错误，got=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"ok"})
	}))
	defer server.Close()

	var dst []string
	if err := githubGet(ctx, server.URL, &dst); err != nil {
		t.Fatalf("githubGet 返回错误: %v", err)
	}
}

func TestGithubGetDoesNotAddAuthorizationHeaderWhenTokenEmpty(t *testing.T) {
	ctx := context.Background()
	adapter, err := gcfg.NewAdapterContent(`{
		"tools": {
			"github": {
				"token": ""
			}
		}
	}`)
	if err != nil {
		t.Fatalf("创建测试配置失败: %v", err)
	}
	oldAdapter := g.Cfg().GetAdapter()
	g.Cfg().SetAdapter(adapter)
	defer g.Cfg().SetAdapter(oldAdapter)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("未配置 token 时不应发送 Authorization 头，got=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"ok"})
	}))
	defer server.Close()

	var dst []string
	if err := githubGet(ctx, server.URL, &dst); err != nil {
		t.Fatalf("githubGet 返回错误: %v", err)
	}
}
