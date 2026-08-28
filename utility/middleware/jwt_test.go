package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/utility/auth"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
)

func startRBACServer(t *testing.T, authEnabled bool) string {
	t.Helper()
	s := g.Server("phase05-" + uuid.NewString())
	s.SetDumpRouterMap(false)
	s.SetPort(0)
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(jwtMiddleware(func(context.Context) bool { return authEnabled }))
		group.Middleware(AuthorizationMiddleware())
		group.ALL("/*path", func(r *ghttp.Request) {
			identity, err := policy.IdentityFromContext(r.Context())
			if err != nil {
				r.Response.WriteStatus(http.StatusInternalServerError)
				return
			}
			response := identity.UserID + ":" + string(identity.Role)
			if clientUserID := r.Get("user_id").String(); clientUserID != "" {
				response += "|" + clientUserID
			}
			r.Response.Write(response)
		})
	})
	s.Group("/ingest", func(group *ghttp.RouterGroup) {
		group.Middleware(authDisabledWriteGuard(func(context.Context) bool { return authEnabled }))
		group.POST("/push", func(r *ghttp.Request) { r.Response.Write("ingested") })
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

func doRequest(t *testing.T, method, url, token, clientUserID string) (int, string) {
	t.Helper()
	var requestBody io.Reader
	if clientUserID != "" && method == http.MethodPost {
		requestBody = strings.NewReader(fmt.Sprintf(`{"user_id":%q}`, clientUserID))
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, requestBody)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if clientUserID != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, strings.TrimSpace(string(responseBody))
}

func TestJWTContextIsAuthoritative(t *testing.T) {
	if err := auth.Init([]byte("phase05-test-jwt-secret-with-sufficient-length")); err != nil {
		t.Fatal(err)
	}
	token, err := auth.Generate("server-user", "alice", "user", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	baseURL := startRBACServer(t, true)
	status, body := doRequest(t, http.MethodGet, baseURL+"/api/trace/v1/list", token, "attacker")
	if status != http.StatusOK || body != "server-user:viewer" {
		t.Fatalf("server identity not authoritative: status=%d body=%q", status, body)
	}
}

func TestClientUserIDCannotOverrideJWT(t *testing.T) {
	if err := auth.Init([]byte("phase05-test-jwt-secret-with-sufficient-length")); err != nil {
		t.Fatal(err)
	}
	token, err := auth.Generate("jwt-user", "alice", "viewer", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	baseURL := startRBACServer(t, true)
	status, body := doRequest(t, http.MethodPost, baseURL+"/api/chat/v1/chat", token, "client-user")
	if status != http.StatusOK || body != "jwt-user:viewer|client-user" {
		t.Fatalf("client user id overrode JWT: status=%d body=%q", status, body)
	}
}

func TestRoleMatrixHTTP(t *testing.T) {
	if err := auth.Init([]byte("phase05-test-jwt-secret-with-sufficient-length")); err != nil {
		t.Fatal(err)
	}
	baseURL := startRBACServer(t, true)
	tokens := make(map[string]string)
	for _, role := range []string{"viewer", "operator", "approver", "admin"} {
		token, err := auth.Generate(role+"-user", role, role, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		tokens[role] = token
	}

	tests := []struct {
		name   string
		role   string
		method string
		path   string
		status int
	}{
		{"viewer scoped read", "viewer", http.MethodGet, "/api/trace/v1/list", http.StatusOK},
		{"viewer read-only run", "viewer", http.MethodPost, "/api/chat/v1/chat", http.StatusOK},
		{"viewer durable read-only run", "viewer", http.MethodPost, "/api/chat/v2/runs", http.StatusOK},
		{"viewer business write", "viewer", http.MethodPost, "/api/event/v1/create", http.StatusForbidden},
		{"operator business write", "operator", http.MethodPost, "/api/event/v1/create", http.StatusOK},
		{"operator approval", "operator", http.MethodPost, "/api/approval/v1/decide", http.StatusForbidden},
		{"operator plural approval", "operator", http.MethodPost, "/api/ops/v1/approvals/id/approve", http.StatusForbidden},
		{"operator plural rejection", "operator", http.MethodPost, "/api/ops/v1/approvals/id/reject", http.StatusForbidden},
		{"approver approval", "approver", http.MethodPost, "/api/approval/v1/decide", http.StatusOK},
		{"approver plural approval", "approver", http.MethodPost, "/api/ops/v1/approvals/id/approve", http.StatusOK},
		{"approver plural rejection", "approver", http.MethodPost, "/api/ops/v1/approvals/id/reject", http.StatusOK},
		{"approver management", "approver", http.MethodPost, "/api/settings/v1/general", http.StatusForbidden},
		{"admin management", "admin", http.MethodPost, "/api/settings/v1/general", http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, _ := doRequest(t, test.method, baseURL+test.path, tokens[test.role], "")
			if status != test.status {
				t.Fatalf("%s %s as %s status=%d, want %d", test.method, test.path, test.role, status, test.status)
			}
		})
	}
}

func TestAuthDisabledInjectsViewer(t *testing.T) {
	baseURL := startRBACServer(t, false)
	status, body := doRequest(t, http.MethodGet, baseURL+"/api/trace/v1/list", "", "client-user")
	if status != http.StatusOK || body != policy.DisabledUserID+":viewer" {
		t.Fatalf("auth-disabled identity mismatch: status=%d body=%q", status, body)
	}
}

func TestAuthDisabledRejectsBusinessWrites(t *testing.T) {
	baseURL := startRBACServer(t, false)
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/event/v1/create"},
		{http.MethodPost, "/api/event/v1/analyze/delete"},
		{http.MethodPost, "/api/chat/v1/rollback"},
		{http.MethodPost, "/api/ops/v1/runs/direct"},
		{http.MethodDelete, "/api/trace/v1/batch_delete"},
		{http.MethodDelete, "/api/rageval/v1/traces"},
		{http.MethodPost, "/api/settings/v1/general"},
		{http.MethodPost, "/api/approval/v1/decide"},
		{http.MethodPost, "/api/ops/v1/approvals/id/approve"},
		{http.MethodPost, "/api/ops/v1/approvals/id/reject"},
	} {
		status, _ := doRequest(t, route.method, baseURL+route.path, "", "")
		if status != http.StatusForbidden {
			t.Fatalf("auth-disabled write %s %s status=%d, want %d", route.method, route.path, status, http.StatusForbidden)
		}
	}
	status, _ := doRequest(t, http.MethodPost, baseURL+"/api/chat/v1/chat", "", "")
	if status != http.StatusOK {
		t.Fatalf("auth-disabled read-only Run status=%d, want %d", status, http.StatusOK)
	}
	status, _ = doRequest(t, http.MethodPost, baseURL+"/ingest/push", "", "")
	if status != http.StatusForbidden {
		t.Fatalf("auth-disabled machine write status=%d, want %d", status, http.StatusForbidden)
	}
}
