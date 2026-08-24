package chat_pipeline

import (
	"SentinelOps/internal/ai/retrieval"
	"context"

	"github.com/cloudwego/eino/components/retriever"
)

func newRetriever(ctx context.Context) (rtr retriever.Retriever, err error) {
	return retrieval.GetRetriever(), nil
}
