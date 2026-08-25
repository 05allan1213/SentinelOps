package retrieval

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"SentinelOps/internal/ai/evidence"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/schema"
)

// ErrEvidenceUnavailable 表示授权或状态真值源不可用；调用方必须返回空证据。
var ErrEvidenceUnavailable = errors.New("evidence unavailable")

// FilterDisabledDocs 过滤失效、未完成索引或越权文档。任何 MySQL/ACL 错误均返回空结果。
func FilterDisabledDocs(ctx context.Context, docs []*schema.Document) []*schema.Document {
	filtered, err := FilterDisabledDocsStrict(ctx, docs)
	if err != nil {
		return nil
	}
	return filtered
}

// FilterDisabledDocsStrict 是 Retriever 与管理查询共用的 fail-closed 入口。
func FilterDisabledDocsStrict(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error) {
	scope, ok := evidence.ScopeFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("%w: missing request scope", ErrEvidenceUnavailable)
	}
	return FilterDocuments(ctx, docs, scope)
}

// FilterDocuments 以 MySQL 状态/ACL 和 Milvus metadata 的交集作为唯一可返回集合。
func FilterDocuments(ctx context.Context, docs []*schema.Document, scope evidence.Scope) ([]*schema.Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}
	docIDs := make([]string, 0, len(docs))
	seen := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		if doc == nil || doc.MetaData == nil {
			continue
		}
		if id, _ := doc.MetaData["doc_id"].(string); id != "" {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				docIDs = append(docIDs, id)
			}
		}
	}
	// events 分区没有 knowledge document ACL，不应被 documents predicate 误伤。
	if len(docIDs) == 0 {
		return docs, nil
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: mysql: %v", ErrEvidenceUnavailable, err)
	}
	var records []dao.KnowledgeDocument
	if err := db.Where("id IN ? AND enabled = ? AND index_status = ?", docIDs, true, "completed").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("%w: document state query: %v", ErrEvidenceUnavailable, err)
	}
	byID := make(map[string]dao.KnowledgeDocument, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	var chunks []dao.KnowledgeChunk
	if err := db.Where("doc_id IN ? AND enabled = ?", docIDs, true).Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("%w: chunk state query: %v", ErrEvidenceUnavailable, err)
	}
	chunkByID := make(map[string]dao.KnowledgeChunk, len(chunks))
	for _, chunk := range chunks {
		chunkByID[chunk.ID] = chunk
	}

	filtered := make([]*schema.Document, 0, len(docs))
	for _, doc := range docs {
		if doc == nil || doc.MetaData == nil {
			continue
		}
		docID, _ := doc.MetaData["doc_id"].(string)
		if docID == "" {
			continue
		}
		record, ok := byID[docID]
		if !ok || !scope.Allows(record.AccessScope) || !metadataMatchesRecord(doc.MetaData, record) {
			continue
		}
		chunk, ok := chunkByID[doc.ID]
		if !ok || !metadataMatchesChunk(doc.MetaData, chunk) {
			continue
		}
		filtered = append(filtered, doc)
	}
	return filtered, nil
}

func metadataMatchesRecord(meta map[string]any, record dao.KnowledgeDocument) bool {
	access, ok := meta["access_scope"].(string)
	if !ok || access == "" || access != record.AccessScope {
		return false
	}
	if stringValue(meta, "source_version") != record.SourceVersion || stringValue(meta, "content_hash") != record.ContentHash {
		return false
	}
	return uint64Value(meta, "indexed_version") == record.IndexedVersion && record.IndexedVersion > 0
}

func metadataMatchesChunk(meta map[string]any, chunk dao.KnowledgeChunk) bool {
	if !chunk.Enabled || stringValue(meta, "source_version") != chunk.SourceVersion || stringValue(meta, "content_hash") != chunk.ContentHash {
		return false
	}
	return stringValue(meta, "access_scope") == chunk.AccessScope && uint64Value(meta, "indexed_version") == chunk.IndexedVersion && chunk.IndexedVersion > 0
}

func stringValue(meta map[string]any, key string) string {
	value, _ := meta[key].(string)
	return value
}

func uint64Value(meta map[string]any, key string) uint64 {
	switch value := meta[key].(type) {
	case uint64:
		return value
	case int:
		if value >= 0 {
			return uint64(value)
		}
	case int64:
		if value >= 0 {
			return uint64(value)
		}
	case float64:
		if value >= 0 {
			return uint64(value)
		}
	case string:
		parsed, _ := strconv.ParseUint(value, 10, 64)
		return parsed
	}
	return 0
}
