package document

import (
	"SentinelOps/internal/ai/document/chunkers"
)

// ChunkStrategy 文档分块策略。
type ChunkStrategy string

const (
	// StrategySlidingWindow 滑动窗口分块（固定大小 + 重叠），适合大多数场景。
	StrategySlidingWindow ChunkStrategy = "sliding_window"
	// StrategyHierarchical 父子分块 + 结构化解析：先用 ParseFileWithStructure 提取文档章节，
	// 再对每章节做固定大小父子分块（父块~1024 rune，子块~256 rune）。
	// 子块存 Milvus 向量（精准检索），metadata 携带父块完整文本（LLM 上下文完整）+ 章节标题。
	// 在索引管道中走 ChunkDocument 路径。
	StrategyHierarchical ChunkStrategy = "hierarchical"
	// StrategyCode 代码语法感知分块，按函数/类/接口边界切分，适合代码文件。
	StrategyCode ChunkStrategy = "code"
)

// ChunkConfig 分块配置，通过 IndexInput 在运行时传入，无需重建 Graph。
type ChunkConfig struct {
	Strategy        ChunkStrategy // 分块策略，默认 sliding_window
	ChunkSize       int           // sliding_window 用：目标块大小（rune），默认 512
	OverlapSize     int           // sliding_window 用：重叠大小（rune），默认 128
	ParentChunkSize int           // hierarchical 用：父块大小（rune），默认 1024
	ChildChunkSize  int           // hierarchical 用：子块大小（rune），默认 256
	ChildOverlap    int           // hierarchical 用：子块重叠（rune），默认 40
	Language        string        // code 用：代码语言（go/python/java/javascript/typescript）
}

// DefaultChunkConfig 返回推荐的默认分块配置（sliding_window 512/128）。
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{
		Strategy:        StrategySlidingWindow,
		ChunkSize:       512,
		OverlapSize:     128,
		ParentChunkSize: 1024,
		ChildChunkSize:  256,
		ChildOverlap:    40,
	}
}

// DefaultHierarchicalConfig 返回推荐的父子分块配置。
func DefaultHierarchicalConfig() ChunkConfig {
	return ChunkConfig{
		Strategy:        StrategyHierarchical,
		ParentChunkSize: 1024,
		ChildChunkSize:  256,
		ChildOverlap:    40,
	}
}

// StrategyForExt 根据文件扩展名返回推荐的分块策略。
// 代码文件（.go/.py/.java）返回 StrategyCode（语法感知分块）；
// 文档文件（.pdf/.md/.docx）返回 StrategyHierarchical（结构化父子分块）。
func StrategyForExt(ext string) ChunkStrategy {
	switch ext {
	case ".go", ".py", ".java":
		return StrategyCode
	default:
		return StrategyHierarchical
	}
}

// ConfigForExt 根据文件扩展名返回推荐的完整分块配置（含所有默认参数）。
// 代码文件自动设置 Language 字段，文档文件使用 hierarchical 默认配置。
func ConfigForExt(ext string) ChunkConfig {
	cfg := DefaultChunkConfig()
	cfg.Strategy = StrategyForExt(ext)

	// 为代码文件设置语言参数
	if cfg.Strategy == StrategyCode {
		switch ext {
		case ".go":
			cfg.Language = "go"
		case ".py":
			cfg.Language = "python"
		case ".java":
			cfg.Language = "java"
		}
	}

	return cfg
}

// ParsedSection 文档中的一个章节，包含标题层级和正文内容。
type ParsedSection struct {
	Level   int    // 标题层级：1=H1, 2=H2, 3=H3, 0=无标题
	Title   string // 章节标题（不含 # 前缀）
	Content string // 章节正文
}

// ParsedDocument 结构化解析结果，包含文档主标题和按章节划分的内容。
type ParsedDocument struct {
	Title    string          // 文档主标题（首个 H1 标题，或文件名）
	Sections []ParsedSection // 按标题层级划分的节
}

// ChunkResult 是 chunkers.ChunkResult 的类型别名（消除重复定义）。
// 子块存入 Milvus 做向量检索，命中时通过 ParentContent 返回更完整的上下文给 LLM。
type ChunkResult = chunkers.ChunkResult

// TruncateToMaxBytes 在 UTF-8 字符边界处截断字符串，确保不超过 maxBytes 字节。
// 用于 Milvus varchar 字段的安全边界保护。
func TruncateToMaxBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for i := maxBytes; i > 0; i-- {
		if (s[i] & 0xC0) != 0x80 {
			return s[:i]
		}
	}
	return ""
}
