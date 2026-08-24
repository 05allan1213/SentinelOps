package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/components/embedding"
)

type fakeEmbedder struct {
	vectors [][]float64
}

func TestDenseEmbedderUsesEndpointSelectedByRouting(t *testing.T) {
	t.Setenv("TEST_EMBEDDING_KEY", "provider-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("request path = %q, want /embeddings", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer provider-key" {
			t.Errorf("Authorization = %q, want configured provider key", got)
		}
		vector := make([]float64, 2048)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data":  []any{map[string]any{"embedding": vector, "index": 0}},
			"model": "configured-embedding",
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	cfg, err := appconfig.Parse([]byte(`
providers:
  provider_custom:
    secret_ref: env:TEST_EMBEDDING_KEY
    endpoints:
      openai_compatible_embedding: ` + server.URL + `
model_catalog:
  provider_custom/embedding:
    model_id: configured-embedding
    driver: openai_compatible_embedding
    capabilities: [embedding]
    dimension: 2048
    pricing: {revision: test-v1, currency: CNY, unit: per_million_tokens, input: 0.5}
routing:
  embedding:
    default: {model: provider_custom/embedding}
`))
	if err != nil {
		t.Fatal(err)
	}
	appconfig.SetCurrent(cfg)
	model, err := NewDenseEmbedder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := model.EmbedStrings(context.Background(), []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 2048 {
		t.Fatalf("embedding shape = %d x %d, want 1 x 2048", len(vectors), len(vectors[0]))
	}
}

func (f fakeEmbedder) EmbedStrings(context.Context, []string, ...embedding.Option) ([][]float64, error) {
	return f.vectors, nil
}

func TestDimensionCheckingEmbedderRequires2048Dimensions(t *testing.T) {
	good := &dimensionCheckingEmbedder{inner: fakeEmbedder{vectors: [][]float64{make([]float64, 2048)}}, dimension: 2048}
	if _, err := good.EmbedStrings(context.Background(), []string{"ok"}); err != nil {
		t.Fatalf("valid vector rejected: %v", err)
	}

	bad := &dimensionCheckingEmbedder{inner: fakeEmbedder{vectors: [][]float64{make([]float64, 1024)}}, dimension: 2048}
	if _, err := bad.EmbedStrings(context.Background(), []string{"bad"}); err == nil || !strings.Contains(err.Error(), "2048") {
		t.Fatalf("dimension error = %v, want 2048", err)
	}
}
