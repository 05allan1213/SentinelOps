package eval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductionRuntimeAdapterUsesDurableHTTPAPI(t *testing.T) {
	var submittedSessionID string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/chat/v2/runs" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization header was not forwarded")
		}
		var body createRunRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Query != "production path" || body.SessionID == "" {
			t.Fatalf("body = %+v", body)
		}
		submittedSessionID = body.SessionID
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"OK","data":{"run_id":"run-http","status":"pending"}}`))
	}))
	defer server.Close()

	adapter, err := NewHTTPRuntimeAdapter(server.URL, server.Client(), http.Header{"Authorization": []string{"Bearer test-token"}})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := adapter.Submit(context.Background(), EvalCase{ID: strings.Repeat("long-case-id-", 16), Query: "production path"})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if handle.RunID != "run-http" || handle.SessionID == "" {
		t.Fatalf("handle = %+v", handle)
	}
	if len(submittedSessionID) > 64 || !strings.HasPrefix(submittedSessionID, "eval-") {
		t.Fatalf("generated session_id = %q", submittedSessionID)
	}
	adapter.Close()
	if adapter.Header != nil {
		t.Fatalf("Close() retained request headers: %#v", adapter.Header)
	}
}

func TestProductionRuntimeAdapterRejectsCredentialedBaseURL(t *testing.T) {
	if _, err := NewHTTPRuntimeAdapter("https://user:password@example.com", http.DefaultClient, nil); err == nil {
		t.Fatal("credentialed production API base URL was accepted")
	}
}

func TestProductionRuntimeAdapterDoesNotEchoErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "Bearer should-never-appear", http.StatusUnauthorized)
	}))
	defer server.Close()
	adapter, err := NewHTTPRuntimeAdapter(server.URL, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Submit(context.Background(), EvalCase{ID: "failure", Query: "safe"})
	if err == nil || err.Error() != "production Run API returned HTTP 401" {
		t.Fatalf("error = %v", err)
	}
}
