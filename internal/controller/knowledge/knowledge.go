// Package knowledge 知识库 HTTP 控制器。
package knowledge

import (
	"context"
	"io"
	"path/filepath"

	v1 "SentinelOps/api/knowledge/v1"
	aidoc "SentinelOps/internal/ai/document"
	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/policy"
	knowledgesvc "SentinelOps/internal/service/knowledge"
)

type controllerV1 struct{}

// NewV1 返回知识库控制器实例。
func NewV1() *controllerV1 {
	return &controllerV1{}
}

// BaseList 获取知识库列表
func (c *controllerV1) BaseList(ctx context.Context, req *v1.BaseListReq) (res *v1.BaseListRes, err error) {
	bases, err := knowledgesvc.ListBases(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]v1.BaseItem, len(bases))
	for i, base := range bases {
		items[i] = v1.BaseItem{
			ID:          base.ID,
			Name:        base.Name,
			Description: base.Description,
			DocCount:    base.DocCount,
			ChunkCount:  base.ChunkCount,
			CreatedAt:   base.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:   base.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	return &v1.BaseListRes{List: items}, nil
}

// BaseCreate 创建知识库
func (c *controllerV1) BaseCreate(ctx context.Context, req *v1.BaseCreateReq) (res *v1.BaseCreateRes, err error) {
	base, err := knowledgesvc.CreateBase(ctx, req.Name, req.Description)
	if err != nil {
		return nil, err
	}
	return &v1.BaseCreateRes{BaseItem: v1.BaseItem{
		ID:          base.ID,
		Name:        base.Name,
		Description: base.Description,
		DocCount:    base.DocCount,
		ChunkCount:  base.ChunkCount,
		CreatedAt:   base.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   base.UpdatedAt.Format("2006-01-02 15:04:05"),
	}}, nil
}

// BaseDelete 删除知识库
func (c *controllerV1) BaseDelete(ctx context.Context, req *v1.BaseDeleteReq) (res *v1.BaseDeleteRes, err error) {
	return &v1.BaseDeleteRes{}, knowledgesvc.DeleteBase(ctx, req.ID)
}

// DocUpload 上传文档并提交异步索引任务。
// 执行步骤：
//  1. 读取 multipart 文件内容（ghttp.UploadFile.Open() → io.ReadAll）
//  2. 构建 ChunkConfig：策略缺省回填为 hierarchical；
//     hierarchical 策略下若 Parent/Child/Overlap 为零则填充 DefaultHierarchicalConfig 默认值
//  3. 调用 UploadDoc：保存文件到磁盘 + 写 MySQL + 投递 Worker Pool
//  4. 返回 doc_id + index_status("pending")，前端轮询状态
func (c *controllerV1) DocUpload(ctx context.Context, req *v1.DocUploadReq) (res *v1.DocUploadRes, err error) {
	// 读取上传文件内容
	f, err := req.File.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fileContent, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	// 构建分块配置：未传 strategy 时按文件扩展名自动选择
	var chunkCfg aidoc.ChunkConfig
	if req.ChunkStrategy != "" {
		chunkCfg = aidoc.DefaultChunkConfig()
		chunkCfg.Strategy = aidoc.ChunkStrategy(req.ChunkStrategy)
	} else {
		chunkCfg = aidoc.ConfigForExt(filepath.Ext(req.File.Filename))
	}
	if req.ChunkSize > 0 {
		chunkCfg.ChildChunkSize = req.ChunkSize
	}

	doc, err := knowledgesvc.UploadDoc(ctx, req.BaseID, req.File.Filename, fileContent, chunkCfg)
	if err != nil {
		return nil, err
	}
	return &v1.DocUploadRes{
		DocID:       doc.ID,
		Name:        doc.Name,
		IndexStatus: doc.IndexStatus,
	}, nil
}

// DocList 获取文档列表（支持按文件名关键词、索引状态、文件类型过滤，服务端分页）
func (c *controllerV1) DocList(ctx context.Context, req *v1.DocListReq) (res *v1.DocListRes, err error) {
	docs, total, err := knowledgesvc.ListDoc(ctx, knowledgesvc.ListDocParams{
		BaseID:   req.BaseID,
		Page:     req.Page,
		PageSize: req.PageSize,
		Keyword:  req.Keyword,
		Status:   req.Status,
		FileType: req.FileType,
	})
	if err != nil {
		return nil, err
	}
	items := make([]v1.DocItem, len(docs))
	for i, doc := range docs {
		item := v1.DocItem{
			ID:            doc.ID,
			Name:          doc.Name,
			FileSize:      doc.FileSize,
			FileType:      doc.FileType,
			ChunkCount:    doc.ChunkCount,
			IndexedChunks: doc.IndexedChunks,
			ChunkStrategy: doc.ChunkStrategy,
			IndexStatus:   doc.IndexStatus,
			IndexError:    doc.IndexError,
			Enabled:       doc.Enabled,
			CreatedAt:     doc.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if doc.IndexedAt != nil {
			item.IndexedAt = doc.IndexedAt.Format("2006-01-02 15:04:05")
			item.IndexDuration = doc.IndexDurationMs
		}
		items[i] = item
	}
	return &v1.DocListRes{List: items, Total: total}, nil
}

// DocDelete 删除文档
func (c *controllerV1) DocDelete(ctx context.Context, req *v1.DocDeleteReq) (res *v1.DocDeleteRes, err error) {
	return &v1.DocDeleteRes{}, knowledgesvc.DeleteDoc(ctx, req.ID)
}

// DocRebuild 重新索引文档
func (c *controllerV1) DocRebuild(ctx context.Context, req *v1.DocRebuildReq) (res *v1.DocRebuildRes, err error) {
	return &v1.DocRebuildRes{}, knowledgesvc.RebuildDoc(ctx, req.ID, req.ChunkStrategy)
}

// ChunkList 获取文档分块列表（支持按内容关键词搜索，服务端分页）
func (c *controllerV1) ChunkList(ctx context.Context, req *v1.ChunkListReq) (res *v1.ChunkListRes, err error) {
	chunks, total, err := knowledgesvc.ListChunk(ctx, knowledgesvc.ListChunkParams{
		DocID:    req.DocID,
		Page:     req.Page,
		PageSize: req.PageSize,
		Keyword:  req.Keyword,
	})
	if err != nil {
		return nil, err
	}
	items := make([]v1.ChunkItem, len(chunks))
	for i, c := range chunks {
		updatedAt := "—"
		if !c.UpdatedAt.IsZero() {
			updatedAt = c.UpdatedAt.Format("2006-01-02 15:04:05")
		}
		items[i] = v1.ChunkItem{
			ID:             c.ID,
			ChunkIndex:     c.ChunkIndex,
			ContentPreview: c.ContentPreview,
			SectionTitle:   c.SectionTitle,
			CharCount:      c.CharCount,
			Enabled:        c.Enabled,
			UpdatedAt:      updatedAt,
		}
	}
	return &v1.ChunkListRes{List: items, Total: total}, nil
}

// BaseDetail 获取知识库详情
func (c *controllerV1) BaseDetail(ctx context.Context, req *v1.BaseDetailReq) (res *v1.BaseDetailRes, err error) {
	base, err := knowledgesvc.GetBase(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return &v1.BaseDetailRes{BaseItem: v1.BaseItem{
		ID:          base.ID,
		Name:        base.Name,
		Description: base.Description,
		DocCount:    base.DocCount,
		ChunkCount:  base.ChunkCount,
		CreatedAt:   base.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   base.UpdatedAt.Format("2006-01-02 15:04:05"),
	}}, nil
}

// DocDetail 获取文档详情
func (c *controllerV1) DocDetail(ctx context.Context, req *v1.DocDetailReq) (res *v1.DocDetailRes, err error) {
	doc, err := knowledgesvc.GetDoc(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	item := v1.DocItem{
		ID:            doc.ID,
		Name:          doc.Name,
		FileSize:      doc.FileSize,
		FileType:      doc.FileType,
		ChunkCount:    doc.ChunkCount,
		IndexedChunks: doc.IndexedChunks,
		ChunkStrategy: doc.ChunkStrategy,
		IndexStatus:   doc.IndexStatus,
		IndexError:    doc.IndexError,
		Enabled:       doc.Enabled,
		CreatedAt:     doc.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	if doc.IndexedAt != nil {
		item.IndexedAt = doc.IndexedAt.Format("2006-01-02 15:04:05")
		item.IndexDuration = doc.IndexDurationMs
	}
	return &v1.DocDetailRes{DocItem: item}, nil
}

// DocBatchDelete 批量删除文档
func (c *controllerV1) DocBatchDelete(ctx context.Context, req *v1.DocBatchDeleteReq) (res *v1.DocBatchDeleteRes, err error) {
	deleted, failed := knowledgesvc.BatchDeleteDoc(ctx, req.IDs)
	return &v1.DocBatchDeleteRes{Deleted: deleted, Failed: failed}, nil
}

// DocBatchRebuild 批量重建文档索引
func (c *controllerV1) DocBatchRebuild(ctx context.Context, req *v1.DocBatchRebuildReq) (res *v1.DocBatchRebuildRes, err error) {
	submitted, failed := knowledgesvc.BatchRebuildDoc(ctx, req.IDs, req.ChunkStrategy)
	return &v1.DocBatchRebuildRes{Submitted: submitted, Failed: failed}, nil
}

// QueueStatus 获取索引队列状态
func (c *controllerV1) QueueStatus(ctx context.Context, req *v1.QueueStatusReq) (res *v1.QueueStatusRes, err error) {
	pool := knowledgesvc.GetWorkerPool()
	return &v1.QueueStatusRes{QueueLength: len(pool.GetQueue())}, nil
}

// DocEnable 启用或禁用文档
func (c *controllerV1) DocEnable(ctx context.Context, req *v1.DocEnableReq) (res *v1.DocEnableRes, err error) {
	return &v1.DocEnableRes{}, knowledgesvc.EnableDoc(ctx, req.ID, req.Enabled)
}

// ChunkEnable 批量启用或禁用分块
func (c *controllerV1) ChunkEnable(ctx context.Context, req *v1.ChunkEnableReq) (res *v1.ChunkEnableRes, err error) {
	updated, err := knowledgesvc.EnableChunks(ctx, req.DocID, req.IDs, req.Enabled)
	if err != nil {
		return nil, err
	}
	return &v1.ChunkEnableRes{Updated: updated}, nil
}

// Search RAG 检索测试：从知识库召回最相关的文档分块，供管理员验证检索效果
func (c *controllerV1) Search(ctx context.Context, req *v1.SearchReq) (res *v1.SearchRes, err error) {
	// 文档分区检索必须携带服务端身份作用域，避免跨用户/知识库泄露，
	// 同时让检索缓存按身份和知识库隔离。知识库文档当前使用 public 访问范围。
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	scope := evidence.Scope{
		UserID:          identity.UserID,
		Role:            string(identity.Role),
		KnowledgeBaseID: req.BaseID,
		AccessScope:     "public",
	}
	if version, versionErr := knowledgesvc.CurrentIndexedVersion(ctx, req.BaseID); versionErr == nil {
		scope.IndexedVersion = version
	}
	ctx = evidence.WithScope(ctx, scope)
	results, err := knowledgesvc.SearchDocs(ctx, req.BaseID, req.Query, req.TopK)
	if err != nil {
		return nil, err
	}
	items := make([]v1.SearchResultItem, len(results))
	for i, r := range results {
		items[i] = v1.SearchResultItem{
			ChunkID:      r.ChunkID,
			DocID:        r.DocID,
			DocTitle:     r.DocTitle,
			Content:      r.Content,
			SectionTitle: r.SectionTitle,
			BaseID:       r.BaseID,
			Score:        r.Score,
		}
	}
	return &v1.SearchRes{Results: items}, nil
}
