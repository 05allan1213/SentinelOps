// Package retrieval 管理需要显式预热的检索单例。
package retrieval

import (
	"context"
	"fmt"
	"sync"

	"SentinelOps/internal/ai/embedder"
	"SentinelOps/internal/ai/rerank"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/milvus"
	clientutil "SentinelOps/utility/client"

	"github.com/cloudwego/eino/components/embedding"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	milvuscli "github.com/milvus-io/milvus-sdk-go/v2/client"
	goredis "github.com/redis/go-redis/v9"
)

var (
	once    sync.Once
	initErr error

	globalEmbedder embedding.Embedder
	globalMilvus   milvuscli.Client
	globalRedis    *goredis.Client
	globalConfig   Config
)

func initialize(ctx context.Context) error {
	// 配置必须先由主程序加载；随后按依赖顺序初始化模型、存储和重排客户端。
	applicationConfig, err := appconfig.Current()
	if err != nil {
		return fmt.Errorf("read application configuration: %w", err)
	}
	embeddingRoute, ok := applicationConfig.Routing.Embedding["default"]
	if !ok {
		return fmt.Errorf("routing.embedding.default is not configured")
	}
	_, embeddingModel, err := applicationConfig.Resolve(embeddingRoute)
	if err != nil {
		return fmt.Errorf("resolve embedding model: %w", err)
	}
	eb, err := embedder.NewDenseEmbedder(ctx)
	if err != nil {
		return fmt.Errorf("initialize embedder: %w", err)
	}
	cli, err := milvus.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("initialize Milvus: %w", err)
	}
	redisClient, err := clientutil.GetRedisClient(ctx)
	if err != nil {
		return fmt.Errorf("initialize Redis: %w", err)
	}
	if _, err := rerank.GetClient(ctx); err != nil {
		return fmt.Errorf("initialize reranker: %w", err)
	}

	globalEmbedder = eb
	globalMilvus = cli
	globalRedis = redisClient
	globalConfig = LoadConfig(ctx)
	globalConfig.EmbeddingModel = embeddingModel.ModelID
	return nil
}

// WarmUp 在应用配置完成选择和校验后，显式初始化全部检索依赖。
func WarmUp(ctx context.Context) error {
	once.Do(func() {
		initErr = initialize(ctx)
	})
	return initErr
}

func GetRetriever() einoretriever.Retriever {
	return New(globalMilvus, globalEmbedder, globalRedis, globalConfig)
}

func GetEventsRetriever() einoretriever.Retriever {
	cfg := globalConfig
	cfg.Partition = "events"
	return New(globalMilvus, globalEmbedder, globalRedis, cfg)
}

func GetDocumentsRetriever() einoretriever.Retriever {
	cfg := globalConfig
	cfg.Partition = "documents"
	return New(globalMilvus, globalEmbedder, globalRedis, cfg)
}
