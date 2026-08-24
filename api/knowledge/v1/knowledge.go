// Package v1 知识库管理 API 路由定义。
// 路由前缀：/api/knowledge/v1/...（GoFrame 注册在 /api 路由组下，path 含完整子路径）
package v1

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// ── 知识库（Base）─────────────────────────────────────────────────────────────

// BaseListReq 知识库列表请求
type BaseListReq struct {
	g.Meta `path:"/knowledge/v1/bases/list" method:"GET" tags:"Knowledge" summary:"获取知识库列表"`
}

// BaseListRes 知识库列表响应
type BaseListRes struct {
	List []BaseItem `json:"list"`
}

// BaseItem 知识库简要信息
type BaseItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DocCount    int    `json:"doc_count"`   // 文档总数
	ChunkCount  int    `json:"chunk_count"` // 向量分块总数
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"` // 最近修改时间
}

// BaseCreateReq 创建知识库请求
type BaseCreateReq struct {
	g.Meta      `path:"/knowledge/v1/bases/create" method:"POST" tags:"Knowledge" summary:"创建知识库"`
	Name        string `json:"name" v:"required#知识库名称不能为空"`
	Description string `json:"description"`
}

// BaseCreateRes 创建知识库响应
type BaseCreateRes struct {
	BaseItem
}

// BaseDeleteReq 删除知识库请求
type BaseDeleteReq struct {
	g.Meta `path:"/knowledge/v1/bases/delete" method:"POST" tags:"Knowledge" summary:"删除知识库"`
	ID     string `json:"id" v:"required#知识库ID不能为空"`
}

// BaseDeleteRes 删除知识库响应
type BaseDeleteRes struct{}

// ── 文档（Doc）────────────────────────────────────────────────────────────────

// DocUploadReq 文档上传请求（multipart/form-data）
type DocUploadReq struct {
	g.Meta        `path:"/knowledge/v1/docs/upload" method:"POST" tags:"Knowledge" summary:"上传知识文档"`
	BaseID        string            `json:"base_id" v:"required#知识库ID不能为空"` // 目标知识库 ID
	ChunkStrategy string            `json:"chunk_strategy"`                 // sliding_window / hierarchical / code
	ChunkSize     int               `json:"chunk_size"`                     // 自定义子块大小（rune 数），0 使用默认值
	File          *ghttp.UploadFile `json:"file" type:"file" v:"required#文件不能为空"`
}

// DocUploadRes 文档上传响应（文档已入库，索引异步执行）
type DocUploadRes struct {
	DocID       string `json:"doc_id"`
	Name        string `json:"name"`
	IndexStatus string `json:"index_status"` // 初始为 "pending"
}

// DocListReq 文档列表请求
type DocListReq struct {
	g.Meta   `path:"/knowledge/v1/docs/list" method:"GET" tags:"Knowledge" summary:"获取文档列表"`
	BaseID   string `json:"base_id" v:"required#知识库ID不能为空"` // 所属知识库 ID
	Page     int    `json:"page"      d:"1"`
	PageSize int    `json:"page_size" d:"20"`
	Keyword  string `json:"keyword"`   // 按文件名模糊搜索（可选）
	Status   string `json:"status"`    // 按索引状态过滤：pending/indexing/completed/failed（可选）
	FileType string `json:"file_type"` // 按文件类型过滤：pdf/docx/md 等（可选）
}

// DocListRes 文档列表响应
type DocListRes struct {
	List  []DocItem `json:"list"`
	Total int64     `json:"total"`
}

// DocItem 文档简要信息
type DocItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	FileSize      int64  `json:"file_size"`
	FileType      string `json:"file_type"`
	ChunkCount    int    `json:"chunk_count"`
	IndexedChunks int    `json:"indexed_chunks"`              // 已写入 MySQL 的分块数（进度追踪）
	ChunkStrategy string `json:"chunk_strategy"`              // 分块策略：fixed_size / hierarchical
	IndexStatus   string `json:"index_status"`                // pending / indexing / completed / failed
	IndexError    string `json:"index_error,omitempty"`       // 失败时的错误信息
	IndexedAt     string `json:"indexed_at,omitempty"`        // 最近一次索引完成时间
	IndexDuration int64  `json:"index_duration_ms,omitempty"` // 最近一次索引耗时（毫秒）
	Enabled       bool   `json:"enabled"`                     // 是否启用（禁用时不参与 RAG 检索）
	CreatedAt     string `json:"created_at"`
}

// DocDeleteReq 删除文档请求
type DocDeleteReq struct {
	g.Meta `path:"/knowledge/v1/docs/delete" method:"POST" tags:"Knowledge" summary:"删除文档"`
	ID     string `json:"id" v:"required#文档ID不能为空"`
}

// DocDeleteRes 删除文档响应
type DocDeleteRes struct{}

// DocRebuildReq 重新索引文档请求
type DocRebuildReq struct {
	g.Meta        `path:"/knowledge/v1/docs/rebuild" method:"POST" tags:"Knowledge" summary:"重新索引文档"`
	ID            string `json:"id" v:"required#文档ID不能为空"`
	ChunkStrategy string `json:"chunk_strategy"` // 可选：切换分块策略（空=保持原策略）fixed_size/hierarchical
}

// DocRebuildRes 重新索引文档响应
type DocRebuildRes struct{}

// ── 分块（Chunk）──────────────────────────────────────────────────────────────

// ChunkListReq 分块列表请求
type ChunkListReq struct {
	g.Meta   `path:"/knowledge/v1/chunks/list" method:"GET" tags:"Knowledge" summary:"获取文档分块列表"`
	DocID    string `json:"doc_id" v:"required#文档ID不能为空"`
	Page     int    `json:"page"      d:"1"`
	PageSize int    `json:"page_size" d:"20"`
	Keyword  string `json:"keyword"` // 按分块内容关键词搜索（可选，LIKE 匹配 content_preview）
}

// ChunkListRes 分块列表响应
type ChunkListRes struct {
	List  []ChunkItem `json:"list"`
	Total int64       `json:"total"`
}

// ChunkItem 分块简要信息（向量本体在 Milvus，此处只存展示用元数据）
type ChunkItem struct {
	ID             string `json:"id"`
	ChunkIndex     int    `json:"chunk_index"`
	ContentPreview string `json:"content_preview"`         // 前 200 字符预览
	SectionTitle   string `json:"section_title,omitempty"` // 父子/结构化分块时的章节标题
	CharCount      int    `json:"char_count"`
	Enabled        bool   `json:"enabled"`    // 是否启用（禁用时不参与 RAG 检索）
	UpdatedAt      string `json:"updated_at"` // 最近更新时间
}

// ── 详情接口 ──────────────────────────────────────────────────────────────────

// BaseDetailReq 获取知识库详情请求
type BaseDetailReq struct {
	g.Meta `path:"/knowledge/v1/bases/detail" method:"GET" tags:"Knowledge" summary:"获取知识库详情"`
	ID     string `json:"id" v:"required#知识库ID不能为空"`
}

// BaseDetailRes 知识库详情响应
type BaseDetailRes struct {
	BaseItem
}

// DocDetailReq 获取文档详情请求
type DocDetailReq struct {
	g.Meta `path:"/knowledge/v1/docs/detail" method:"GET" tags:"Knowledge" summary:"获取文档详情"`
	ID     string `json:"id" v:"required#文档ID不能为空"`
}

// DocDetailRes 文档详情响应
type DocDetailRes struct {
	DocItem
}

// ── 批量操作 ──────────────────────────────────────────────────────────────────

// DocBatchDeleteReq 批量删除文档请求
type DocBatchDeleteReq struct {
	g.Meta `path:"/knowledge/v1/docs/batch_delete" method:"POST" tags:"Knowledge" summary:"批量删除文档"`
	IDs    []string `json:"ids" v:"required#文档ID列表不能为空"`
}

// DocBatchDeleteRes 批量删除文档响应
type DocBatchDeleteRes struct {
	Deleted int `json:"deleted"` // 成功删除数
	Failed  int `json:"failed"`  // 失败数
}

// DocBatchRebuildReq 批量重建索引请求
type DocBatchRebuildReq struct {
	g.Meta        `path:"/knowledge/v1/docs/batch_rebuild" method:"POST" tags:"Knowledge" summary:"批量重建文档索引"`
	IDs           []string `json:"ids" v:"required#文档ID列表不能为空"`
	ChunkStrategy string   `json:"chunk_strategy"` // 可选：为所有文档切换分块策略（空=各自保持原策略）
}

// DocBatchRebuildRes 批量重建索引响应
type DocBatchRebuildRes struct {
	Submitted int `json:"submitted"` // 成功提交数
	Failed    int `json:"failed"`    // 失败数
}

// ── 队列状态（Queue）──────────────────────────────────────────────────────────

// QueueStatusReq 索引队列状态请求
type QueueStatusReq struct {
	g.Meta `path:"/knowledge/v1/queue/status" method:"GET" tags:"Knowledge" summary:"获取索引队列状态"`
}

// QueueStatusRes 索引队列状态响应
type QueueStatusRes struct {
	QueueLength int `json:"queue_length"` // 当前等待索引的任务数
}

// ── 启用/禁用 ──────────────────────────────────────────────────────────────────

// DocEnableReq 启用/禁用文档请求
type DocEnableReq struct {
	g.Meta  `path:"/knowledge/v1/docs/enable" method:"POST" tags:"Knowledge" summary:"启用或禁用文档"`
	ID      string `json:"id" v:"required#文档ID不能为空"`
	Enabled bool   `json:"enabled"`
}

// DocEnableRes 启用/禁用文档响应
type DocEnableRes struct{}

// ChunkEnableReq 批量启用/禁用分块请求
type ChunkEnableReq struct {
	g.Meta  `path:"/knowledge/v1/chunks/enable" method:"POST" tags:"Knowledge" summary:"批量启用或禁用分块"`
	DocID   string   `json:"doc_id"` // 全量操作时传 doc_id（ids 为空时按 doc_id 全量更新）
	IDs     []string `json:"ids"`    // 指定分块 ID 列表（优先于 doc_id 全量）
	Enabled bool     `json:"enabled"`
}

// ChunkEnableRes 批量启用/禁用分块响应
type ChunkEnableRes struct {
	Updated int `json:"updated"` // 成功更新的分块数
}

// ── RAG 检索测试 ───────────────────────────────────────────────────────────────

// SearchReq RAG 检索测试请求（输入查询词，返回召回分块）
type SearchReq struct {
	g.Meta `path:"/knowledge/v1/search" method:"POST" tags:"Knowledge" summary:"RAG检索测试"`
	BaseID string `json:"base_id" v:"required#知识库ID不能为空"` // 限定检索范围的知识库 ID
	Query  string `json:"query"   v:"required#查询词不能为空"`
	TopK   int    `json:"top_k"   d:"5"` // 返回文档数，默认 5，最大 20
}

// SearchRes RAG 检索测试响应
type SearchRes struct {
	Results []SearchResultItem `json:"results"`
	Cached  bool               `json:"cached"` // 是否命中语义缓存
}

// SearchResultItem 单条召回结果
type SearchResultItem struct {
	ChunkID      string  `json:"chunk_id"`
	DocID        string  `json:"doc_id"`
	DocTitle     string  `json:"doc_title,omitempty"`     // 文档标题（来自 Milvus metadata.doc_title）
	Content      string  `json:"content"`                 // 子块文本
	SectionTitle string  `json:"section_title,omitempty"` // 章节标题
	BaseID       string  `json:"base_id,omitempty"`       // 所属知识库 ID
	Score        float64 `json:"score,omitempty"`         // 余弦相似度分（0-1）
}
