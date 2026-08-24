package retrieval

import (
	"context"
	"fmt"

	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/retrieval/searcher"
	aitrace "SentinelOps/internal/ai/trace"

	"github.com/cloudwego/eino/components/embedding"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	milvuscli "github.com/milvus-io/milvus-sdk-go/v2/client"
	goredis "github.com/redis/go-redis/v9"
)

// Retriever 检索编排器：实现 Eino Retriever 接口
//
// 工作流程：
//  1. Embedding：查询文本 → 稠密向量
//  2. 缓存检查：命中则直接返回，跳过 Milvus
//  3. 检索执行：根据配置选择混合/纯稠密检索
//  4. 结果过滤：按 MinScore 过滤 + FinalTopK 截断
//  5. 缓存写入：结果写入 Redis 供后续复用
type Retriever struct {
	milvusCli milvuscli.Client
	embedder  embedding.Embedder
	cache     *cache.SemanticCache
	cfg       Config
}

func New(cli milvuscli.Client, eb embedding.Embedder, redisCli *goredis.Client, cfg Config) *Retriever {
	return &Retriever{
		milvusCli: cli,
		embedder:  eb,
		cache:     cache.New(redisCli, cfg.CacheKeyPrefix, int(cfg.CacheTTL.Hours()), cfg.CacheThreshold),
		cfg:       cfg,
	}
}

// Retrieve 执行检索：Embedding → 缓存/检索 → 过滤 → 返回
func (r *Retriever) Retrieve(ctx context.Context, query string, opts ...einoretriever.Option) ([]*schema.Document, error) {
	// ── 阶段1：向量嵌入 ──
	// 追踪节点：记录 Embedding API 调用耗时和成本
	embSpanCtx, embSpanID := aitrace.StartSpan(ctx, aitrace.NodeTypeEmbedding, r.cfg.EmbeddingModel)
	vecs, err := r.embedder.EmbedStrings(embSpanCtx, []string{query})
	estimatedTokens := int(float64(len([]rune(query))) / 1.5) // 估算 Token：1.5 字符/token
	aitrace.FinishSpanWithCost(embSpanCtx, embSpanID, r.cfg.EmbeddingModel, estimatedTokens, 0, err)

	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed returned empty")
	}
	queryVec := vecs[0]

	// ── 阶段2：语义缓存检查 ──
	// 命中：跳过 Milvus，直接返回缓存结果（节省 ~50ms）
	if docs, sim, ok := r.cache.Get(ctx, queryVec); ok {
		g.Log().Infof(ctx, "[Retriever] 缓存命中 %d 文档 | query=%q | sim=%.4f", len(docs), query, sim)
		return docs, nil
	}
	g.Log().Infof(ctx, "[Retriever] 缓存未命中，执行检索 | query=%q | partition=%q | hybrid=%v", query, r.cfg.Partition, r.cfg.HybridEnabled)

	// ── 阶段3：Milvus 检索 ──
	// 根据配置选择检索策略：混合检索（BM25+语义）或纯稠密检索
	retSpanCtx, retSpanID := aitrace.StartSpan(ctx, aitrace.NodeTypeRetriever, "Milvus-Search")
	var docs []*schema.Document
	if r.cfg.HybridEnabled {
		s := searcher.NewHybridSearcher(r.milvusCli, r.cfg.TopK, r.cfg.Partition, r.cfg.RRFK)
		docs, err = s.Search(retSpanCtx, query, queryVec)
	} else {
		s := searcher.NewDenseSearcher(r.milvusCli, r.cfg.TopK, r.cfg.Partition)
		docs, err = s.Search(retSpanCtx, queryVec)
	}

	meta := map[string]any{
		"doc_count": len(docs),
		"partition": r.cfg.Partition,
		"hybrid":    r.cfg.HybridEnabled,
	}
	aitrace.FinishSpan(retSpanCtx, retSpanID, err, meta)

	if err != nil {
		return nil, err
	}

	// ── 阶段4：结果过滤与截断 ──
	// 过滤：仅对纯稠密检索应用 MinScore 过滤
	// 混合检索使用 RRF 分数（0.01-0.05），不适用传统相似度阈值
	filtered := docs
	if !r.cfg.HybridEnabled {
		// 纯稠密检索：过滤低于 MinScore 的文档
		filtered = make([]*schema.Document, 0, len(docs))
		for _, doc := range docs {
			if doc.Score() >= r.cfg.MinScore {
				filtered = append(filtered, doc)
			}
		}
	}

	// 截断：控制送入 LLM 的文档数量（平衡上下文质量与 Token 消耗）
	if r.cfg.FinalTopK > 0 && len(filtered) > r.cfg.FinalTopK {
		filtered = filtered[:r.cfg.FinalTopK]
	}

	if r.cfg.HybridEnabled {
		g.Log().Infof(ctx, "[Retriever] 召回 %d 块，保留 %d 块（混合检索，finalTopK=%d）", len(docs), len(filtered), r.cfg.FinalTopK)
	} else {
		g.Log().Infof(ctx, "[Retriever] 召回 %d 块，保留 %d 块（minScore=%.2f，finalTopK=%d）", len(docs), len(filtered), r.cfg.MinScore, r.cfg.FinalTopK)
	}

	// ── 阶段5：缓存写入 ──
	r.cache.Set(ctx, queryVec, filtered)
	return filtered, nil
}
