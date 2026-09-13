package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestClassifiedErrorHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"gorm not found", gorm.ErrRecordNotFound, http.StatusNotFound},
		{"wrapped gorm not found", fmt.Errorf("报告不存在: %w", gorm.ErrRecordNotFound), http.StatusNotFound},
		{"gerror not found", gerror.NewCode(gcode.CodeNotFound, "知识库不存在"), http.StatusNotFound},
		{"validation", gerror.NewCode(gcode.CodeValidationFailed, "参数不合法"), http.StatusBadRequest},
		{"not supported", gerror.NewCode(gcode.CodeNotSupported, "暂不支持"), http.StatusNotImplemented},
		{"not authorized", gerror.NewCode(gcode.CodeNotAuthorized, "无权限"), http.StatusForbidden},
		{"unclassified business error", errors.New("策略不存在"), 0},
	}
	for _, tc := range cases {
		if got := classifiedErrorHTTPStatus(tc.err); got != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// 限流/鉴权中间件先写入响应体时，ResponseMiddleware 不能追加统一信封，
// 否则客户端收到的是两段 JSON 拼接的非法报文。
func TestResponseMiddlewareKeepsSingleBodyWhenBodyAlreadyWritten(t *testing.T) {
	base := startResponseMiddlewareServer(t)

	res, err := http.Get(base + "/api/rate-limited")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusTooManyRequests)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("rate limited body is not a single JSON document: %v body=%q", err, string(body))
	}
	if decoded["code"] != float64(http.StatusTooManyRequests) {
		t.Fatalf("rate limited body = %v, want code=429", decoded)
	}
}

func TestResponseMiddlewareMapsNotSupportedErrorToStatus(t *testing.T) {
	base := startResponseMiddlewareServer(t)

	res, err := http.Get(base + "/api/not-supported")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNotImplemented)
	}
	var decoded struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("not supported body is not JSON: %v body=%q", err, string(body))
	}
	if decoded.Message != "暂不支持" {
		t.Fatalf("message = %q, want 暂不支持", decoded.Message)
	}
}

// startResponseMiddlewareServer 复现真实中间件顺序：业务中间件先写响应体，
// 再由 ResponseMiddleware 统一包装 handler 返回值。
func startResponseMiddlewareServer(t *testing.T) string {
	t.Helper()
	s := g.Server("response-middleware-" + uuid.NewString())
	s.SetDumpRouterMap(false)
	s.SetPort(0)
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(ResponseMiddleware)
		group.GET("/rate-limited", func(r *ghttp.Request) {
			r.Response.WriteHeader(http.StatusTooManyRequests)
			r.Response.WriteJson(g.Map{"code": http.StatusTooManyRequests, "message": "请求过于频繁，请稍后重试"})
		})
		group.GET("/not-supported", func(context.Context, *notSupportedRequest) (*notSupportedResponse, error) {
			return nil, gerror.NewCode(gcode.CodeNotSupported, "暂不支持")
		})
	})
	s.Start()
	t.Cleanup(func() { _ = s.Shutdown() })
	deadline := time.Now().Add(2 * time.Second)
	for s.GetListenedPort() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.GetListenedPort() == 0 {
		t.Fatal("GoFrame test server did not start")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", s.GetListenedPort())
}

type notSupportedRequest struct{}

type notSupportedResponse struct{}
