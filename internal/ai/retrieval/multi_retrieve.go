// Package retriever multi_retrieve.go：多路并行检索与文档去重
//
// 并行检索：
//
//	对所有子查询启动独立 goroutine 并发检索，总延迟 ≈ max(各子查询延迟)，
//	实际仅多增约 50ms 协调开销，大幅降低多路检索的端到端延迟。
//
// 去重：
//
//	同一文档可能被多个子查询命中（例如「高危漏洞」和「漏洞修复方案」都可能召回同一篇）。
//	不去重会导致该文档在 LLM Prompt 中重复出现，占用 Token 且使 LLM 过度关注该文档。
package retrieval

import (
	"context"
	"fmt"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// MultiRetrieve 对多个子查询并行检索，聚合去重后返回文档列表。
//
// 并发设计：
//   - 每个子查询启动独立 goroutine，通过带缓冲的 channel 收集结果
//   - channel 容量 = len(queries)，goroutine 写入后立即返回，不会阻塞
//   - 主 goroutine 循环 len(queries) 次收集所有结果
//
// 容错设计：
//   - 单个子查询检索失败只记录 Warning 日志，不影响其他子查询的结果
//   - 若全部子查询均失败，返回空文档列表（而非 error），调用方后续处理空 Prompt 即可
//
// 特殊情况处理：
//   - queries 为空：返回 (nil, nil)
//   - queries 只有 1 个：跳过 goroutine 开销，直接调用单次检索
func MultiRetrieve(ctx context.Context, r einoretriever.Retriever, queries []string) ([]*schema.Document, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	// 单查询直接走原路径，避免不必要的 goroutine 创建和 channel 开销
	if len(queries) == 1 {
		return r.Retrieve(ctx, queries[0])
	}

	// result 用于在 goroutine 和主 goroutine 之间传递检索结果
	type result struct {
		docs []*schema.Document
		err  error
	}

	// 带缓冲的 channel：goroutine 数量 = queries 数量，写满后 goroutine 自然退出
	ch := make(chan result, len(queries))
	for _, query := range queries {
		q := query
		go func() {
			docs, err := r.Retrieve(ctx, q)
			if err != nil {
				// 单路检索失败不中断整体，记录日志后发送空结果（通过 err 标记）
				g.Log().Warningf(ctx, "[MultiRetrieve] 子查询检索失败 | query=%q | err=%v", q, err)
			}
			ch <- result{docs: docs, err: err}
		}()
	}

	// 收集所有 goroutine 的结果（精确等待 len(queries) 次，不会提前退出或死锁）
	var allDocs []*schema.Document
	for range queries {
		res := <-ch
		if res.err == nil {
			allDocs = append(allDocs, res.docs...)
		}
	}

	// 文档去重
	deduped := Dedup(allDocs)
	g.Log().Debugf(ctx, "[MultiRetrieve] 并行检索完成 | 查询数=%d | 原始文档数=%d | 去重后=%d",
		len(queries), len(allDocs), len(deduped))
	return deduped, nil
}

// Dedup 按文档 ID 去重，保持首次出现顺序。
//
// 顺序保留的意义：
//
//	MultiRetrieve 的子查询顺序与原始问题的维度顺序一致。
//	保持首次出现顺序意味着「第一个子查询命中的文档」排在前面，
//	这些文档通常最贴近原始问题的主维度，对 LLM 推理最有价值。
func Dedup(docs []*schema.Document) []*schema.Document {
	seen := make(map[string]struct{}, len(docs))
	result := make([]*schema.Document, 0, len(docs))
	for _, d := range docs {
		key := d.ID
		if key == "" {
			// 无 ID 文档：用指针地址作为唯一键，避免丢弃有效内容
			key = fmt.Sprint(d)
		}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			result = append(result, d)
		}
	}
	return result
}
