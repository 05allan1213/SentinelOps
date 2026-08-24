package parsers

import (
	"regexp"
	"strings"
)

var (
	reTrailingSpace = regexp.MustCompile(`[ \t]+\n`)
	reExcessNewline = regexp.MustCompile(`\n{3,}`)
)

// cleanupText 清理文本：
//  1. 移除 UTF-8 BOM
//  2. 清除行尾空格/制表符
//  3. 压缩连续 3+ 空行为双空行
//  4. 去除首尾空白
func cleanupText(text string) string {
	text = strings.TrimPrefix(text, "\uFEFF")
	text = reTrailingSpace.ReplaceAllString(text, "\n")
	text = reExcessNewline.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}
