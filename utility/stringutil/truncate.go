package stringutil

// TruncateRunes 按 rune 数量截断字符串,超出时追加省略号
func TruncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// TruncateBytes 按字节长度截断字符串,在 UTF-8 字符边界截断
func TruncateBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// 向前查找 UTF-8 字符边界
	for i := maxBytes; i > 0; i-- {
		if (s[i] & 0xC0) != 0x80 {
			return s[:i]
		}
	}
	return ""
}

// TruncateError 截断错误消息到指定长度
func TruncateError(err error, maxLen int) string {
	if err == nil {
		return ""
	}
	// 错误消息可能包含多字节字符；按字节裸切会劈开 UTF-8 序列，写入
	// MySQL VARCHAR 时报 Incorrect string value 并丢失整条 trace 错误。
	return TruncateBytes(err.Error(), maxLen)
}
