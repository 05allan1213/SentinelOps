package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/schema"
)

func TestClientUsesProviderSelectedByRouting(t *testing.T) {
	cfg, err := appconfig.Parse([]byte(`
providers:
  provider_a:
    api_key: key-a
    endpoints: {dashscope_rerank: https://provider-a.example/v1}
  provider_b:
    api_key: key-b
    endpoints: {dashscope_rerank: https://provider-b.example/v1}
model_catalog:
  provider_a/rerank:
    model_id: model-a
    driver: dashscope_rerank
    capabilities: [rerank]
  provider_b/rerank:
    model_id: model-b
    driver: dashscope_rerank
    capabilities: [rerank]
routing:
  rerank:
    default:
      model: provider_b/rerank
      options: {instruct: configured instruction}
`))
	if err != nil {
		t.Fatal(err)
	}
	client, err := clientFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if client.apiKey != "key-b" || client.baseURL != "https://provider-b.example/v1" || client.model != "model-b" {
		t.Fatalf("resolved client = %#v, want provider_b configuration", client)
	}
	if client.instruct != "configured instruction" {
		t.Fatalf("instruct = %q, want configured instruction", client.instruct)
	}
}

func TestRerankUsesFlatCompatibleRequestAndBearerAuth(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9}],"usage":{"total_tokens":17}}`))
	}))
	defer server.Close()

	client := &Client{apiKey: "secret", baseURL: server.URL, model: "qwen3-rerank", instruct: "rank"}
	result := client.Rerank(context.Background(), "query", []*schema.Document{{Content: "a"}, {Content: "b"}}, 1)
	if len(result) != 1 || result[0].Doc.Content != "b" {
		t.Fatalf("result = %#v", result)
	}
	for _, key := range []string{"model", "query", "documents", "top_n", "instruct"} {
		if _, ok := body[key]; !ok {
			t.Errorf("flat request missing %q: %#v", key, body)
		}
	}
	if _, nested := body["input"]; nested {
		t.Fatalf("request must not use legacy input wrapper: %#v", body)
	}
}
