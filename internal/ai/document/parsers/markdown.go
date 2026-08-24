package parsers

import (
	"fmt"
	"os"
)

// ParseMarkdown 读取 Markdown 文件，返回清理后的原始文本（保留 # 等 Markdown 语法标记）。
//
// 保留原始 Markdown 语法的关键作用：
//
//	extractMarkdownSections 依赖 # 标题行识别章节边界，用于 StrategyHierarchical 结构化解析
func ParseMarkdown(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return cleanupText(string(data)), nil
}
