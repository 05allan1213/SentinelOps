package base

import (
	"context"

	"SentinelOps/internal/ai/rerank"
	"SentinelOps/internal/ai/retrieval"
	"SentinelOps/internal/ai/rewrite"
	"SentinelOps/internal/ai/rule"
	"SentinelOps/internal/ai/split"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// RetrievalOptions 控制专业 Agent 共享检索阶段的查询改写与拆分行为。
type RetrievalOptions struct {
	RewriteEnabled bool
	SplitEnabled   bool
}

type retrievalDependencies struct {
	normalize       func(context.Context, string) string
	rewriteQuery    func(context.Context, string, []*schema.Message) string
	rewriteAndSplit func(context.Context, string, []*schema.Message) (string, []string)
	splitQuestions  func(context.Context, string) []string
	retriever       func() einoretriever.Retriever
	rerank          func(context.Context, string, []*schema.Document, int) []*schema.Document
}

func productionRetrievalDependencies() retrievalDependencies {
	return retrievalDependencies{
		normalize:       rule.Normalize,
		rewriteQuery:    rewrite.RewriteQuery,
		rewriteAndSplit: rewrite.RewriteAndSplit,
		splitQuestions:  split.SplitQuestions,
		retriever:       retrieval.GetRetriever,
		rerank: func(ctx context.Context, query string, docs []*schema.Document, topN int) []*schema.Document {
			client, _ := rerank.GetClient(ctx)
			if client == nil || len(docs) <= 1 {
				return docs
			}
			results := client.Rerank(ctx, query, docs, topN)
			reranked := make([]*schema.Document, 0, len(results))
			for _, result := range results {
				reranked = append(reranked, result.Doc)
			}
			return reranked
		},
	}
}

// RetrieveDocuments 执行专业 Agent 唯一的 Normalize、Rewrite/Split、Retriever
// 和 Rerank 阶段，并保留 Retriever 已附加的文档元数据。
func RetrieveDocuments(ctx context.Context, input *UserMessage, opts RetrievalOptions) ([]*schema.Document, error) {
	return retrieveDocuments(ctx, input, opts, productionRetrievalDependencies())
}

func retrieveDocuments(ctx context.Context, input *UserMessage, opts RetrievalOptions, deps retrievalDependencies) ([]*schema.Document, error) {
	query := deps.normalize(ctx, input.Query)
	r := deps.retriever()

	if !opts.SplitEnabled {
		if opts.RewriteEnabled && len(input.History) > 0 {
			query = deps.rewriteQuery(ctx, query, input.History)
		}
		return r.Retrieve(ctx, query)
	}

	var queries []string
	if opts.RewriteEnabled {
		_, queries = deps.rewriteAndSplit(ctx, query, input.History)
	} else {
		queries = deps.splitQuestions(ctx, query)
	}
	docs, err := retrieval.MultiRetrieve(ctx, r, queries)
	if err != nil {
		g.Log().Warningf(ctx, "[RetrievalNode] 向量检索失败: %v", err)
		return nil, err
	}
	g.Log().Debugf(ctx, "[RetrievalNode] 检索完成 | 子查询数=%d | 文档数=%d", len(queries), len(docs))
	return deps.rerank(ctx, input.Query, docs, 3), nil
}
