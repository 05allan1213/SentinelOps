// Package retrieval owns the explicitly warmed retrieval singletons.
package retrieval

import (
	"context"
	"fmt"
	"sync"

	"SentinelOps/internal/ai/embedder"
	"SentinelOps/internal/ai/rerank"
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
	return nil
}

// WarmUp explicitly initializes all retrieval dependencies after application
// configuration has been selected and validated.
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
