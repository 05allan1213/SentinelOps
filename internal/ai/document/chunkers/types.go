package chunkers

// ChunkResult 父子分块的单个子块结果。
//
// 向量化策略：
//   - 向量化时使用 Content（子块更短，语义聚焦，向量质量更高）
//   - LLM 回答时使用 ParentContent（完整父块，保证上下文完整性）
type ChunkResult struct {
	ID            string // 子块ID（外层赋值，对应 Milvus 文档 ID）
	Content       string // 子块文本（用于向量化，长度 ~ChildChunkSize）
	ParentContent string // 父块文本（检索命中后返回给 LLM，长度 ~ParentChunkSize）
	SectionTitle  string // 所在章节标题（metadata 注入，辅助 LLM 理解上下文）
	ChunkIndex    int    // 子块在文档中的全局序号（从 0 开始）
	CharCount     int    // 子块字符数（rune 数，不含父块）
}
