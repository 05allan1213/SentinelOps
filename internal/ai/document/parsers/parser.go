package parsers

import (
	"SentinelOps/internal/ai/document"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ParseFileToPlainText 根据文件扩展名选择解析器，返回清理后的纯文本。
// 支持：.pdf、.docx、.md、.markdown、代码文件（.go/.py/.java）
//
// 注意：.md 文件返回保留 # 标记的原始 Markdown 文本，供 StrategyHierarchical 的 extractMarkdownSections 使用。
func ParseFileToPlainText(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".pdf":
		return ParsePDF(filePath)
	case ".docx":
		return ParseDocx(filePath)
	case ".md", ".markdown":
		return ParseMarkdown(filePath)
	case ".go", ".py", ".java":
		return ParseCode(filePath)
	default:
		return "", fmt.Errorf("unsupported file format: %s", ext)
	}
}

// ParseFileWithStructure 解析文件，返回含标题层级的 ParsedDocument。
// 支持：.pdf、.docx、.md、.markdown
//
// 参数 defaultTitle：当文档无标题时使用的默认标题（通常为原始文件名）。
//
// 专为 StrategyHierarchical 设计：章节信息作为 SectionTitle metadata 写入 Milvus，
// 检索命中后 LLM 可感知内容所属的文档章节，提升回答准确性。
func ParseFileWithStructure(filePath, defaultTitle string) (*document.ParsedDocument, error) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".md", ".markdown":
		text, err := ParseMarkdown(filePath)
		if err != nil {
			return nil, err
		}
		return extractMarkdownSections(text, defaultTitle), nil
	case ".docx":
		return ParseDocxWithStructure(filePath, defaultTitle)
	case ".pdf":
		return parseFlatToStructured(filePath, defaultTitle, ParsePDF)
	default:
		return nil, fmt.Errorf("unsupported file format: %s", filepath.Ext(filePath))
	}
}

// parseFlatToStructured 用指定解析函数提取纯文本，再尝试按 Markdown 标题切分章节。
// 供 DOCX 和 PDF 复用，消除两者的重复逻辑。
func parseFlatToStructured(filePath, defaultTitle string, parseFn func(string) (string, error)) (*document.ParsedDocument, error) {
	text, err := parseFn(filePath)
	if err != nil {
		return nil, err
	}
	return extractMarkdownSections(text, defaultTitle), nil
}

// reMarkdownHeading 匹配 Markdown 标题行（# 到 ######）。
var reMarkdownHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

// extractMarkdownSections 从文本中提取 Markdown 标题层级，切分为 ParsedDocument。
//
// 规则：
//   - 标题行本身写入所在节的 Content 开头，确保分块后标题文本可被向量检索命中
//   - SectionTitle 按层级路径拼接（"H1 > H2 > H3"），为 LLM 提供章节层次上下文
//   - 文档无任何标题时退化为单节（Level=0, Title=""），确保有内容时不丢数据
func extractMarkdownSections(text, defaultTitle string) *document.ParsedDocument {
	// 统一换行符，消除 Windows \r\n 导致标题正则捕获到 \r 的问题
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	doc := &document.ParsedDocument{}
	var currentSection *document.ParsedSection
	var contentBuilder strings.Builder

	// titlePath 维护各层级标题路径（index 0=H1, ..., 5=H6）
	titlePath := make([]string, 6)

	flushSection := func() {
		if currentSection != nil {
			currentSection.Content = strings.TrimSpace(contentBuilder.String())
			doc.Sections = append(doc.Sections, *currentSection)
			contentBuilder.Reset()
		}
	}

	for _, line := range lines {
		if m := reMarkdownHeading.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			title := strings.TrimSpace(m[2])
			flushSection()

			if doc.Title == "" && level == 1 {
				doc.Title = title
			}

			// 更新层级路径：设置当前层级，清空所有子层级
			titlePath[level-1] = title
			for i := level; i < 6; i++ {
				titlePath[i] = ""
			}

			// 构建 "H1 > H2 > H3" 层级路径
			var pathParts []string
			for i := 0; i < level; i++ {
				if titlePath[i] != "" {
					pathParts = append(pathParts, titlePath[i])
				}
			}

			currentSection = &document.ParsedSection{Level: level, Title: strings.Join(pathParts, " > ")}
			// 标题行写入 content，确保标题文本可被向量检索命中
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		} else {
			if currentSection == nil {
				currentSection = &document.ParsedSection{Level: 0, Title: ""}
			}
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		}
	}
	flushSection()

	if doc.Title == "" {
		doc.Title = defaultTitle
	}
	// 过滤内容和标题均为空的节
	var sections []document.ParsedSection
	for _, s := range doc.Sections {
		if s.Content != "" || s.Title != "" {
			sections = append(sections, s)
		}
	}
	if len(sections) == 0 {
		// 无任何标题时整体作为单节，避免内容丢失
		sections = []document.ParsedSection{{Level: 0, Title: "", Content: text}}
	}
	doc.Sections = sections
	return doc
}
