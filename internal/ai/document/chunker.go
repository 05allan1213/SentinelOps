package document

import (
	"strings"
	"unicode/utf8"

	"SentinelOps/internal/ai/document/chunkers"
)

// ChunkText 将纯文本按指定策略切分为字符串列表。
// 供非结构化路径（StrategySlidingWindow / StrategyCode）使用；
// 结构化路径（StrategyHierarchical）请使用 ChunkDocument。
func ChunkText(text string, cfg ChunkConfig) []string {
	switch cfg.Strategy {
	case StrategyCode:
		return toContentSlice(chunkers.Code(text, cfg.Language, cfg.ChunkSize))
	default: // StrategySlidingWindow 及未知策略均走滑动窗口分块
		return chunkers.SlidingWindow(text, cfg.ChunkSize, cfg.OverlapSize)
	}
}

// ChunkDocument 对结构化文档按 StrategyHierarchical 做父子分块。
// 每个章节独立分块，SectionTitle 携带层级路径（"H1 > H2"），ChunkIndex 全文档连续编号。
// 空内容章节自动跳过，避免产生无意义分块。
func ChunkDocument(doc *ParsedDocument, cfg ChunkConfig) []ChunkResult {
	pSize := cfg.ParentChunkSize
	if pSize <= 0 {
		pSize = 1024
	}
	cSize := cfg.ChildChunkSize
	if cSize <= 0 {
		cSize = 256
	}
	overlap := cfg.ChildOverlap
	if overlap < 0 {
		overlap = 0
	}

	var results []ChunkResult
	globalIdx := 0
	for _, section := range doc.Sections {
		if strings.TrimSpace(section.Content) == "" {
			continue
		}
		// 章节无标题时退回到文档主标题，确保 SectionTitle 不为空
		title := section.Title
		if title == "" {
			title = doc.Title
		}
		chunks := chunkers.Hierarchical(section.Content, title, pSize, cSize, overlap)
		for i := range chunks {
			chunks[i].ChunkIndex = globalIdx
			chunks[i].CharCount = utf8.RuneCountInString(chunks[i].Content)
			globalIdx++
		}
		results = append(results, chunks...)
	}
	return results
}

// toContentSlice 从 ChunkResult 列表中提取 Content 字段，用于 ChunkText 的非父子路径。
func toContentSlice(results []ChunkResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Content
	}
	return out
}
