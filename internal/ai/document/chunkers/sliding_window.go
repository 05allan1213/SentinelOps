package chunkers

import (
	"regexp"
	"strings"
)

var reURLBreak = regexp.MustCompile(`([a-zA-Z0-9])\.\n([a-zA-Z])`)

// SlidingWindow 滑动窗口分块，在优先边界（\n > 中文句末 > 英文句末）处对齐。
//
// 算法：
//  1. normalizeText：修复 PDF/DOCX 解析产物中 URL 换行断开
//  2. 计算 [start, end) 窗口，end = start + chunkSize
//  3. 当 end 未到文本末尾时，adjustToBoundary 在 overlap 窗口内向前寻找最优切分点
//  4. 下一个 start = end - overlapSize（相邻块保留 overlapSize 个 rune 的重叠上下文）
//
// 边界条件：
//   - chunkSize <= 0 时使用默认值 512
//   - overlapSize < 0 时修正为 0
//   - nextStart <= start 时强制 +1，防止无限循环（极端配置下的兜底）
func SlidingWindow(text string, chunkSize, overlapSize int) []string {
	text = normalizeText(text)
	runes := []rune(text)
	total := len(runes)
	if total == 0 {
		return nil
	}

	if chunkSize <= 0 {
		chunkSize = 512
	}
	if overlapSize < 0 {
		overlapSize = 0
	}

	var chunks []string
	start := 0
	for start < total {
		end := start + chunkSize
		if end > total {
			end = total
		} else {
			end = adjustToBoundary(runes, end, overlapSize)
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= total {
			break
		}
		nextStart := end - overlapSize
		if nextStart <= start {
			nextStart = start + 1 // 防死循环（极端配置兜底）
		}
		start = nextStart
	}
	return chunks
}

// normalizeText 修复 PDF/DOCX 解析时 URL 因换行被错误断开的情况。
// 处理形如 "github.com\ngoogles" → "github.com googles" 的断行场景。
func normalizeText(text string) string {
	return reURLBreak.ReplaceAllString(text, "$1.$2")
}

// adjustToBoundary 在 [targetEnd-lookback, targetEnd] 范围内向前查找最优切分边界。
// 优先级：换行符 > 中文句末标点（。！？）> 英文句末（.!? 后跟空白）。
// 三级均未命中时返回原始 targetEnd，即直接按字符数截断。
//
// 安全保证：
//   - lookback = min(maxLookback, targetEnd)，确保下界 >= 0
//   - 前两级循环访问 runes[i-1]，下界限制为 i >= 1（lo >= 1）
//   - 第三级循环访问 runes[i-2]，下界限制为 i >= 2（lo3 >= 2）
func adjustToBoundary(runes []rune, targetEnd, maxLookback int) int {
	lookback := maxLookback
	if lookback > targetEnd {
		lookback = targetEnd
	}

	lo := targetEnd - lookback
	if lo < 1 {
		lo = 1
	}

	// 优先：换行符
	for i := targetEnd; i >= lo; i-- {
		if runes[i-1] == '\n' {
			return i
		}
	}
	// 次优：中文句末标点
	for i := targetEnd; i >= lo; i-- {
		r := runes[i-1]
		if r == '。' || r == '！' || r == '？' {
			return i
		}
	}
	// 再次：英文句末（.!? 后跟空白），需同时访问 runes[i-2]，下界提升至 2
	lo3 := lo
	if lo3 < 2 {
		lo3 = 2
	}
	for i := targetEnd; i >= lo3; i-- {
		r := runes[i-2]
		next := runes[i-1]
		if (r == '.' || r == '!' || r == '?') && (next == ' ' || next == '\t' || next == '\n') {
			return i
		}
	}
	return targetEnd
}
