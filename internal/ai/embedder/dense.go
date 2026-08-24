package embedder

import (
	"context"
	"fmt"
	"net/http"

	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/milvus"

	openaiacl "github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/cloudwego/eino/components/embedding"
)

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
		// 向量库 Schema 固定为 2048 维，响应异常必须在写入 Milvus 前失败。
		if len(vector) != e.dimension {
			return nil, fmt.Errorf("embedding vector %d has dimension %d, want %d", i, len(vector), e.dimension)
		}
	}
	return vectors, nil
}

// NewDenseEmbedder 根据 routing.embedding.default 创建向量模型，并校验 Milvus
// 所需的 2048 维契约。
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
	if model.Dimension != milvus.EmbeddingDim {
		return nil, fmt.Errorf("embedding dimension %d does not match Milvus dimension %d", model.Dimension, milvus.EmbeddingDim)
	}
	// 当前 Driver 使用 OpenAI-compatible Embedding 协议；供应商、端点和模型均由
	// provider/model 路由决定，适配层不绑定某个云厂商。
	inner, err := openaiacl.NewEmbeddingClient(ctx, &openaiacl.EmbeddingConfig{
		BaseURL:    provider.Endpoints.DashScope,
		APIKey:     provider.APIKey,
		HTTPClient: http.DefaultClient,
		Model:      model.ModelID,
		Dimensions: &model.Dimension,
	})
	if err != nil {
		return nil, err
	}
	return &dimensionCheckingEmbedder{inner: inner, dimension: model.Dimension}, nil
}
