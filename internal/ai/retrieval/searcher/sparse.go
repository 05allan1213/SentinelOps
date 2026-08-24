package searcher

import (
	"context"
	"fmt"

	"SentinelOps/internal/ai/embedder"
	"SentinelOps/internal/dao/milvus"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	milvuscli "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// SparseSearcher 稀疏向量检索器：基于 BM25 的精确词匹配检索
//
// 原理：BM25 算法将文本编码为稀疏向量（词项-权重对）
// 通过倒排索引快速匹配精确词项，适合 CVE 编号、产品名等精确查询
// 优势：无需语义理解，对专有名词、代码片段检索效果优于稠密向量
type SparseSearcher struct {
	cli       milvuscli.Client
	topK      int
	partition string
}

func NewSparseSearcher(cli milvuscli.Client, topK int, partition string) *SparseSearcher {
	return &SparseSearcher{
		cli:       cli,
		topK:      topK,
		partition: partition,
	}
}

// Search 执行 BM25 稀疏向量检索
// 参数 query：原始查询文本（直接用于 BM25 编码）
// 返回：按 BM25 分数降序排列的文档列表
func (s *SparseSearcher) Search(ctx context.Context, query string) ([]*schema.Document, error) {
	g.Log().Debugf(ctx, "[SparseSearcher] 开始稀疏检索 | query=%s | topK=%d | partition=%s", query, s.topK, s.partition)

	// BM25 编码：文本 → 稀疏向量（词项索引 + TF-IDF 权重）
	sparseVec, err := embedder.BM25Embed(query)
	if err != nil {
		return nil, fmt.Errorf("bm25 embed: %w", err)
	}
	g.Log().Debugf(ctx, "[SparseSearcher] BM25编码完成")

	// 稀疏倒排索引参数：drop_ratio=0.0 保留所有词项
	sp, err := entity.NewIndexSparseInvertedSearchParam(0.0)
	if err != nil {
		return nil, fmt.Errorf("build sparse param: %w", err)
	}

	partitions := []string{}
	if s.partition != "" {
		partitions = []string{s.partition}
	}

	// 执行稀疏向量检索
	// - IP 内积：稀疏向量标准度量（等价于 BM25 分数）
	// - sparse_vector 字段：Milvus SPARSE_INVERTED_INDEX
	results, err := s.cli.Search(
		ctx,
		milvus.CollectionName,
		partitions,
		"",
		[]string{"id", "content", "metadata"},
		[]entity.Vector{sparseVec},
		"sparse_vector",
		entity.IP,
		s.topK,
		sp,
	)
	if err != nil {
		g.Log().Warningf(ctx, "[SparseSearcher] Milvus检索失败: %v", err)
		return nil, fmt.Errorf("milvus sparse search: %w", err)
	}
	if len(results) == 0 {
		g.Log().Debugf(ctx, "[SparseSearcher] 稀疏检索无结果")
		return []*schema.Document{}, nil
	}
	docs, parseErr := ParseMilvusResult(results[0])
	if parseErr == nil {
		g.Log().Debugf(ctx, "[SparseSearcher] 稀疏检索完成 | doc_count=%d | partition=%s", len(docs), s.partition)
	}
	return docs, parseErr
}
