package knowledge_index_pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
	"unicode/utf8"

	aidoc "SentinelOps/internal/ai/document"
	"SentinelOps/internal/ai/document/chunkers"
	"SentinelOps/internal/ai/document/parsers"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// IndexInput 知识库索引管道的输入，包含文件路径、分块配置和元数据。
// 分块配置在运行时传入，无需重建单例 Graph。
type IndexInput struct {
	FilePath       string
	BaseID         string            // 所属知识库ID（可选，metadata 注入）
	DocID          string            // 文档ID（可选，Milvus 删除依据）
	DocTitle       string            // 文档主标题（metadata 注入）
	SourceVersion  string            // phase03 Evidence 字段快照
	ContentHash    string            // phase03 内容 hash 快照
	AccessScope    string            // phase03 授权 Scope 快照
	IndexedVersion uint64            // phase03 索引版本快照
	Config         aidoc.ChunkConfig // 含 hierarchical 策略参数
}

// BuildAndIndex 执行知识索引并返回 ChunkResult 列表（供知识库服务写 MySQL）。
// 该函数在 Worker Pool 中被调用，不走 Eino Graph（直接调用解析+索引），
// 以便拿到完整的 ChunkResult（含 section_title、parent_content）。
//
// 策略自动推断：Config.Strategy 为空时按扩展名填充（所有格式均为 hierarchical），其余参数保留。
func BuildAndIndex(ctx context.Context, input IndexInput) ([]aidoc.ChunkResult, error) {
	cfg := input.Config
	if cfg.Strategy == "" {
		// 仅补全策略，保留调用方已设置的 ChunkSize / ParentChunkSize 等字段
		cfg.Strategy = aidoc.StrategyForExt(filepath.Ext(input.FilePath))
	}

	var chunks []aidoc.ChunkResult
	var docs []*schema.Document

	if cfg.Strategy == aidoc.StrategyHierarchical {
		// Hierarchical 策略：父子分块，结构化解析保留章节标题，携带章节 metadata
		t0 := time.Now()
		// 使用 DocTitle（原始文件名）作为默认标题，避免 UUID 文件名污染
		defaultTitle := input.DocTitle
		if defaultTitle == "" {
			defaultTitle = filepath.Base(input.FilePath)
		}
		parsed, err := parsers.ParseFileWithStructure(input.FilePath, defaultTitle)
		if err != nil {
			return nil, err
		}
		g.Log().Infof(ctx, "[index] ParseFileWithStructure: %v", time.Since(t0))
		t1 := time.Now()

		chunks = aidoc.ChunkDocument(parsed, cfg)
		g.Log().Infof(ctx, "[index] ChunkDocument: %v, chunks=%d", time.Since(t1), len(chunks))
	} else if cfg.Strategy == aidoc.StrategyCode {
		// Code 策略：语法感知分块，按函数/类边界切分，保留函数名作为 SectionTitle
		t0 := time.Now()
		text, err := parsers.ParseFileToPlainText(input.FilePath)
		if err != nil {
			return nil, err
		}
		g.Log().Infof(ctx, "[index] ParseFileToPlainText: %v", time.Since(t0))
		t1 := time.Now()

		chunks = chunkers.Code(text, cfg.Language, cfg.ChunkSize)
		for i := range chunks {
			chunks[i].ChunkIndex = i
			chunks[i].CharCount = utf8.RuneCountInString(chunks[i].Content)
		}
		g.Log().Infof(ctx, "[index] Code chunking: %v, chunks=%d", time.Since(t1), len(chunks))
	} else {
		// SlidingWindow 策略：滑动窗口分块，无章节标题
		t0 := time.Now()
		text, err := parsers.ParseFileToPlainText(input.FilePath)
		if err != nil {
			return nil, err
		}
		g.Log().Infof(ctx, "[index] ParseFileToPlainText: %v", time.Since(t0))
		t1 := time.Now()

		rawChunks := aidoc.ChunkText(text, cfg)
		chunks = make([]aidoc.ChunkResult, len(rawChunks))
		for i, c := range rawChunks {
			chunks[i] = aidoc.ChunkResult{
				Content:    c,
				ChunkIndex: i,
				CharCount:  utf8.RuneCountInString(c),
			}
		}
		g.Log().Infof(ctx, "[index] ChunkText: %v, chunks=%d", time.Since(t1), len(chunks))
	}

	// 构建 Milvus 文档（携带完整 metadata）
	for i, c := range chunks {
		id := uuid.New().String()
		chunks[i].ID = id
		meta := map[string]any{
			"_type":           "document",
			"base_id":         input.BaseID,
			"doc_id":          input.DocID,
			"doc_title":       input.DocTitle,
			"chunk_index":     c.ChunkIndex,
			"section_title":   c.SectionTitle,
			"source_version":  input.SourceVersion,
			"content_hash":    input.ContentHash,
			"access_scope":    input.AccessScope,
			"indexed_version": input.IndexedVersion,
		}
		if c.ParentContent != "" {
			meta["parent_content"] = aidoc.TruncateToMaxBytes(c.ParentContent, 6000)
		}
		docs = append(docs, &schema.Document{
			ID:       id,
			Content:  aidoc.TruncateToMaxBytes(c.Content, 8000),
			MetaData: meta,
		})
	}

	if len(docs) == 0 {
		return chunks, nil
	}

	// 获取 documents 分区索引器并串行分批写入（批次上限由当前 Embedding 配置约束）
	idx, err := newIndexer(ctx)
	if err != nil {
		return nil, err
	}
	const embedBatchSize = 10

	// 串行执行批次写入，避免触发 Milvus 速率限制（rate=0.1，即每10秒1次）
	tEmbed := time.Now()
	batchCount := 0
	for i := 0; i < len(docs); i += embedBatchSize {
		end := i + embedBatchSize
		if end > len(docs) {
			end = len(docs)
		}
		if _, err := idx.Store(ctx, docs[i:end]); err != nil {
			return nil, fmt.Errorf("store batch [%d,%d): %w", i, end, err)
		}
		batchCount++
		// 每批次间隔 200ms，确保不超过 Milvus 速率限制
		if i+embedBatchSize < len(docs) {
			time.Sleep(200 * time.Millisecond)
		}
	}
	g.Log().Infof(ctx, "[index] Embedding+Store: %v, batches=%d", time.Since(tEmbed), batchCount)
	if err != nil {
		return nil, err
	}
	return chunks, nil
}
