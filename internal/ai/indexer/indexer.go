package indexer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"SentinelOps/internal/ai/embedder"
	milvus "SentinelOps/internal/dao/milvus"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/stringutil"

	"github.com/bytedance/sonic"
	einomilvus "github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// floatVectorRow 是用于 Milvus InsertRows 的行结构体。
// Vector 字段类型为 []float32（稠密向量），SparseVector 为 BM25 稀疏向量。
type floatVectorRow struct {
	ID           string                 `json:"id" milvus:"name:id"`
	Content      string                 `json:"content" milvus:"name:content"`
	Vector       []float32              `json:"vector" milvus:"name:vector"`
	SparseVector entity.SparseEmbedding `json:"-" milvus:"name:sparse_vector"`
	Metadata     []byte                 `json:"metadata" milvus:"name:metadata"`
}

// floatVectorDocumentConverter 将 Document + Embedding 结果转为 Milvus FloatVector 行。
// eino-ext 默认转换器生成 []byte（BinaryVector），不兼容 FloatVector schema。
// 此转换器同时生成 BM25 稀疏向量，支持 Sparse-Dense 混合检索。
func floatVectorDocumentConverter(_ context.Context, docs []*schema.Document, vectors [][]float64) ([]interface{}, error) {
	rows := make([]interface{}, 0, len(docs))
	for i, doc := range docs {
		metadata, err := sonic.Marshal(doc.MetaData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		vec32 := make([]float32, len(vectors[i]))
		for j, v := range vectors[i] {
			vec32[j] = float32(v)
		}
		// 生成 BM25 稀疏向量
		sparseVec, err := embedder.BM25Embed(doc.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to generate sparse vector: %w", err)
		}
		rows = append(rows, &floatVectorRow{
			ID:           doc.ID,
			Content:      doc.Content,
			Vector:       vec32,
			SparseVector: sparseVec,
			Metadata:     metadata,
		})
	}
	return rows, nil
}

var (
	globalIndexer     *einomilvus.Indexer
	globalIndexerOnce sync.Once
	globalIndexerErr  error
)

// GetIndexer 返回全局单例 Milvus 索引器（懒初始化，线程安全）。
// 使用 events 分区，供 StoreEvents 调用。
func GetIndexer(ctx context.Context) (*einomilvus.Indexer, error) {
	globalIndexerOnce.Do(func() {
		globalIndexer, globalIndexerErr = NewIndexer(ctx, milvus.PartitionEvents)
	})
	return globalIndexer, globalIndexerErr
}

// NewIndexer 创建并返回一个新的 Milvus 向量索引器（每次调用都做完整初始化检查）。
// partition 指定写入的分区名（PartitionEvents 或 PartitionDocuments）。
func NewIndexer(ctx context.Context, partition string) (*einomilvus.Indexer, error) {
	cli, err := milvus.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	eb, err := embedder.NewDenseEmbedder(ctx)
	if err != nil {
		return nil, err
	}
	config := &einomilvus.IndexerConfig{
		Client:            cli,
		Collection:        milvus.CollectionName,
		Fields:            milvus.CollectionFields,
		Embedding:         eb,
		DocumentConverter: floatVectorDocumentConverter,
		PartitionName:     partition,
	}
	idx, err := einomilvus.NewIndexer(ctx, config)
	if err != nil {
		return nil, err
	}
	return idx, nil
}

// StoreEvents 将安全事件批量转换为向量文档并写入 Milvus events 分区，按 batchSize 分批执行。
// 单批写入失败会继续其余批次，并在末尾返回聚合错误，禁止局部成功伪装整体成功。
// 同时返回所有成功写入 Milvus 的事件 ID，供调用方据此更新 indexed_at 标记。
func StoreEvents(ctx context.Context, events []dao.Event, batchSize int) ([]string, error) {
	if len(events) == 0 {
		return nil, nil
	}
	idx, err := GetIndexer(ctx)
	if err != nil {
		return nil, fmt.Errorf("初始化 Milvus 索引器失败: %w", err)
	}

	// 将事件转换为 schema.Document（event → 向量文档）
	docs := make([]*schema.Document, 0, len(events))
	totalChars := 0
	for _, e := range events {
		text := e.Title
		if e.Content != "" {
			text += "\n" + e.Content
		}
		text = stringutil.TruncateBytes(text, 8000)
		totalChars += len([]rune(text))
		meta := map[string]any{
			"source":     e.Source,
			"event_type": e.EventType,
			"severity":   e.Severity,
			"created_at": e.CreatedAt.Format("2006-01-02"),
		}
		if e.CVEID != "" {
			meta["cve_id"] = e.CVEID
		}
		docs = append(docs, &schema.Document{ID: e.ID, Content: text, MetaData: meta})
	}

	// 分批写入 Milvus（Store 内部无分批，单次全量易超 Embedding API 限制）
	totalBatches := (len(docs) + batchSize - 1) / batchSize
	var successIDs []string
	var batchErrors []error
	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batchIDs, storeErr := idx.Store(ctx, docs[i:end])
		if storeErr != nil {
			g.Log().Warningf(ctx, "[indexer] 第 %d/%d 批向量写入失败（%d~%d）: %v",
				i/batchSize+1, totalBatches, i, end, storeErr)
			batchErrors = append(batchErrors, fmt.Errorf("milvus batch %d/%d: %w", i/batchSize+1, totalBatches, storeErr))
			continue
		}
		successIDs = append(successIDs, batchIDs...)
	}

	// 索引完成后记录 Embedding 成本估算（中英文混合按 1.5 字符/token）
	if len(successIDs) > 0 {
		estimatedTokens := int(float64(totalChars) / 1.5)
		estimatedCost := float64(estimatedTokens) * 0.5 / 1_000_000.0
		g.Log().Debugf(ctx, "[indexer] Embedding 成本估算 | docs=%d | chars=%d | estimatedTokens=%d | estimatedCost=¥%.6f",
			len(successIDs), totalChars, estimatedTokens, estimatedCost)
	}

	return finalizeStoreResult(successIDs, len(events), batchErrors)
}

func finalizeStoreResult(successIDs []string, total int, batchErrors []error) ([]string, error) {
	err := errors.Join(batchErrors...)
	if err != nil {
		return successIDs, err
	}
	if len(successIDs) != total {
		return successIDs, fmt.Errorf("milvus 索引结果不完整: succeeded=%d total=%d", len(successIDs), total)
	}
	return successIDs, nil
}

// QueryMilvusTargetState 通过现有 MySQL indexed_at 元数据核对 Milvus 派生目标；
// 只读，不启动索引器、不写入向量库。
func QueryMilvusTargetState(ctx context.Context, eventID string) (bool, map[string]any, error) {
	if eventID == "" {
		return false, nil, fmt.Errorf("milvus target event id is required")
	}
	event, err := dao.GetEventByID(ctx, eventID)
	if err != nil {
		return false, map[string]any{"mysql_record": "unknown", "milvus_index": "unknown"}, err
	}
	if event.IndexedAt == nil {
		return false, map[string]any{"mysql_record": "confirmed", "milvus_index": "not_confirmed"}, nil
	}
	return true, map[string]any{"mysql_record": "confirmed", "milvus_index": "confirmed", "indexed_at": event.IndexedAt.UTC().Format(time.RFC3339Nano)}, nil
}
