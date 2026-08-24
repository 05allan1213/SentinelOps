package chunkers

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// codePatterns 各语言的顶层声明边界正则，用于识别函数/类/接口定义。
var codePatterns = map[string][]*regexp.Regexp{
	"go": {
		regexp.MustCompile(`(?m)^func\s+`),
		regexp.MustCompile(`(?m)^type\s+\w+\s+struct`),
		regexp.MustCompile(`(?m)^type\s+\w+\s+interface`),
	},
	"python": {
		regexp.MustCompile(`(?m)^def\s+\w+`),
		regexp.MustCompile(`(?m)^class\s+\w+`),
		regexp.MustCompile(`(?m)^async\s+def\s+\w+`),
	},
	"java": {
		regexp.MustCompile(`(?m)^\s*(public|private|protected)?\s*(static)?\s+\w+\s+\w+\s*\(`),
		regexp.MustCompile(`(?m)^\s*(public|private|protected)?\s+class\s+\w+`),
		regexp.MustCompile(`(?m)^\s*(public|private|protected)?\s+interface\s+\w+`),
	},
	"javascript": {
		regexp.MustCompile(`(?m)^function\s+\w+`),
		regexp.MustCompile(`(?m)^class\s+\w+`),
		regexp.MustCompile(`(?m)^const\s+\w+\s*=\s*\(`),
		regexp.MustCompile(`(?m)^export\s+(function|class|const)`),
	},
	"typescript": {
		regexp.MustCompile(`(?m)^function\s+\w+`),
		regexp.MustCompile(`(?m)^class\s+\w+`),
		regexp.MustCompile(`(?m)^const\s+\w+\s*=\s*\(`),
		regexp.MustCompile(`(?m)^export\s+(function|class|const|interface)`),
		regexp.MustCompile(`(?m)^interface\s+\w+`),
	},
}

// Code 按函数/类/接口边界切分代码文件，超过 maxSize 时强制截断。
// language 支持：go / python / java / javascript / typescript。
// 未识别语言降级为 SlidingWindow 分块。
func Code(text, language string, maxSize int) []ChunkResult {
	// maxSize 默认值必须在首次使用前设置（含未识别语言的降级路径）
	if maxSize <= 0 {
		maxSize = 1024
	}

	patterns, ok := codePatterns[strings.ToLower(language)]
	if !ok {
		// 未知语言：降级为滑动窗口分块
		raw := SlidingWindow(text, maxSize, 0)
		results := make([]ChunkResult, len(raw))
		for i, chunk := range raw {
			results[i] = ChunkResult{
				Content:    chunk,
				ChunkIndex: i,
				CharCount:  utf8.RuneCountInString(chunk),
			}
		}
		return results
	}

	lines := strings.Split(text, "\n")
	var results []ChunkResult
	var cur strings.Builder
	var currentTitle string // 当前块的函数/类名
	chunkIndex := 0

	// extractTitle 从声明行提取函数/类名作为章节标题
	extractTitle := func(line string) string {
		// 移除前导空格和修饰符，提取核心标识符
		line = strings.TrimSpace(line)
		// Go: func FuncName / type StructName struct
		if strings.HasPrefix(line, "func ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[1]
				// 移除接收者 (t *Type)
				if idx := strings.Index(name, "("); idx > 0 {
					name = name[:idx]
				}
				// 移除参数列表
				if idx := strings.Index(name, "("); idx > 0 {
					name = name[:idx]
				}
				return name
			}
		}
		if strings.HasPrefix(line, "type ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
		// Python: def func_name / class ClassName
		if strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "async def ") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "def" && i+1 < len(parts) {
					name := parts[i+1]
					if idx := strings.Index(name, "("); idx > 0 {
						return name[:idx]
					}
					return name
				}
			}
		}
		if strings.HasPrefix(line, "class ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[1]
				if idx := strings.Index(name, "("); idx > 0 {
					return name[:idx]
				}
				if idx := strings.Index(name, ":"); idx > 0 {
					return name[:idx]
				}
				return name
			}
		}
		// Java: public/private/protected ... methodName(
		if strings.Contains(line, "(") {
			// 简化提取：找到 ( 前的最后一个单词
			idx := strings.Index(line, "(")
			before := strings.TrimSpace(line[:idx])
			parts := strings.Fields(before)
			if len(parts) > 0 {
				return parts[len(parts)-1]
			}
		}
		return ""
	}

	// flush 提交当前缓冲区为一个 ChunkResult，非空时才追加
	flush := func() {
		content := strings.TrimSpace(cur.String())
		if content != "" {
			results = append(results, ChunkResult{
				Content:      content,
				SectionTitle: currentTitle,
				ChunkIndex:   chunkIndex,
				CharCount:    utf8.RuneCountInString(content),
			})
			chunkIndex++
		}
		cur.Reset()
		currentTitle = ""
	}

	for i, line := range lines {
		// 检测是否命中边界模式（函数/类/接口声明行）
		isBoundary := false
		for _, pattern := range patterns {
			if pattern.MatchString(line) {
				isBoundary = true
				break
			}
		}

		// 遇到边界且缓冲区非空时，先提交前一段代码
		if isBoundary && cur.Len() > 0 {
			flush()
		}

		// 遇到边界时提取函数/类名作为新块的标题
		if isBoundary {
			currentTitle = extractTitle(line)
		}

		cur.WriteString(line)
		if i < len(lines)-1 {
			cur.WriteString("\n")
		}

		// 单块超过 maxSize 时强制截断，防止 LLM 上下文溢出
		if utf8.RuneCountInString(cur.String()) > maxSize {
			flush()
		}
	}

	// 提交最后一段
	if cur.Len() > 0 {
		flush()
	}

	return results
}
