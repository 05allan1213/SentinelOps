package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"SentinelOps/utility/middleware"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
)

// Exercise the real GoFrame buffer + canonical envelope, not a controller-only call.
func TestRuntimeStatusPreservesSingleJSONEnvelope(t *testing.T) {
	server := g.Server("runtime-json-" + uuid.NewString())
	server.SetDumpRouterMap(false)
	server.SetPort(0)
	server.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.ResponseMiddleware)
		group.GET("/status", func(r *ghttp.Request) { setStatus(r.Context(), r.Get("status").Int()) })
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	deadline := time.Now().Add(2 * time.Second)
	for server.GetListenedPort() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	for _, status := range []int{202, 400, 403, 404, 409, 422, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status?status=%d", server.GetListenedPort(), status))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != status {
				t.Fatalf("status=%d want=%d", response.StatusCode, status)
			}
			var envelope struct {
				Message string `json:"message"`
				Data    any    `json:"data"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatalf("invalid JSON envelope %q: %v", body, err)
			}
			if envelope.Message != "OK" {
				t.Fatalf("unexpected envelope: %s", body)
			}
		})
	}
}
