// Package searcher 提供多种检索策略实现
//
// # 混合检索背景
//
// 纯稠密向量检索在精确词匹配场景（CVE编号、产品名）召回率不足，
// 纯稀疏向量检索在语义理解场景（同义词、改写）效果较差。
// 混合检索结合两者优势，通过 RRF 算法融合排序，提升检索质量。
//
// # 稠密向量 vs 稀疏向量
//
// **稠密向量（Dense Vector）**
//   - 原理：通过 qwen3.7-text-embedding 将文本编码为 2048 维连续向量
//   - 优势：捕捉语义相似性，理解同义词、改写、上下文关系
//   - 适用：语义搜索、相似问题匹配、跨语言检索
//   - 度量：COSINE 余弦相似度（归一化向量的标准度量）
//   - 示例：查询"SQL注入"能匹配到"数据库注入攻击"
//
// **稀疏向量（Sparse Vector）**
//   - 原理：基于 BM25 算法，将文本编码为高维稀疏向量（仅非零项有值）
//   - 优势：精确词匹配，对专有名词、编号、缩写敏感
//   - 适用：关键词搜索、CVE编号查询、产品名精确匹配
//   - 度量：IP 内积（稀疏向量的标准度量）
//   - 示例：查询"CVE-2024-1234"精确匹配该编号
//
// **RRF 融合算法（Reciprocal Rank Fusion）**
//   - 公式：score(d) = Σ 1/(k + rank_i(d))，其中 k=60 为标准参数
//   - 优势：无需手动调整权重，自动平衡双路检索结果
//   - 原理：根据文档在各路检索中的排名倒数求和，排名越靠前贡献越大
//   - 效果：精确匹配和语义理解兼顾，召回率和准确率双提升
//
// # 实际案例对比
//
// 查询："Apache Log4j 远程代码执行漏洞"
//   - 纯稠密：召回语义相关的"Java日志库安全问题"（语义匹配）
//   - 纯稀疏：召回包含"Log4j"的精确文档（词匹配）
//   - 混合检索：同时召回上述两类，RRF 融合后排序更优
//
// 查询："CVE-2021-44228"
//   - 纯稠密：可能召回不相关文档（向量空间中的近邻）
//   - 纯稀疏：精确召回该 CVE 编号的文档
//   - 混合检索：稀疏路径保证精确召回，稠密路径补充相关漏洞
package searcher

import (
	"context"
	"fmt"

	"SentinelOps/internal/ai/embedder"
	aitrace "SentinelOps/internal/ai/trace"
	"SentinelOps/internal/dao/milvus"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	milvuscli "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// HybridSearcher 混合检索器：Sparse-Dense 双路检索 + RRF 融合
//
// 工作流程：
//  1. 稠密向量检索（COSINE 相似度）
//  2. 稀疏向量检索（BM25 内积）
//  3. RRF 融合排序（Reciprocal Rank Fusion）
//  4. 失败降级为纯稠密检索
type HybridSearcher struct {
	cli       milvuscli.Client
	topK      int    // 每路召回数量
	partition string // Milvus 分区名
	rrfK      int    // RRF 参数 k，控制排序平滑度
}

func NewHybridSearcher(cli milvuscli.Client, topK int, partition string, rrfK int) *HybridSearcher {
	return &HybridSearcher{
		cli:       cli,
		topK:      topK,
		partition: partition,
		rrfK:      rrfK,
	}
}

// Search 执行混合检索：稠密向量 + 稀疏向量 → RRF 融合
//
// 参数：
//   - query: 原始查询文本（用于 BM25）
//   - queryVec: 稠密向量（已由 Embedder 生成）
//
// 返回：融合排序后的文档列表，失败时自动降级为纯稠密检索
func (s *HybridSearcher) Search(ctx context.Context, query string, queryVec []float64) ([]*schema.Document, error) {
	g.Log().Debugf(ctx, "[HybridSearcher] 开始混合检索 | query=%s | topK=%d | partition=%s", query, s.topK, s.partition)

	f32 := make([]float32, len(queryVec))
	for i, v := range queryVec {
		f32[i] = float32(v)
	}

	// ── 阶段1：稠密向量检索准备 ──
	// 追踪节点：记录稠密向量准备耗时
	denseSpanCtx, denseSpanID := aitrace.StartSpan(ctx, aitrace.NodeTypeRetriever, "Dense-Vector-Prep")
	denseSP, err := entity.NewIndexAUTOINDEXSearchParam(1) // nprobe=1，快速检索
	if err != nil {
		aitrace.FinishSpan(denseSpanCtx, denseSpanID, err, nil)
		return nil, fmt.Errorf("build dense param: %w", err)
	}
	// COSINE 相似度：适合归一化向量的语义匹配
	denseReq := milvuscli.NewANNSearchRequest("vector", entity.COSINE, "", []entity.Vector{entity.FloatVector(f32)}, denseSP, s.topK)
	aitrace.FinishSpan(denseSpanCtx, denseSpanID, nil, map[string]any{"topk": s.topK})

	// ── 阶段2：稀疏向量检索准备 ──
	// 追踪节点：记录 BM25 编码 + 稀疏向量准备耗时
	sparseSpanCtx, sparseSpanID := aitrace.StartSpan(ctx, aitrace.NodeTypeRetriever, "Sparse-Vector-Prep")
	sparseVec, err := embedder.BM25Embed(query) // BM25 编码：精确词匹配
	if err != nil {
		aitrace.FinishSpan(sparseSpanCtx, sparseSpanID, err, nil)
		return nil, fmt.Errorf("bm25 embed: %w", err)
	}
	g.Log().Debugf(ctx, "[HybridSearcher] BM25编码完成")
	sparseSP, err := entity.NewIndexSparseInvertedSearchParam(0.0) // drop_ratio=0，保留所有词
	if err != nil {
		aitrace.FinishSpan(sparseSpanCtx, sparseSpanID, err, nil)
		return nil, fmt.Errorf("build sparse param: %w", err)
	}
	// IP 内积：稀疏向量标准度量
	sparseReq := milvuscli.NewANNSearchRequest("sparse_vector", entity.IP, "", []entity.Vector{sparseVec}, sparseSP, s.topK)
	aitrace.FinishSpan(sparseSpanCtx, sparseSpanID, nil, map[string]any{"topk": s.topK})

	partitions := []string{}
	if s.partition != "" {
		partitions = []string{s.partition}
	}

	// ── 阶段3：RRF 混合检索与融合 ──
	// RRF (Reciprocal Rank Fusion) 融合算法原理：
	//   score(d) = Σ 1/(k + rank_i(d))
	//   - d: 文档
	//   - rank_i(d): 文档 d 在第 i 路检索中的排名（从 1 开始）
	//   - k: 平滑参数，默认 60（标准值）
	//
	// 工作流程：
	//   1. Dense 检索返回排序列表：[doc1(rank=1), doc2(rank=2), doc3(rank=3), ...]
	//   2. Sparse 检索返回排序列表：[doc3(rank=1), doc1(rank=3), doc5(rank=2), ...]
	//   3. 对每个文档计算 RRF 分数：
	//      doc1: 1/(60+1) + 1/(60+3) = 0.0164 + 0.0159 = 0.0323
	//      doc3: 1/(60+3) + 1/(60+1) = 0.0159 + 0.0164 = 0.0323
	//   4. 按 RRF 分数降序排列，返回 Top-K
	//
	// 分数含义：
	//   - 分数越大 = 文档在多路检索中综合排名越靠前 = 越相关
	//   - 典型范围：0.01-0.05（远低于传统余弦相似度 0-1）
	//   - 排名靠前的文档贡献更大：rank=1 贡献 0.0164，rank=10 贡献 0.0143
	//
	// 优势：
	//   - 无需手动调整权重（Dense vs Sparse）
	//   - 自动平衡双路检索结果
	//   - 排名靠前的文档贡献更大（倒数关系）
	rrfSpanCtx, rrfSpanID := aitrace.StartSpan(ctx, aitrace.NodeTypeRetriever, "RRF-Hybrid-Search")
	results, err := s.cli.HybridSearch(
		rrfSpanCtx,
		milvus.CollectionName,
		partitions,
		s.topK,
		[]string{"id", "content", "metadata"},
		milvuscli.NewRRFReranker(), // RRF 融合器（k=60）
		[]*milvuscli.ANNSearchRequest{denseReq, sparseReq},
	)

	// 容错降级：混合检索失败时回退到纯稠密检索
	if err != nil {
		aitrace.FinishSpan(rrfSpanCtx, rrfSpanID, err, map[string]any{"rrf_k": s.rrfK, "partition": s.partition})
		g.Log().Warningf(ctx, "[HybridSearcher] 混合检索失败，降级稠密检索: %v", err)
		denseSearcher := NewDenseSearcher(s.cli, s.topK, s.partition)
		return denseSearcher.Search(ctx, queryVec)
	}

	if len(results) == 0 {
		aitrace.FinishSpan(rrfSpanCtx, rrfSpanID, nil, map[string]any{"rrf_k": s.rrfK, "doc_count": 0})
		g.Log().Debugf(ctx, "[HybridSearcher] 混合检索无结果")
		return []*schema.Document{}, nil
	}

	docs, parseErr := ParseMilvusResult(results[0])
	aitrace.FinishSpan(rrfSpanCtx, rrfSpanID, parseErr, map[string]any{
		"rrf_k":     s.rrfK,
		"doc_count": len(docs),
		"partition": s.partition,
	})
	g.Log().Debugf(ctx, "[HybridSearcher] 混合检索完成 | doc_count=%d | partition=%s", len(docs), s.partition)

	return docs, parseErr
}
