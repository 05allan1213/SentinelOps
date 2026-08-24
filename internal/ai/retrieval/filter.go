package retrieval

import (
	"context"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"

	dao "SentinelOps/internal/dao/mysql"
)

// FilterDisabledDocs 过滤已禁用文档的分块
//
// 背景：知识库文档可被用户禁用（enabled=false），但 Milvus 中的分块仍存在
// 原理：从检索结果的 metadata.doc_id 提取文档 ID，批量查询 MySQL 禁用状态
// 设计：MySQL 不可用时降级返回原始结果，不影响检索主流程
func FilterDisabledDocs(ctx context.Context, docs []*schema.Document) []*schema.Document {
	if len(docs) == 0 {
		return docs
	}

	// 步骤1：收集所有文档 ID（去重）
	docIDSet := make(map[string]struct{}, len(docs))
	for _, d := range docs {
		if docID, ok := d.MetaData["doc_id"]; ok {
			if s, ok := docID.(string); ok && s != "" {
				docIDSet[s] = struct{}{}
			}
		}
	}
	if len(docIDSet) == 0 {
		return docs
	}

	docIDs := make([]string, 0, len(docIDSet))
	for id := range docIDSet {
		docIDs = append(docIDs, id)
	}

	// 步骤2：批量查询禁用文档（容错降级）
	db, err := dao.DB(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "[retrieval] FilterDisabledDocs: DB 不可用，跳过过滤: %v", err)
		return docs
	}

	var disabledDocs []dao.KnowledgeDocument
	db.Select("id").Where("id IN ? AND enabled = false", docIDs).Find(&disabledDocs)

	if len(disabledDocs) == 0 {
		return docs
	}

	// 步骤3：构建禁用集合并过滤
	disabledSet := make(map[string]struct{}, len(disabledDocs))
	for _, d := range disabledDocs {
		disabledSet[d.ID] = struct{}{}
	}

	filtered := make([]*schema.Document, 0, len(docs))
	for _, d := range docs {
		docID, _ := d.MetaData["doc_id"].(string)
		if _, disabled := disabledSet[docID]; !disabled {
			filtered = append(filtered, d)
		}
	}
	g.Log().Debugf(ctx, "[retrieval] FilterDisabledDocs: 原始=%d 过滤后=%d（禁用=%d）", len(docs), len(filtered), len(disabledDocs))
	return filtered
}
