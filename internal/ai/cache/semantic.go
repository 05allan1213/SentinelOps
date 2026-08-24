package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	aitrace "SentinelOps/internal/ai/trace"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// Entry 缓存条目，包含查询向量和检索结果
// JSON 序列化后存入 Redis，支持持久化和跨进程共享
type Entry struct {
	Embedding []float64          `json:"embedding"` // 查询向量（2048维）
	Docs      []*schema.Document `json:"docs"`      // 检索结果文档列表
}

// SemanticCache 基于 Redis 的语义相似缓存
//
// 工作原理：
//  1. 查询时计算新向量与缓存向量的余弦相似度
//  2. 相似度 >= threshold 时命中缓存，直接返回结果
//  3. 未命中时执行实际检索，并将结果写入缓存
//
// 数据结构：
//   - Redis Set: {keyPrefix}:idx 存储所有条目 ID
//   - Redis String: {keyPrefix}:{id} 存储序列化的 Entry
//
// 优势：
//   - 跳过 Embedding API 调用（节省成本和延迟）
//   - 跳过 Milvus 检索（节省 ~50ms）
//   - 支持语义相似查询的缓存复用
type SemanticCache struct {
	client    *goredis.Client // Redis 客户端
	keyPrefix string          // 缓存键前缀（如 "rag:cache"）
	ttl       time.Duration   // 缓存有效期（默认 24 小时）
	threshold float64         // 余弦相似度命中阈值（默认 0.85）
}

// New 创建语义缓存实例
//
// 参数：
//   - client: Redis 客户端
//   - keyPrefix: 缓存键前缀，用于命名空间隔离
//   - ttlHours: 缓存有效期（小时）
//   - threshold: 余弦相似度命中阈值 [0,1]，推荐 0.85
func New(client *goredis.Client, keyPrefix string, ttlHours int, threshold float64) *SemanticCache {
	return &SemanticCache{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       time.Duration(ttlHours) * time.Hour,
		threshold: threshold,
	}
}

// indexKey 返回索引集合的 Redis 键
// 格式：{keyPrefix}:idx
func (sc *SemanticCache) indexKey() string {
	return sc.keyPrefix + ":idx"
}

// entryKey 返回指定条目的 Redis 键
// 格式：{keyPrefix}:{id}
func (sc *SemanticCache) entryKey(id string) string {
	return sc.keyPrefix + ":" + id
}

// Get 查询缓存，返回最相似的检索结果
//
// 返回值：
//   - docs: 缓存的检索结果文档列表
//   - sim: 最高相似度分数
//   - hit: 是否命中缓存（相似度 >= threshold）
//
// 追踪：记录 CACHE 节点，标记 RAG_HIT 或 RAG_MISS
func (sc *SemanticCache) Get(ctx context.Context, vec []float64) (docs []*schema.Document, sim float64, hit bool) {
	spanCtx, spanID := aitrace.StartSpan(ctx, aitrace.NodeTypeCache, "RAG_GET")
	docs, sim, hit = sc.doGet(spanCtx, vec)
	label := "RAG_MISS"
	if hit {
		label = "RAG_HIT"
	}
	aitrace.FinishSpan(spanCtx, spanID, nil, map[string]any{
		"op":  label,
		"hit": hit,
		"sim": fmt.Sprintf("%.4f", sim),
	})
	return docs, sim, hit
}

// doGet 执行实际的缓存查询逻辑
//
// 流程：
//  1. 从索引集合获取所有条目 ID
//  2. 遍历每个条目，计算余弦相似度
//  3. 相似度 >= threshold 时立即返回（命中）
//  4. 未命中时返回最高相似度（用于调试）
//
// 容错：
//   - 条目不存在时自动从索引中移除（清理过期数据）
//   - 反序列化失败时记录警告并跳过
func (sc *SemanticCache) doGet(ctx context.Context, vec []float64) ([]*schema.Document, float64, bool) {
	ids, err := sc.client.SMembers(ctx, sc.indexKey()).Result()
	if err != nil || len(ids) == 0 {
		return nil, 0, false
	}

	var bestSim float64
	for _, id := range ids {
		data, err := sc.client.Get(ctx, sc.entryKey(id)).Bytes()
		if err != nil {
			if errors.Is(err, goredis.Nil) {
				// 条目已过期，从索引中移除
				sc.client.SRem(ctx, sc.indexKey(), id)
			}
			continue
		}

		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			g.Log().Warningf(ctx, "[SemanticCache] 反序列化失败 id=%s: %v", id, err)
			continue
		}

		s := CosineSim(vec, entry.Embedding)
		if s > bestSim {
			bestSim = s
		}
		if s >= sc.threshold {
			return entry.Docs, s, true
		}
	}
	g.Log().Debugf(ctx, "[SemanticCache] 未达阈值 | 最高=%.4f | 阈值=%.2f | 条目数=%d", bestSim, sc.threshold, len(ids))
	return nil, 0, false
}

// Set 将检索结果写入缓存
//
// 参数：
//   - vec: 查询向量
//   - docs: 检索结果文档列表
//
// 优化：
//   - 写入前检查是否已存在相似条目，避免重复写入
//   - 使用 Pipeline 批量执行 Redis 命令，减少网络往返
//
// 追踪：记录 CACHE 节点，包含文档数量
func (sc *SemanticCache) Set(ctx context.Context, vec []float64, docs []*schema.Document) {
	spanCtx, spanID := aitrace.StartSpan(ctx, aitrace.NodeTypeCache, "RAG_SET")
	sc.doSet(spanCtx, vec, docs)
	aitrace.FinishSpan(spanCtx, spanID, nil, map[string]any{
		"op":        "SET",
		"doc_count": len(docs),
	})
}

// doSet 执行实际的缓存写入逻辑
//
// 流程：
//  1. 检查是否已存在相似条目（避免重复）
//  2. 序列化 Entry 为 JSON
//  3. 使用 Pipeline 批量写入：
//     - 写入条目数据
//     - 设置条目 TTL
//     - 将条目 ID 加入索引集合
//     - 设置索引集合 TTL
//
// 容错：序列化失败时静默跳过，不影响主流程
func (sc *SemanticCache) doSet(ctx context.Context, vec []float64, docs []*schema.Document) {
	if sc.hasSimilar(ctx, vec) {
		g.Log().Debugf(ctx, "[SemanticCache] 近似条目已存在，跳过")
		return
	}

	entry := Entry{Embedding: vec, Docs: docs}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	id := uuid.New().String()
	pipe := sc.client.Pipeline()
	pipe.Set(ctx, sc.entryKey(id), data, 0)
	pipe.Expire(ctx, sc.entryKey(id), sc.ttl)
	pipe.SAdd(ctx, sc.indexKey(), id)
	pipe.Expire(ctx, sc.indexKey(), sc.ttl)
	pipe.Exec(ctx)
	g.Log().Debugf(ctx, "[SemanticCache] 写入 id=%s", id)
}

// hasSimilar 检查是否已存在相似条目
//
// 用途：避免重复写入相似的缓存条目，节省 Redis 存储空间
// 判断标准：相似度 >= threshold
func (sc *SemanticCache) hasSimilar(ctx context.Context, vec []float64) bool {
	ids, err := sc.client.SMembers(ctx, sc.indexKey()).Result()
	if err != nil || len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		data, err := sc.client.Get(ctx, sc.entryKey(id)).Bytes()
		if err != nil {
			if errors.Is(err, goredis.Nil) {
				// 条目已过期，从索引中移除
				sc.client.SRem(ctx, sc.indexKey(), id)
			}
			continue
		}
		var entry Entry
		if json.Unmarshal(data, &entry) != nil {
			continue
		}
		if CosineSim(vec, entry.Embedding) >= sc.threshold {
			return true
		}
	}
	return false
}

// CosineSim 计算两个向量的余弦相似度
//
// 公式：cos(θ) = (a · b) / (||a|| * ||b||)
//
// 返回值：[0, 1] 范围内的相似度分数
//   - 1.0: 完全相同
//   - 0.85+: 高度相似（推荐作为缓存命中阈值）
//   - 0.0: 完全不相关
//
// 容错：分母为 0 时返回 0（避免除零错误）
func CosineSim(a, b []float64) float64 {
	var dot, sumSqA, sumSqB float64
	for i := range a {
		dot += a[i] * b[i]
		sumSqA += a[i] * a[i]
		sumSqB += b[i] * b[i]
	}
	denom := math.Sqrt(sumSqA) * math.Sqrt(sumSqB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
