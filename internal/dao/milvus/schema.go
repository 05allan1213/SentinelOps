package milvus

import (
	"fmt"

	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	DBName         = "sentinel"
	CollectionName = "rag_store"

	// PartitionEvents 安全事件向量分区
	PartitionEvents = "events"
	// PartitionDocuments 知识文档向量分区
	PartitionDocuments = "documents"

	// EmbeddingDim 是 qwen3.7-text-embedding 的向量维度，embedder 与 Milvus Schema 共用此常量保证一致。
	EmbeddingDim = 2048
)

// CollectionFields 是 rag_store 集合的 Schema 定义（唯一事实来源）。
// client.go 建表和 indexer.go 写入共同引用，杜绝字段漂移。
//
// 设计说明：使用 FloatVector(2048) + COSINE 度量做稠密检索，
// 同时提供 SparseVector 字段支持 BM25 关键词检索，
// 两路结果通过 HybridSearch + RRF 融合排序，兼顾语义相似度与关键词匹配。
var CollectionFields = []*entity.Field{
	{
		Name:       "id",
		DataType:   entity.FieldTypeVarChar,
		TypeParams: map[string]string{"max_length": "256"},
		PrimaryKey: true,
	},
	{
		// float32[2048]，COSINE 度量；稠密语义检索
		Name:       "vector",
		DataType:   entity.FieldTypeFloatVector,
		TypeParams: map[string]string{"dim": fmt.Sprintf("%d", EmbeddingDim)},
	},
	{
		// 稀疏向量，BM25 关键词检索
		Name:     "sparse_vector",
		DataType: entity.FieldTypeSparseVector,
	},
	{
		// 被向量化的原始文本（子块内容），检索命中后作为 RAG 上下文返回给 LLM
		Name:       "content",
		DataType:   entity.FieldTypeVarChar,
		TypeParams: map[string]string{"max_length": "8192"},
	},
	{
		// JSON 元数据：title、source、severity、event_type、parent_content、section_title 等
		Name:     "metadata",
		DataType: entity.FieldTypeJSON,
	},
}
