package knowledge_index_pipeline

import (
	"context"

	indexer2 "SentinelOps/internal/ai/indexer"
	milvus "SentinelOps/internal/dao/milvus"

	"github.com/cloudwego/eino/components/indexer"
)

// newIndexer 初始化知识索引流水线中的向量索引器节点（写入 documents 分区）。
// - 底层基于 Milvus 实现，内部会注入 NewDenseEmbedder 作为向量生成器
// - 对上层编排只暴露统一的 indexer.Indexer 接口，屏蔽具体向量库实现细节
func newIndexer(ctx context.Context) (idr indexer.Indexer, err error) {
	return indexer2.NewIndexer(ctx, milvus.PartitionDocuments)
}
