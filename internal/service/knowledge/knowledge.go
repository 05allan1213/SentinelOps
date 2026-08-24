// Package knowledge 知识库业务服务层：管理知识库、文档的 CRUD 和向量索引。
package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"gorm.io/gorm"

	pipeline "SentinelOps/internal/ai/agent/knowledge_index_pipeline"
	aidoc "SentinelOps/internal/ai/document"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/retrieval"
	milvus "SentinelOps/internal/dao/milvus"
	dao "SentinelOps/internal/dao/mysql"
)

// maxUploadBytes 服务端单文件上传大小上限（50 MB）。
const maxUploadBytes = 50 * 1024 * 1024

// RegisterExistingFile 为已存在的文件创建 MySQL 记录并投入索引队列。
// 文件已由调用方保存到 filePath，此函数只负责 MySQL 记录和异步索引，不重复写文件。
// 供 /api/chat/v1/upload 向后兼容调用，文件自动归入 baseID 指定的知识库。
func RegisterExistingFile(ctx context.Context, baseID, filePath string, chunkCfg aidoc.ChunkConfig) (*dao.KnowledgeDocument, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return nil, err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	docID := uuid.New().String()

	cfgJSON, _ := sonic.Marshal(chunkCfg)
	doc := &dao.KnowledgeDocument{
		ID:            docID,
		BaseID:        baseID,
		Name:          filepath.Base(filePath),
		FilePath:      filePath,
		FileSize:      info.Size(),
		FileType:      strings.TrimPrefix(ext, "."),
		ChunkStrategy: string(chunkCfg.Strategy),
		ChunkConfig:   string(cfgJSON),
		IndexStatus:   "pending",
		Enabled:       true,
	}
	if err = db.Create(doc).Error; err != nil {
		return nil, fmt.Errorf("create doc record: %w", err)
	}

	db.Model(&dao.KnowledgeBase{}).Where("id = ?", baseID).UpdateColumn("doc_count", gorm.Expr("doc_count + 1"))
	GetWorkerPool().Submit(IndexTask{DocID: docID, BaseID: baseID})
	return doc, nil
}

// EnsureDefaultBase 确保"默认知识库"存在（幂等，main.go 启动时调用）。
// 默认知识库 ID 固定为 "default"，上传的文档默认归入此库。
func EnsureDefaultBase(ctx context.Context) {
	db, err := dao.DB(ctx)
	if err != nil {
		return
	}
	var count int64
	db.Model(&dao.KnowledgeBase{}).Where("id = ?", "default").Count(&count)
	if count > 0 {
		return
	}
	defaultBase := &dao.KnowledgeBase{
		ID:          "default",
		Name:        "默认知识库",
		Description: "上传的文档默认归入此知识库",
	}
	db.Create(defaultBase)
	g.Log().Infof(ctx, "[knowledge] 默认知识库已创建")
}

// CreateBase 创建新知识库。
func CreateBase(ctx context.Context, name, description string) (*dao.KnowledgeBase, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return nil, err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}
	base := &dao.KnowledgeBase{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
	}
	if err = db.Create(base).Error; err != nil {
		return nil, fmt.Errorf("create knowledge base: %w", err)
	}
	return base, nil
}

// GetBase 返回单个知识库详情。
func GetBase(ctx context.Context, baseID string) (*dao.KnowledgeBase, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}
	var base dao.KnowledgeBase
	if err = db.Where("id = ?", baseID).First(&base).Error; err != nil {
		return nil, err
	}
	return &base, nil
}

// GetDoc 返回单个文档详情。
func GetDoc(ctx context.Context, docID string) (*dao.KnowledgeDocument, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}
	var doc dao.KnowledgeDocument
	if err = db.Where("id = ?", docID).First(&doc).Error; err != nil {
		return nil, err
	}
	return &doc, nil
}

// ListBases 返回所有知识库（按创建时间倒序）。
func ListBases(ctx context.Context) ([]dao.KnowledgeBase, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}
	var bases []dao.KnowledgeBase
	if err = db.Order("created_at DESC").Find(&bases).Error; err != nil {
		return nil, err
	}
	return bases, nil
}

// DeleteBase 删除知识库（软删除）及其所有文档（文档内向量、分块记录、本地文件一并清理）。
func DeleteBase(ctx context.Context, baseID string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	// 获取所有文档，逐个调用 DeleteDoc 保证 Milvus 向量和本地文件同步清理
	var docs []dao.KnowledgeDocument
	if err = db.Where("base_id = ?", baseID).Find(&docs).Error; err != nil {
		return err
	}
	for _, doc := range docs {
		if e := DeleteDoc(ctx, doc.ID); e != nil {
			g.Log().Warningf(ctx, "[knowledge] 删除文档 %s 失败: %v", doc.ID, e)
		}
	}
	return db.Delete(&dao.KnowledgeBase{}, "id = ?", baseID).Error
}

// UploadDoc 保存上传文件并创建 MySQL 记录（状态 pending），然后投入 Worker Pool 异步索引。
// 文件保存路径：manifest/upload/knowledge/<base_id>/<doc_id>.<ext>
// 同知识库内相同内容（SHA256 哈希相同）的文件会被拒绝，避免重复向量浪费。
func UploadDoc(ctx context.Context, baseID, fileName string, fileData []byte, chunkCfg aidoc.ChunkConfig) (*dao.KnowledgeDocument, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return nil, err
	}
	// P1：服务端文件大小校验
	if int64(len(fileData)) > maxUploadBytes {
		return nil, fmt.Errorf("文件超过大小限制（最大 %d MB）", maxUploadBytes/1024/1024)
	}

	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}

	// P2：SHA256 内容去重（同知识库内）
	hash := fmt.Sprintf("%x", sha256.Sum256(fileData))
	var existingCount int64
	db.Model(&dao.KnowledgeDocument{}).Where("base_id = ? AND file_hash = ?", baseID, hash).Count(&existingCount)
	if existingCount > 0 {
		return nil, fmt.Errorf("该文件内容在知识库中已存在（SHA256 相同），如需重新索引请使用「重建」功能")
	}

	docID := uuid.New().String()
	ext := strings.ToLower(filepath.Ext(fileName))
	uploadDir := filepath.Join("manifest", "upload", "knowledge", baseID)
	if err = os.MkdirAll(uploadDir, 0755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	savePath := filepath.Join(uploadDir, docID+ext)
	if err = os.WriteFile(savePath, fileData, 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	cfgJSON, _ := sonic.Marshal(chunkCfg)
	doc := &dao.KnowledgeDocument{
		ID:            docID,
		BaseID:        baseID,
		Name:          fileName,
		FilePath:      savePath,
		FileSize:      int64(len(fileData)),
		FileType:      strings.TrimPrefix(ext, "."),
		FileHash:      hash,
		ChunkStrategy: string(chunkCfg.Strategy),
		ChunkConfig:   string(cfgJSON),
		IndexStatus:   "pending",
		Enabled:       true,
	}
	if err = db.Create(doc).Error; err != nil {
		os.Remove(savePath)
		return nil, fmt.Errorf("create doc record: %w", err)
	}

	// 更新知识库文档数
	db.Model(&dao.KnowledgeBase{}).Where("id = ?", baseID).UpdateColumn("doc_count", gorm.Expr("doc_count + 1"))

	// 投入异步索引队列
	GetWorkerPool().Submit(IndexTask{DocID: docID, BaseID: baseID})
	return doc, nil
}

// ListDocParams 文档列表查询参数。
type ListDocParams struct {
	BaseID   string
	Page     int
	PageSize int
	Keyword  string // 文件名模糊搜索
	Status   string // 索引状态过滤
	FileType string // 文件类型过滤
}

// ListDoc 返回知识库下的文档列表（分页，支持按名称/状态/类型过滤，按创建时间倒序）。
func ListDoc(ctx context.Context, params ListDocParams) ([]dao.KnowledgeDocument, int64, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, 0, err
	}
	query := db.Model(&dao.KnowledgeDocument{}).Where("base_id = ?", params.BaseID)
	if params.Keyword != "" {
		query = query.Where("name LIKE ?", "%"+params.Keyword+"%")
	}
	if params.Status != "" {
		query = query.Where("index_status = ?", params.Status)
	}
	if params.FileType != "" {
		query = query.Where("file_type = ?", params.FileType)
	}
	var total int64
	query.Count(&total)
	var docs []dao.KnowledgeDocument
	offset := (params.Page - 1) * params.PageSize
	if err = query.Order("created_at DESC").Offset(offset).Limit(params.PageSize).Find(&docs).Error; err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

// deleteMilvusVectors 删除指定文档在 Milvus documents 分区的所有向量。
// 先 Query 查 ID 列表，再按 ID 批量删除（Milvus JSON 字段表达式直接删除受版本兼容性限制）。
// 失败时只打印 Warning 日志，不影响上层事务。
func deleteMilvusVectors(ctx context.Context, docID string) {
	milvusCli, mErr := milvus.GetClient(ctx)
	if mErr != nil {
		g.Log().Warningf(ctx, "[knowledge] deleteMilvusVectors: get client failed: %v", mErr)
		return
	}
	expr := fmt.Sprintf(`metadata["doc_id"] == "%s"`, docID)
	queryResult, _ := milvusCli.Query(ctx, milvus.CollectionName, []string{milvus.PartitionDocuments}, expr, []string{"id"})
	if len(queryResult) == 0 {
		return
	}
	var idsToDelete []string
	for _, col := range queryResult {
		if col.Name() == "id" {
			for i := 0; i < col.Len(); i++ {
				if id, e := col.GetAsString(i); e == nil {
					idsToDelete = append(idsToDelete, id)
				}
			}
		}
	}
	if len(idsToDelete) == 0 {
		return
	}
	quoted := make([]string, len(idsToDelete))
	for i, id := range idsToDelete {
		quoted[i] = `"` + id + `"`
	}
	delExpr := fmt.Sprintf(`id in [%s]`, strings.Join(quoted, ", "))
	milvusCli.Delete(ctx, milvus.CollectionName, milvus.PartitionDocuments, delExpr)
	g.Log().Infof(ctx, "[knowledge] deleteMilvusVectors: 删除 %d 个向量（doc=%s）", len(idsToDelete), docID)
}

// DeleteDoc 删除文档，按以下顺序执行（保证数据一致性）：
//  1. 查询文档记录（已软删除的不可操作）
//  2. 删除 Milvus documents 分区中 metadata.doc_id == docID 的所有向量
//  3. 批量删除 knowledge_chunks 记录（硬删）
//  4. 软删 knowledge_documents 记录
//  5. 更新知识库 doc_count / chunk_count 计数（使用 GREATEST 避免负数）
//  6. 删除本地文件（最后执行，前序步骤失败不影响文件保留）
func DeleteDoc(ctx context.Context, docID string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	var doc dao.KnowledgeDocument
	if err = db.Where("id = ?", docID).First(&doc).Error; err != nil {
		return err
	}

	// 删除 Milvus 向量
	deleteMilvusVectors(ctx, docID)

	// 删 knowledge_chunks
	db.Where("doc_id = ?", docID).Delete(&dao.KnowledgeChunk{})

	// 软删文档记录
	db.Delete(&doc)

	// 更新知识库统计
	db.Model(&dao.KnowledgeBase{}).Where("id = ?", doc.BaseID).UpdateColumns(map[string]any{
		"doc_count":   gorm.Expr("GREATEST(doc_count - 1, 0)"),
		"chunk_count": gorm.Expr("GREATEST(chunk_count - ?, 0)", doc.ChunkCount),
	})

	// 删本地文件
	os.Remove(doc.FilePath)
	return nil
}

// BatchDeleteDoc 批量删除文档（逐个删除，统计成功/失败数）。
func BatchDeleteDoc(ctx context.Context, docIDs []string) (deleted, failed int) {
	for _, id := range docIDs {
		if err := DeleteDoc(ctx, id); err != nil {
			g.Log().Warningf(ctx, "[knowledge] 批量删除文档 %s 失败: %v", id, err)
			failed++
		} else {
			deleted++
		}
	}
	return
}

// BatchRebuildDoc 批量重建文档索引（逐个提交，统计成功/失败数）。
// newStrategy 可选，非空时为所有文档切换分块策略。
func BatchRebuildDoc(ctx context.Context, docIDs []string, newStrategy ...string) (submitted, failed int) {
	for _, id := range docIDs {
		if err := RebuildDoc(ctx, id, newStrategy...); err != nil {
			g.Log().Warningf(ctx, "[knowledge] 批量重建文档 %s 失败: %v", id, err)
			failed++
		} else {
			submitted++
		}
	}
	return
}

// RebuildDoc 重新索引文档：
//  1. 删除旧 Milvus 向量（P0 修复：原实现遗漏此步骤，导致重建后向量重复）
//  2. 删除旧 knowledge_chunks 记录
//  3. 若 newStrategy 非空，更新文档的分块策略和配置
//  4. 重置文档状态为 pending
//  5. 重新投入 Worker Pool
func RebuildDoc(ctx context.Context, docID string, newStrategy ...string) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	var doc dao.KnowledgeDocument
	if err = db.Where("id = ?", docID).First(&doc).Error; err != nil {
		return err
	}

	// P0 修复：先清理旧 Milvus 向量，避免重建后向量重复
	deleteMilvusVectors(ctx, docID)

	// 删旧 chunks 记录
	db.Where("doc_id = ?", docID).Delete(&dao.KnowledgeChunk{})

	// 若传入新策略，更新分块策略和配置
	updates := map[string]any{
		"index_status":   "pending",
		"index_error":    "",
		"indexed_at":     nil,
		"chunk_count":    0,
		"indexed_chunks": 0,
	}
	if len(newStrategy) > 0 && newStrategy[0] != "" {
		strategy := aidoc.ChunkStrategy(newStrategy[0])
		var newCfg aidoc.ChunkConfig
		switch strategy {
		case aidoc.StrategyHierarchical:
			newCfg = aidoc.DefaultHierarchicalConfig()
		case aidoc.StrategySlidingWindow:
			newCfg = aidoc.ChunkConfig{Strategy: strategy, ChunkSize: 512, OverlapSize: 128}
		case aidoc.StrategyCode:
			// 保留原有语言配置（如有），切换策略时不丢失 language 字段
			var existingCfg aidoc.ChunkConfig
			if doc.ChunkConfig != "" {
				sonic.UnmarshalString(doc.ChunkConfig, &existingCfg)
			}
			newCfg = aidoc.ChunkConfig{Strategy: strategy, Language: existingCfg.Language}
		default:
			newCfg = aidoc.ChunkConfig{Strategy: strategy}
		}
		cfgJSON, _ := sonic.Marshal(newCfg)
		updates["chunk_strategy"] = string(strategy)
		updates["chunk_config"] = string(cfgJSON)
	}
	db.Model(&dao.KnowledgeDocument{}).Where("id = ?", docID).Updates(updates)

	GetWorkerPool().Submit(IndexTask{DocID: docID, BaseID: doc.BaseID})
	return nil
}

// ListChunkParams 分块列表查询参数。
type ListChunkParams struct {
	DocID    string
	Page     int
	PageSize int
	Keyword  string // 按 content_preview 模糊搜索
}

// ListChunk 返回文档的分块列表（分页，支持关键词搜索，按 chunk_index 升序）。
func ListChunk(ctx context.Context, params ListChunkParams) ([]dao.KnowledgeChunk, int64, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, 0, err
	}
	query := db.Model(&dao.KnowledgeChunk{}).Where("doc_id = ?", params.DocID)
	if params.Keyword != "" {
		query = query.Where("content_preview LIKE ?", "%"+params.Keyword+"%")
	}
	var total int64
	query.Count(&total)
	var chunks []dao.KnowledgeChunk
	offset := (params.Page - 1) * params.PageSize
	if err = query.Order("chunk_index ASC").Offset(offset).Limit(params.PageSize).Find(&chunks).Error; err != nil {
		return nil, 0, err
	}
	return chunks, total, nil
}

// EnableDoc 启用或禁用文档。
func EnableDoc(ctx context.Context, docID string, enabled bool) error {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	return db.Model(&dao.KnowledgeDocument{}).Where("id = ?", docID).Update("enabled", enabled).Error
}

// EnableChunks 批量启用或禁用分块。
// ids 非空时按指定 ID 列表操作；ids 为空但 docID 非空时全量操作该文档所有分块。
func EnableChunks(ctx context.Context, docID string, ids []string, enabled bool) (int, error) {
	if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
		return 0, err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return 0, err
	}
	var tx *gorm.DB
	if len(ids) > 0 {
		tx = db.Model(&dao.KnowledgeChunk{}).Where("id IN ?", ids).Update("enabled", enabled)
	} else if docID != "" {
		tx = db.Model(&dao.KnowledgeChunk{}).Where("doc_id = ?", docID).Update("enabled", enabled)
	} else {
		return 0, nil
	}
	if tx.Error != nil {
		return 0, tx.Error
	}
	return int(tx.RowsAffected), nil
}

// SearchResult RAG 检索结果条目（供 controller 组装响应）。
type SearchResult struct {
	ChunkID      string
	DocID        string
	DocTitle     string
	Content      string
	SectionTitle string
	BaseID       string
	Score        float64 // Milvus 余弦相似度分（0-1）
}

// SearchDocs 执行 RAG 检索测试：从 documents 分区按语义相似度召回分块，并按 base_id 过滤。
// 供"检索测试"面板使用，不经过查询重写，直接检索。
func SearchDocs(ctx context.Context, baseID, query string, topK int) ([]SearchResult, error) {
	if topK <= 0 || topK > 20 {
		topK = 5
	}
	rr := retrieval.GetDocumentsRetriever()
	docs, err := rr.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}

	// 过滤已禁用文档的分块
	docs = retrieval.FilterDisabledDocs(ctx, docs)

	// 按 base_id 过滤（Milvus 无分区级 base_id 过滤，在应用层补充）
	filtered := make([]*schema.Document, 0, len(docs))
	for _, d := range docs {
		if bid, ok := d.MetaData["base_id"].(string); ok && bid == baseID {
			filtered = append(filtered, d)
		}
	}
	// 若过滤后结果不足，保留 topK 原始结果（不做 base_id 过滤，退化为全库搜索）
	if len(filtered) == 0 {
		filtered = docs
	}
	if len(filtered) > topK {
		filtered = filtered[:topK]
	}

	results := make([]SearchResult, 0, len(filtered))
	for _, d := range filtered {
		r := SearchResult{
			ChunkID: d.ID,
			Content: d.Content,
			Score:   d.Score(),
		}
		if v, ok := d.MetaData["doc_id"].(string); ok {
			r.DocID = v
		}
		if v, ok := d.MetaData["doc_title"].(string); ok {
			r.DocTitle = v
		}
		if v, ok := d.MetaData["section_title"].(string); ok {
			r.SectionTitle = v
		}
		if v, ok := d.MetaData["base_id"].(string); ok {
			r.BaseID = v
		}
		results = append(results, r)
	}
	return results, nil
}

// buildDocIndex 在 Worker Pool goroutine 中执行文档向量索引，状态机流转：
//
//	pending → indexing → completed（成功）
//	pending → indexing → failed（失败，index_error 记录原因，RetryCount < maxRetries 时自动重试）
//
// 执行步骤：
//  1. 查 MySQL 获取文档记录（文件路径、分块配置）
//  2. 状态置为 indexing
//  3. 解析 ChunkConfig（JSON → struct），缺省时使用 DefaultHierarchicalConfig
//  4. 调用 pipeline.BuildAndIndex（解析文件 → 分块 → 写 Milvus documents 分区）
//  5. 提前更新 chunk_count（用于进度计算分母）
//  6. 分批写 knowledge_chunks（每批 100 条），每批更新 indexed_chunks（进度追踪）
//  7. 更新文档状态为 completed + 更新知识库 chunk_count 累加
func buildDocIndex(ctx context.Context, task IndexTask) error {
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}

	// 查询文档记录
	var doc dao.KnowledgeDocument
	if err = db.Where("id = ?", task.DocID).First(&doc).Error; err != nil {
		return err
	}
	g.Log().Infof(ctx, "[knowledge] 开始解析文档 %s（%s，策略=%s，大小=%d字节）",
		doc.Name, doc.FileType, doc.ChunkStrategy, doc.FileSize)

	// 更新状态为 indexing，记录开始时间
	indexStartAt := time.Now()
	db.Model(&dao.KnowledgeDocument{}).Where("id = ?", task.DocID).Update("index_status", "indexing")

	// 解析分块配置
	var chunkCfg aidoc.ChunkConfig
	if doc.ChunkConfig != "" {
		sonic.UnmarshalString(doc.ChunkConfig, &chunkCfg)
	}
	if chunkCfg.Strategy == "" {
		chunkCfg = aidoc.DefaultHierarchicalConfig()
	}

	// 获取文档主标题（文件名去扩展名）
	docTitle := strings.TrimSuffix(doc.Name, filepath.Ext(doc.Name))

	// 执行索引（解析 + 分块 + Milvus 写入）
	childSize := chunkCfg.ChildChunkSize
	if childSize == 0 {
		childSize = chunkCfg.ChunkSize
	}
	g.Log().Infof(ctx, "[knowledge] 开始分块+向量化文档 %s（策略=%s，childChunkSize=%d）",
		doc.Name, chunkCfg.Strategy, childSize)
	chunks, err := pipeline.BuildAndIndex(ctx, pipeline.IndexInput{
		FilePath: doc.FilePath,
		BaseID:   task.BaseID,
		DocID:    task.DocID,
		DocTitle: docTitle,
		Config:   chunkCfg,
	})
	if err != nil {
		db.Model(&dao.KnowledgeDocument{}).Where("id = ?", task.DocID).Updates(map[string]any{
			"index_status": "failed",
			"index_error":  err.Error(),
		})
		return fmt.Errorf("index doc %s: %w", task.DocID, err)
	}
	if len(chunks) == 0 {
		db.Model(&dao.KnowledgeDocument{}).Where("id = ?", task.DocID).Updates(map[string]any{
			"index_status": "failed",
			"index_error":  "文档解析结果为空，可能是纯图片 PDF 或格式不支持",
		})
		return fmt.Errorf("index doc %s: no chunks extracted", task.DocID)
	}
	g.Log().Infof(ctx, "[knowledge] 文档 %s 分块完成，共 %d 块，开始写入 MySQL chunks 记录", doc.Name, len(chunks))

	// 提前更新 chunk_count（写入 MySQL chunks 之前），用于前端进度条分母
	db.Model(&dao.KnowledgeDocument{}).Where("id = ?", task.DocID).
		UpdateColumn("chunk_count", len(chunks))

	// 构建 knowledge_chunks 记录（存储完整子块文本，不截断）
	dbChunks := make([]dao.KnowledgeChunk, 0, len(chunks))
	for _, c := range chunks {
		dbChunks = append(dbChunks, dao.KnowledgeChunk{
			ID:             c.ID,
			DocID:          task.DocID,
			ChunkIndex:     c.ChunkIndex,
			ContentPreview: c.Content, // 存储完整子块文本（type:text，无长度限制）
			SectionTitle:   c.SectionTitle,
			CharCount:      c.CharCount,
			Enabled:        true,
		})
	}

	// 分批写入 knowledge_chunks，每批完成后更新 indexed_chunks（进度追踪）
	const chunkBatch = 100
	if len(dbChunks) > 0 {
		for batchStart := 0; batchStart < len(dbChunks); batchStart += chunkBatch {
			end := batchStart + chunkBatch
			if end > len(dbChunks) {
				end = len(dbChunks)
			}
			batch := dbChunks[batchStart:end]
			// 检查批量写入错误
			if writeErr := db.CreateInBatches(batch, len(batch)).Error; writeErr != nil {
				g.Log().Warningf(ctx, "[knowledge] 文档 %s 写入 chunks 批次 [%d,%d) 失败: %v",
					task.DocID, batchStart, end, writeErr)
			}
			// 每批成功后更新进度计数
			db.Model(&dao.KnowledgeDocument{}).Where("id = ?", task.DocID).
				UpdateColumn("indexed_chunks", gorm.Expr("indexed_chunks + ?", len(batch)))
		}
	}

	// 更新文档状态和知识库统计
	now := time.Now()
	durationMs := now.Sub(indexStartAt).Milliseconds()
	db.Model(&dao.KnowledgeDocument{}).Where("id = ?", task.DocID).Updates(map[string]any{
		"index_status":      "completed",
		"index_error":       "",
		"indexed_at":        now,
		"index_duration_ms": durationMs,
		"chunk_count":       len(chunks),
		"indexed_chunks":    len(chunks), // 确保最终值与 chunk_count 一致
	})
	db.Model(&dao.KnowledgeBase{}).Where("id = ?", task.BaseID).UpdateColumn(
		"chunk_count", gorm.Expr("chunk_count + ?", len(chunks)),
	)
	g.Log().Infof(ctx, "[knowledge] 文档 %s 索引完成，共 %d 块", task.DocID, len(chunks))
	return nil
}
