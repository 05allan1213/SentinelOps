// Package retrieval 提供统一的混合检索能力
//
// 背景：传统纯稠密向量检索在精确词匹配（CVE编号、产品名）场景召回率不足
// 原理：结合 BM25 稀疏向量（精确词匹配）+ 语义稠密向量（语义相似），通过 RRF 融合排序
// 设计：分层架构 - 缓存层/嵌入层/检索层/编排层，支持分区隔离和配置化开关
package retrieval

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// Config 检索系统统一配置
type Config struct {
	// 缓存配置：Redis 语义缓存，相似查询跳过 Embedding + Milvus
	CacheTTL       time.Duration // 缓存有效期
	CacheThreshold float64       // 余弦相似度命中阈值 [0,1]
	CacheKeyPrefix string        // Redis key 前缀

	// 检索配置：控制召回数量和质量阈值
	TopK      int     // Milvus 初始召回数（候选池）
	FinalTopK int     // 返回给 LLM 的最终文档数
	MinScore  float64 // 最低相似度阈值（仅用于纯稠密检索，混合检索内部直接返回 RRF 融合后的结果）

	// 混合检索配置：Sparse-Dense 双路检索 + RRF 融合
	HybridEnabled bool // 是否启用混合检索（关闭则纯稠密检索）
	RRFK          int  // RRF 融合参数 k，控制排序平滑度

	// 分区配置：Milvus 分区隔离（events/documents）
	Partition string
}

// LoadConfig 从配置文件加载检索系统配置，未配置项使用合理默认值
// 配置读取失败时使用默认值，保证系统可用性
func LoadConfig(ctx context.Context) Config {
	cfg := Config{
		CacheTTL:       24 * time.Hour, // 24h 缓存，平衡命中率与时效性
		CacheThreshold: 0.85,           // 高阈值避免误命中
		CacheKeyPrefix: "rag:cache",
		TopK:           5,    // 候选池 5 条，平衡召回率与性能
		FinalTopK:      3,    // 送入 LLM 3 条，控制 Token 消耗
		MinScore:       0.30, // 过滤低相关文档
		HybridEnabled:  true, // 默认启用混合检索
		RRFK:           60,   // RRF 标准参数，平衡排序平滑度
	}

	// 安全读取配置，失败时保留默认值
	if v, err := g.Cfg().Get(ctx, "redis.semantic_cache.ttl"); err == nil && v.Int64() > 0 {
		cfg.CacheTTL = time.Duration(v.Int64()) * time.Hour
	}
	if v, err := g.Cfg().Get(ctx, "redis.semantic_cache.threshold"); err == nil && v.Float64() > 0 {
		cfg.CacheThreshold = v.Float64()
	}
	if v, err := g.Cfg().Get(ctx, "redis.semantic_cache.key_prefix"); err == nil && v.String() != "" {
		cfg.CacheKeyPrefix = v.String()
	}
	if v, err := g.Cfg().Get(ctx, "redis.semantic_cache.topk"); err == nil && v.Int() > 0 {
		cfg.TopK = v.Int()
	}
	if v, err := g.Cfg().Get(ctx, "redis.semantic_cache.final_topk"); err == nil && v.Int() > 0 {
		cfg.FinalTopK = v.Int()
	}
	if v, err := g.Cfg().Get(ctx, "redis.semantic_cache.min_score"); err == nil && v.Float64() > 0 {
		cfg.MinScore = v.Float64()
	}
	if v, err := g.Cfg().Get(ctx, "retriever.hybrid.enabled"); err == nil {
		cfg.HybridEnabled = v.Bool()
	}
	if v, err := g.Cfg().Get(ctx, "retriever.hybrid.rrf_k"); err == nil && v.Int() > 0 {
		cfg.RRFK = v.Int()
	}

	return cfg
}
