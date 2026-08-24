package chunkers

import (
	"unicode/utf8"
)

// Hierarchical 父子分块：先切父块（固定大小、无重叠），再将每个父块细切为子块（固定大小、可重叠）。
//
// 向量化策略：
//   - 向量化：对子块文本进行嵌入，语义更聚焦，向量质量更高
//   - 检索：命中子块向量 → 读取 ChunkResult.ParentContent → 注入完整父块上下文给 LLM
//
// 参数说明：
//   - sectionTitle: 章节标题，由 HierarchicalChunkFromDocument 注入；直接调用时可传空串
//   - parentSize:   父块大小（rune），默认 1024
//   - childSize:    子块大小（rune），默认 256
//   - childOverlap: 子块间重叠（rune），默认 0
//
// ChunkIndex 为本次调用内的连续编号（从 0 起），跨章节全局编号由 ChunkDocument 重写。
func Hierarchical(text, sectionTitle string, parentSize, childSize, childOverlap int) []ChunkResult {
	if parentSize <= 0 {
		parentSize = 1024
	}
	if childSize <= 0 {
		childSize = 256
	}
	if childOverlap < 0 {
		childOverlap = 0
	}

	// 1. 切父块（无重叠，保证每个子块只属于唯一父块）
	parents := SlidingWindow(text, parentSize, 0)
	// 2. 预分配容量，减少 GC（平均每父块 parentSize/childSize 个子块）
	estimatedChildren := (parentSize / childSize) + 1
	results := make([]ChunkResult, 0, len(parents)*estimatedChildren)

	globalIdx := 0
	for _, parent := range parents {
		children := SlidingWindow(parent, childSize, childOverlap)
		if len(children) == 0 {
			// 父块极短（≤ childSize）时直接作为唯一子块，避免丢失内容
			children = []string{parent}
		}
		for _, child := range children {
			results = append(results, ChunkResult{
				Content:       child,
				ParentContent: parent,
				SectionTitle:  sectionTitle,
				ChunkIndex:    globalIdx,
				CharCount:     utf8.RuneCountInString(child),
			})
			globalIdx++
		}
	}
	return results
}
