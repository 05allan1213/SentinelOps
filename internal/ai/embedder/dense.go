package embedder

import (
	"context"
	"fmt"

	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/milvus"

	"github.com/cloudwego/eino-ext/components/embedding/dashscope"
	"github.com/cloudwego/eino/components/embedding"
)

const bailianCompatibleEmbeddingEndpoint = "https://dashscope.aliyuncs.com/compatible-mode/v1"

type dimensionCheckingEmbedder struct {
	inner     embedding.Embedder
	dimension int
}

func (e *dimensionCheckingEmbedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	vectors, err := e.inner.EmbedStrings(ctx, texts, opts...)
	if err != nil {
		return nil, err
	}
	for i, vector := range vectors {
		if len(vector) != e.dimension {
			return nil, fmt.Errorf("embedding vector %d has dimension %d, want %d", i, len(vector), e.dimension)
		}
	}
	return vectors, nil
}

// NewDenseEmbedder creates the embedding model selected by
// routing.embedding.default and enforces the Milvus 2048-dimension contract.
func NewDenseEmbedder(ctx context.Context) (embedding.Embedder, error) {
	cfg, err := appconfig.Current()
	if err != nil {
		return nil, err
	}
	route := cfg.Routing.Embedding["default"]
	provider, model, err := cfg.Resolve(route)
	if err != nil {
		return nil, err
	}
	if model.Driver != "dashscope" {
		return nil, fmt.Errorf("routing.embedding.default uses driver %s, want dashscope", model.Driver)
	}
	if provider.Endpoints.DashScope != bailianCompatibleEmbeddingEndpoint {
		return nil, fmt.Errorf("dashscope embedder supports endpoint %s, got %s", bailianCompatibleEmbeddingEndpoint, provider.Endpoints.DashScope)
	}
	if model.Dimension != milvus.EmbeddingDim {
		return nil, fmt.Errorf("embedding dimension %d does not match Milvus dimension %d", model.Dimension, milvus.EmbeddingDim)
	}
	inner, err := dashscope.NewEmbedder(ctx, &dashscope.EmbeddingConfig{
		Model:      model.ModelID,
		APIKey:     provider.APIKey,
		Dimensions: &model.Dimension,
	})
	if err != nil {
		return nil, err
	}
	return &dimensionCheckingEmbedder{inner: inner, dimension: model.Dimension}, nil
}
