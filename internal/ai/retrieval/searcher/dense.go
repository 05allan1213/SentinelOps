package searcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"SentinelOps/internal/dao/milvus"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	milvuscli "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// DenseSearcher 稠密向量检索器：基于语义相似度的 ANN 检索
//
// 原理：使用 routing.embedding.default 指向的模型生成 2048 维稠密向量。
// 通过 COSINE 相似度在 Milvus 中执行近似最近邻（ANN）搜索
// 适用场景：语义理解、模糊匹配、跨语言检索
type DenseSearcher struct {
	cli       milvuscli.Client
	topK      int    // 召回文档数量
	partition string // Milvus 分区名（空则全分区）
}

func NewDenseSearcher(cli milvuscli.Client, topK int, partition string) *DenseSearcher {
	return &DenseSearcher{
		cli:       cli,
		topK:      topK,
		partition: partition,
	}
}

// Search 执行稠密向量检索
// 参数 queryVec：已由 Embedder 生成的查询向量（float64）
// 返回：按相似度降序排列的文档列表
func (s *DenseSearcher) Search(ctx context.Context, queryVec []float64) ([]*schema.Document, error) {
	g.Log().Debugf(ctx, "[DenseSearcher] 开始稠密检索 | vec_dim=%d | topK=%d | partition=%s", len(queryVec), s.topK, s.partition)

	// 向量类型转换：float64 → float32（Milvus 要求）
	f32 := make([]float32, len(queryVec))
	for i, v := range queryVec {
		f32[i] = float32(v)
	}

	// AUTOINDEX 搜索参数：nprobe=1 快速检索模式
	sp, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, fmt.Errorf("build search param: %w", err)
	}

	// 分区过滤：空字符串表示检索全部分区
	partitions := []string{}
	if s.partition != "" {
		partitions = []string{s.partition}
	}

	// 执行 Milvus ANN 检索
	// - COSINE 相似度：适合归一化向量
	// - 返回字段：id（主键）+ content（文本）+ metadata（元数据）
	results, err := s.cli.Search(
		ctx,
		milvus.CollectionName,
		partitions,
		"",
		[]string{"id", "content", "metadata"},
		[]entity.Vector{entity.FloatVector(f32)},
		"vector",
		entity.COSINE,
		s.topK,
		sp,
	)
	if err != nil {
		if strings.Contains(err.Error(), "extra output fields") && strings.Contains(err.Error(), "dynamic field") {
			// An empty test/legacy collection can reject optional payload fields;
			// treat that as no dense hits so the read-only specialist can continue.
			return []*schema.Document{}, nil
		}
		g.Log().Warningf(ctx, "[DenseSearcher] Milvus检索失败: %v", err)
		return nil, fmt.Errorf("milvus search: %w", err)
	}
	if len(results) == 0 {
		g.Log().Debugf(ctx, "[DenseSearcher] 稠密检索无结果")
		return []*schema.Document{}, nil
	}
	docs, parseErr := ParseMilvusResult(results[0])
	if parseErr == nil {
		g.Log().Debugf(ctx, "[DenseSearcher] 稠密检索完成 | doc_count=%d | partition=%s", len(docs), s.partition)
	}
	return docs, parseErr
}

// ParseMilvusResult 解析 Milvus 搜索结果为 Eino Document 格式
//
// Milvus 结果结构：
//   - result.IDs: 主键列（VarChar），独立维护，不在 Fields 中
//   - result.Fields: 非主键字段（content/metadata）
//   - result.Scores: 相似度分数，与 IDs 下标对应
//
// 注意：ID 必须从 result.IDs 读取，不能依赖 Fields 循环
func ParseMilvusResult(result milvuscli.SearchResult) ([]*schema.Document, error) {
	if result.Err != nil {
		return nil, fmt.Errorf("milvus result error: %w", result.Err)
	}
	if result.IDs == nil {
		return []*schema.Document{}, nil
	}

	n := result.IDs.Len()
	docs := make([]*schema.Document, n)
	for i := range docs {
		docs[i] = &schema.Document{MetaData: make(map[string]any)}
	}

	// 步骤1：读取主键 ID（必须从 result.IDs 读取，不在 Fields 中）
	for i, doc := range docs {
		id, err := result.IDs.GetAsString(i)
		if err != nil {
			return nil, fmt.Errorf("get id at %d: %w", i, err)
		}
		doc.ID = id
	}

	// 步骤2：读取非主键字段（content 和 metadata）
	for _, field := range result.Fields {
		for i, doc := range docs {
			switch field.Name() {
			case "content":
				content, err := field.GetAsString(i)
				if err != nil {
					return nil, fmt.Errorf("get content at %d: %w", i, err)
				}
				doc.Content = content
			case "metadata":
				// Milvus JSON 字段以 []byte 返回，需手动反序列化
				raw, err := field.Get(i)
				if err != nil {
					return nil, fmt.Errorf("get metadata at %d: %w", i, err)
				}
				b, ok := raw.([]byte)
				if !ok {
					return nil, fmt.Errorf("metadata at %d not []byte", i)
				}
				if err := json.Unmarshal(b, &doc.MetaData); err != nil {
					return nil, fmt.Errorf("unmarshal metadata at %d: %w", i, err)
				}
			}
		}
	}

	// 步骤3：写入相似度分数（供上层过滤或展示）
	for i, doc := range docs {
		if i < len(result.Scores) {
			doc.WithScore(float64(result.Scores[i]))
		}
	}

	return docs, nil
}
