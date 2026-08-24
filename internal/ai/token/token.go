// Package token 提供轻量级的 token 数量估算工具，用于触发对话历史压缩的阈值判断。
//
// 估算规则（基于主流 LLM 分词器的统计规律）：
//   - 中文汉字：1字符 ≈ 1 token（汉字通常独立成 token）
//   - 英文/符号：4字符 ≈ 1 token（英文单词平均长度约 4-5 字母）
//   - 每条消息固定开销：+10 tokens（role 标签、分隔符、特殊标记等）
package token

import "github.com/cloudwego/eino/schema"

// EstimateMessages 估算消息列表的 token 总量。
// 对每条消息调用 EstimateContent，再加上每条消息固定的 role 开销（+10 tokens）。
func EstimateMessages(msgs []*schema.Message) int {
	total := 0
	for _, msg := range msgs {
		total += EstimateContent(msg.Content) + 10
	}
	return total
}

// EstimateContent 估算单条消息正文的 token 数（不含 role 开销）。
// 逐码点遍历：中文字符按 1 token 计，其余字符按 4字符 1 token 计（取整后 +1 保底）。
func EstimateContent(content string) int {
	cjk, other := 0, 0
	for _, r := range content {
		if isChineseRune(r) {
			cjk++
		} else {
			other++
		}
	}
	return cjk + other/4 + 1
}

// isChineseRune 判断一个 Unicode 码点是否属于中文字符或中文标点范围。
//
// 涵盖的 Unicode 区块：
//   - U+4E00–U+9FFF：常用中文汉字（基本区，覆盖绝大多数日常用字）
//   - U+3400–U+4DBF：中文扩展-A（生僻字、古汉字）
//   - U+F900–U+FAFF：中文兼容汉字（Unicode 历史遗留的重复编码区块）
//   - U+3000–U+303F：中文标点及符号（。，、《》「」…等）
//   - U+FF00–U+FFEF：全角 ASCII 字符（全角字母、全角数字、全角符号）
func isChineseRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // 常用中文汉字
		(r >= 0x3400 && r <= 0x4DBF) || // 中文扩展-A（生僻字）
		(r >= 0xF900 && r <= 0xFAFF) || // 中文兼容汉字
		(r >= 0x3000 && r <= 0x303F) || // 中文标点符号
		(r >= 0xFF00 && r <= 0xFFEF) // 全角字符
}
