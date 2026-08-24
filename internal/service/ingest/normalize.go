// Package ingest 提供多源告警接入与归一化能力。
//
// 解决的问题：
//
//	企业安全环境中存在多种异构安全设备（SIEM、EDR、WAF、IDS），各自使用不同的告警格式
//	和字段命名规范，导致告警数据无法统一处理。本包通过归一化层将所有格式转换为统一的
//	NormalizedAlert 结构，屏蔽上游差异，使下游 AI 分析管道无需感知数据来源。
//
// 支持格式：
//   - 通用 JSON Webhook（Splunk / Elastic / 自定义）
//   - CEF（ArcSight Common Event Format）
//   - LEEF（IBM QRadar Log Event Extended Format）
//   - 标准 REST API 推送（新系统对接推荐）
//
// 归一化流程：
//
//	原始 payload → 格式解析器 → NormalizedAlert → 去重（DedupKey）→ 写入 events 表 → 异步向量索引
package ingest

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// NormalizedAlert 归一化后的统一告警结构，与 dao.Event 字段对齐。
type NormalizedAlert struct {
	Title        string            // 告警标题/名称
	Content      string            // 详细描述
	Severity     string            // critical / high / medium / low / info
	Source       string            // 来源系统名称（如 "Splunk", "QRadar"）
	IngestSource string            // 接入渠道：webhook / cef / leef / api_push
	CVEID        string            // CVE 编号（可选）
	RawPayload   string            // 原始 payload（JSON 字符串）
	ExtraFields  map[string]string // 扩展字段（link、host、src_ip 等）
	OccurredAt   time.Time         // 告警发生时间（来源系统时间，零值用当前时间）
}

// DedupKey 计算去重键：SHA256(title|source|content[:200])
// 与 events 表 dedup_key 字段逻辑一致。
func (a *NormalizedAlert) DedupKey() string {
	preview := a.Content
	if len([]rune(preview)) > 200 {
		runes := []rune(preview)
		preview = string(runes[:200])
	}
	raw := fmt.Sprintf("%s|%s|%s", a.Title, a.Source, preview)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)[:32]
}

// normalizeSeverity 将各厂商严重程度映射到统一枚举。
func normalizeSeverity(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "critical", "fatal", "emergency", "10", "9", "8":
		return "critical"
	case "high", "error", "7", "6":
		return "high"
	case "medium", "warning", "warn", "5", "4":
		return "medium"
	case "low", "notice", "3", "2":
		return "low"
	default:
		return "info"
	}
}
