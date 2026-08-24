// Package event 提供 ReAct Agent 可调用的安全事件相关工具。
package event

import (
	"context"
	"encoding/json"
	"fmt"

	"SentinelOps/internal/ai/retrieval"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
)

// SearchSimilarEventsInput 相似事件语义搜索参数。
//
// 注意：Limit 受 Retriever 初始化时的 topK 配置约束（默认 3），
// 实际返回条数为 min(Limit, topK)，无法在运行时突破该上限。
type SearchSimilarEventsInput struct {
	Query string `json:"query" jsonschema:"description=语义搜索关键词，支持自然语言描述，如 'Apache 远程代码执行漏洞'"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=返回条数，默认 5，受系统 topK 配置约束"`
}

// SimilarEventResult 相似事件搜索结果，融合 Milvus 向量内容与 MySQL 结构化元数据。
//
// 字段来源：
//   - ID、Content、Score：Milvus 向量检索返回
//   - 其余字段：MySQL events 表元数据补充（以 ID 为键 JOIN）
//
// 设计说明（双源存储架构）：
//   - Milvus 存储事件完整文本（content，max 8192 字节），支持语义向量检索
//   - MySQL 仅存储结构化可查询字段，不存 content，避免大字段拖慢索引与查询
//   - 两者以事件 ID 为桥梁在查询时动态合并，兼顾检索性能与数据完整性
type SimilarEventResult struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`      // MySQL：事件标题
	Content   string  `json:"content"`    // Milvus：完整事件文本
	Score     float64 `json:"score"`      // 余弦相似度，范围 [-1,1]，越接近 1 越相似
	EventType string  `json:"event_type"` // MySQL：来源大类，github / rss
	Severity  string  `json:"severity"`   // MySQL：critical / high / medium / low
	RiskScore float64 `json:"risk_score"` // MySQL：风险评分 0-10，0 表示未评估
	CveID     string  `json:"cve_id"`     // MySQL：CVE 编号，如 CVE-2024-12345
	Status    string  `json:"status"`     // MySQL：new / processing / resolved / ignored
	Source    string  `json:"source"`     // MySQL：具体订阅源名称
	CreatedAt string  `json:"created_at"` // MySQL：事件发现时间
}

// NewSearchSimilarEventsTool 创建 search_similar_events 工具。
//
// # 相似度识别原理
//
// 工具通过以下流程识别语义相似事件：
//
//  1. 向量化：调用 DashScope qwen3.7-text-embedding 将查询文本嵌入为 2048 维 float32 向量。
//
//  2. 语义缓存比对（Redis）：将查询向量与 Redis 中已缓存的历史查询向量逐一计算余弦相似度，
//     相似度 ≥ 0.85（可配置）则缓存命中，直接返回上次 Milvus 检索结果，跳过 ANN 搜索。
//
//  3. ANN 向量检索（Milvus）：缓存未命中时，将查询向量送入 biz 集合执行近似最近邻搜索，
//     度量方式为 COSINE（余弦相似度），召回 topK 条候选（默认 3）。
//
//  4. 相似度过滤：过滤掉余弦相似度低于 minScore（默认 0.30）的低相关结果，
//     避免将无关内容送入 LLM 上下文。
//
// # 完整事件数据获取（双源融合）
//
// Milvus 返回：事件 ID + 完整文本 content + 余弦相似度 score
// MySQL 补充：title、severity、cve_id、status、source 等结构化字段
// 以 ID 为键做内存 JOIN，返回融合结果。
func NewSearchSimilarEventsTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"search_similar_events",
		"Search for security events semantically similar to the query using vector similarity (cosine distance). "+
			"Returns full event content from Milvus enriched with structured metadata from MySQL. "+
			"Use when investigating related incidents, event correlation, or contextual threat analysis.",
		func(ctx context.Context, input *SearchSimilarEventsInput, opts ...tool.Option) (string, error) {
			if input.Limit <= 0 {
				input.Limit = 5
			}

			g.Log().Infof(ctx, "[Tool] search_similar_events 开始 | query=%q | limit=%d", input.Query, input.Limit)
			rr := retrieval.GetEventsRetriever()

			docs, retErr := rr.Retrieve(ctx, input.Query)
			if retErr != nil {
				return "", fmt.Errorf("vector search failed: %w", retErr)
			}
			if len(docs) == 0 {
				return "[]", nil
			}

			// 在 Retriever topK 范围内按 Limit 截断
			n := input.Limit
			if n > len(docs) {
				n = len(docs)
			}

			// 收集有效事件 ID（防御性过滤：doc.ID 异常为空时跳过）
			ids := make([]string, 0, n)
			for i := 0; i < n; i++ {
				if docs[i].ID != "" {
					ids = append(ids, docs[i].ID)
				}
			}

			// 批量从 MySQL 查询结构化元数据，以 ID 为键构建查找表
			metaMap := fetchEventMetaByIDs(ctx, ids)

			results := make([]SimilarEventResult, 0, n)
			for i := 0; i < n; i++ {
				r := SimilarEventResult{
					ID:      docs[i].ID,
					Content: docs[i].Content, // 完整事件文本，来自 Milvus
					Score:   docs[i].Score(), // 余弦相似度
				}
				// 以 ID 为键合并 MySQL 元数据
				if meta, ok := metaMap[docs[i].ID]; ok {
					r.Title = meta.Title
					r.EventType = meta.EventType
					r.Severity = meta.Severity
					r.RiskScore = meta.RiskScore
					r.CveID = meta.CVEID
					r.Status = meta.Status
					r.Source = meta.Source
					r.CreatedAt = meta.CreatedAt.Format("2006-01-02 15:04:05")
				}
				results = append(results, r)
			}
			b, _ := json.Marshal(results)
			g.Log().Infof(ctx, "[Tool] search_similar_events 完成 | 返回=%d 条", len(results))
			return string(b), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}

// fetchEventMetaByIDs 按事件 ID 批量查询 MySQL 元数据，返回 map[id]*dao.Event。
//
// 设计说明：
//   - 仅读取结构化元数据字段；content 字段在 dao.Event 中标记为 gorm:"-"，不映射 MySQL 列
//   - 查询失败时静默返回空 map，调用方仍可用 Milvus 的 content 和 score 构造部分结果
func fetchEventMetaByIDs(ctx context.Context, ids []string) map[string]*dao.Event {
	result := make(map[string]*dao.Event)
	if len(ids) == 0 {
		return result
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return result
	}
	var events []dao.Event
	if err = db.Where("id IN ?", ids).Find(&events).Error; err != nil {
		return result
	}
	for i := range events {
		result[events[i].ID] = &events[i]
	}
	return result
}
