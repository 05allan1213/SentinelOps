// Package rule 提供 RAG 检索前的安全域术语归一化功能。
// 进程内缓存，单遍词边界替换，< 1ms 无 LLM 调用，适合热路径。
package rule

import (
	"context"
	"sort"
	"strings"
	"sync"

	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/frame/g"
)

// termRule 进程内规则缓存条目
type termRule struct {
	sourceLower string // 小写化原始词，用于大小写不敏感匹配
	source      string // 原始写法（保持原始大小写）
	target      string // 归一化目标词
	priority    int
}

var (
	termMu    sync.RWMutex
	termRules []termRule
)

// InitTermMappings 启动时从 MySQL 加载规则到进程内存（main.go 调用）。
// 失败时设置空规则集，Normalize 直接返回原始 query。
func InitTermMappings(ctx context.Context) {
	rules, err := dao.LoadTermMappings(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "[Rule] 加载术语规则失败，使用空规则集: %v", err)
		setTermRules(nil)
		return
	}
	setTermRules(rules)
	g.Log().Infof(ctx, "[Rule] 已加载 %d 条术语规则", len(rules))
}

// ReloadTermMappings 热重载进程内规则缓存（规则管理 API 写入后调用）。
// 返回加载的规则条数。
func ReloadTermMappings(ctx context.Context) int {
	rules, err := dao.LoadTermMappings(ctx)
	if err != nil {
		g.Log().Warningf(ctx, "[Rule] 热重载术语规则失败: %v", err)
		return 0
	}
	setTermRules(rules)
	n := len(rules)
	g.Log().Infof(ctx, "[Rule] 热重载完成，共 %d 条规则", n)
	return n
}

// setTermRules 将 DAO 层数据转换为进程内规则并写入缓存（含排序）。
func setTermRules(mappings []dao.TermMappingRow) {
	rules := make([]termRule, 0, len(mappings))
	for _, m := range mappings {
		if m.SourceTerm == "" {
			continue
		}
		rules = append(rules, termRule{
			sourceLower: strings.ToLower(m.SourceTerm),
			source:      m.SourceTerm,
			target:      m.TargetTerm,
			priority:    m.Priority,
		})
	}
	// 按 priority DESC, len(sourceLower) DESC 排序：优先级高的先匹配，同优先级下长词优先
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].priority != rules[j].priority {
			return rules[i].priority > rules[j].priority
		}
		return len(rules[i].sourceLower) > len(rules[j].sourceLower)
	})
	termMu.Lock()
	termRules = rules
	termMu.Unlock()
}

// Normalize 对查询串按优先级顺序依次应用所有启用的术语规则，完成大小写不敏感的全量替换。
//
// 替换策略：
//   - 规则在 setTermRules 中已按 priority DESC、len(source) DESC 预排序，
//     因此高优先级规则先匹配，同优先级下较长词先匹配，防止短词污染长词。
//   - 每条规则对当前 result 做一次完整的左→右扫描（replaceInsensitive），
//     扫描指针只前进不回退，已替换内容不会被本条规则重复命中。
//   - 多条规则串行叠加：第 N 条规则在第 N-1 条的输出上继续匹配，
//     因此替换结果（target）可能被后续低优先级规则再次命中。
//     若需避免此情形，可在种子数据中将相互依赖的规则设为相同优先级并调整顺序。
//
// 热路径：进程内缓存（termRules），< 1ms，无外部依赖。失败时静默返回原始 query。
func Normalize(ctx context.Context, query string) string {
	if query == "" {
		return query
	}

	// 快照读：仅在持锁期间复制 slice header（指针+长度+容量），
	// 不复制底层数组，之后无锁访问，避免规则遍历期间持锁阻塞热重载。
	termMu.RLock()
	rules := termRules
	termMu.RUnlock()

	if len(rules) == 0 {
		return query
	}

	result := query
	changed := false
	for _, rule := range rules {
		// 将当前 result 传入，以支持规则叠加（前一条规则的输出作为下一条的输入）
		replaced := replaceInsensitive(result, rule.sourceLower, rule.target)
		if replaced != result {
			result = replaced
			changed = true
		}
	}

	if changed {
		g.Log().Debugf(ctx, "[Rule] 术语归一化完成 | 原始=%q | 归一化=%q", query, result)
	}
	return result
}

// replaceInsensitive 在字符串 s 中查找所有满足词边界的 fromLower（大小写不敏感），
// 将其替换为 to，返回替换后的新字符串。
//
// 算法（左→右单遍扫描，指针只前进）：
//
//  1. 将 s 整体小写为 lower，用于匹配；s 本身保留原始大小写，用于拼接非匹配段。
//
//  2. 用 start 游标标记"尚未处理"的起始位置，初始为 0。
//
//  3. 在 lower[start:] 中搜索 fromLower 的下一个出现位置 idx（相对偏移）；
//     绝对位置 abs = start + idx。
//
//  4. 词边界检查：若 abs-1 或 abs+fromLen 处紧邻 ASCII 字母/数字，
//     则视为词内子串（如 "rce" 在 "force" 中），跳过此次匹配（start = abs+1）。
//
//  5. 命中且边界合法时：
//     - 将 s[start:abs]（匹配位置之前的原文）写入 Builder
//     - 将 to（替换词）写入 Builder
//     - 推进 start = abs + fromLen，跳过已被替换的原文段，不回头重扫
//
//  6. lower[start:] 中找不到 fromLower 时退出循环，
//     将 s[start:]（剩余原文）追加到 Builder 并返回。
//
//  7. 若 Builder 为空（从未写入），说明零次命中，直接返回 s 原始引用，不分配新内存。
//
// 使用 strings.Index 而非 regexp，避免热路径引入正则编译和回溯开销。
func replaceInsensitive(s, fromLower, to string) string {
	lower := strings.ToLower(s)
	fromLen := len(fromLower)

	var b strings.Builder
	start := 0
	for {
		// 在剩余片段中搜索目标词（idx 为相对 start 的偏移）
		idx := strings.Index(lower[start:], fromLower)
		if idx < 0 {
			break // 无更多匹配，退出
		}
		abs := start + idx // 转换为绝对位置

		// 词边界检查：避免 "rce" 命中 "force" 中的 "rce"
		if !isWordBoundary(s, abs, abs+fromLen) {
			start = abs + 1 // 跳过当前位置，继续向右搜索
			continue
		}

		b.WriteString(s[start:abs]) // 追加匹配位置之前的原文段
		b.WriteString(to)           // 追加替换词
		start = abs + fromLen       // 推进游标，跳过已替换的原文段
	}
	if b.Len() == 0 {
		return s // 零次命中：直接返回原始引用，避免无意义内存分配
	}
	b.WriteString(s[start:]) // 追加最后一个匹配之后的剩余原文
	return b.String()
}

// isWordBoundary 检查 [start, end) 位置是否在词边界。
// 左右相邻的 ASCII 字母/数字视为非边界（中文字符因 UTF-8 编码宽度 ≥ 2 不参与此检查）。
func isWordBoundary(s string, start, end int) bool {
	if start > 0 && isASCIIAlphaNum(s[start-1]) {
		return false
	}
	if end < len(s) && isASCIIAlphaNum(s[end]) {
		return false
	}
	return true
}

func isASCIIAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
