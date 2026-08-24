package parsers

import (
	"SentinelOps/internal/ai/document"
	"fmt"
	"os"
	"strings"

	"github.com/fumiama/go-docx"
)

// ParseDocx 使用 go-docx 提取 .docx 文件的纯文本内容。
func ParseDocx(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat docx: %w", err)
	}

	doc, err := docx.Parse(f, stat.Size())
	if err != nil {
		return "", fmt.Errorf("parse docx: %w", err)
	}

	var sb strings.Builder
	for _, item := range doc.Document.Body.Items {
		p, ok := item.(*docx.Paragraph)
		if !ok || p == nil {
			continue
		}
		for _, child := range p.Children {
			r, ok := child.(*docx.Run)
			if !ok || r == nil {
				continue
			}
			for _, t := range r.Children {
				text, ok := t.(*docx.Text)
				if ok && text != nil {
					sb.WriteString(text.Text)
				}
			}
		}
		sb.WriteString("\n")
	}
	return cleanupText(sb.String()), nil
}

// ParseDocxWithStructure 解析 DOCX 文档，识别 Heading 样式提取章节结构。
// defaultTitle 用于无标题文档的默认标题（通常为原始文件名）。
func ParseDocxWithStructure(filePath, defaultTitle string) (*document.ParsedDocument, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat docx: %w", err)
	}

	doc, err := docx.Parse(f, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("parse docx: %w", err)
	}

	result := &document.ParsedDocument{
		Title: defaultTitle,
	}
	var currentSection *document.ParsedSection
	var contentBuilder strings.Builder
	titlePath := make([]string, 6)

	flushSection := func() {
		if currentSection != nil {
			currentSection.Content = strings.TrimSpace(contentBuilder.String())
			if currentSection.Content != "" || currentSection.Title != "" {
				result.Sections = append(result.Sections, *currentSection)
			}
			contentBuilder.Reset()
		}
	}

	for _, item := range doc.Document.Body.Items {
		p, ok := item.(*docx.Paragraph)
		if !ok || p == nil {
			continue
		}

		text := extractParagraphText(p)
		if text == "" {
			continue
		}

		// 检测 Heading 样式
		level := getHeadingLevel(p)
		if level > 0 {
			flushSection()
			if result.Title == "" && level == 1 {
				result.Title = text
			}
			titlePath[level-1] = text
			for i := level; i < 6; i++ {
				titlePath[i] = ""
			}
			var pathParts []string
			for i := 0; i < level; i++ {
				if titlePath[i] != "" {
					pathParts = append(pathParts, titlePath[i])
				}
			}
			currentSection = &document.ParsedSection{
				Level: level,
				Title: strings.Join(pathParts, " > "),
			}
			contentBuilder.WriteString(text)
			contentBuilder.WriteString("\n")
		} else {
			if currentSection == nil {
				currentSection = &document.ParsedSection{Level: 0, Title: ""}
			}
			contentBuilder.WriteString(text)
			contentBuilder.WriteString("\n")
		}
	}
	flushSection()

	if len(result.Sections) == 0 {
		text, _ := ParseDocx(filePath)
		result.Sections = []document.ParsedSection{{Level: 0, Title: "", Content: text}}
	}
	return result, nil
}

// extractParagraphText 从段落中提取所有 Run 的纯文本内容。
func extractParagraphText(p *docx.Paragraph) string {
	var sb strings.Builder
	for _, child := range p.Children {
		r, ok := child.(*docx.Run)
		if !ok || r == nil {
			continue
		}
		for _, t := range r.Children {
			text, ok := t.(*docx.Text)
			if ok && text != nil {
				sb.WriteString(text.Text)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// getHeadingLevel 从段落样式中识别 Heading 1-6 级别，返回 0 表示非标题段落。
func getHeadingLevel(p *docx.Paragraph) int {
	if p.Properties == nil || p.Properties.Style == nil {
		return 0
	}
	style := strings.ToLower(p.Properties.Style.Val)
	if strings.HasPrefix(style, "heading") && len(style) > 7 {
		switch style[7] {
		case '1':
			return 1
		case '2':
			return 2
		case '3':
			return 3
		case '4':
			return 4
		case '5':
			return 5
		case '6':
			return 6
		}
	}
	return 0
}
