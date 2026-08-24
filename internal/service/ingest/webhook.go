package ingest

import (
	"encoding/json"
	"fmt"
	"time"
)

// ParseWebhook 解析通用 JSON Webhook 告警（兼容 Splunk / Elastic / 自定义格式）。
//
// 设计思想：
//
//	不同厂商的 Webhook 字段命名差异很大（title/name/alert_name/message），
//	通过优先级别名列表（alias fallback）实现零配置兼容，无需为每个厂商单独写适配器。
//
// 字段映射优先级：标准字段名 > 别名字段名 > 默认值。
func ParseWebhook(payload []byte, sourceName string) (*NormalizedAlert, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("webhook json 解析失败: %w", err)
	}

	alert := &NormalizedAlert{
		Source:       sourceName,
		IngestSource: "webhook",
		RawPayload:   string(payload),
		ExtraFields:  map[string]string{},
	}

	// 标题：title > name > alert_name > message
	alert.Title = strField(raw, "title", "name", "alert_name", "message")
	if alert.Title == "" {
		return nil, fmt.Errorf("webhook 缺少告警标题字段(title/name/alert_name/message)")
	}

	// 描述：description > details > body > summary
	alert.Content = strField(raw, "description", "details", "body", "summary")

	// 严重程度：severity > priority > level > criticality
	alert.Severity = normalizeSeverity(strField(raw, "severity", "priority", "level", "criticality"))

	// CVE：cve_id > cve
	alert.CVEID = strField(raw, "cve_id", "cve")

	// 来源系统名：source > vendor > product（覆盖传入的 sourceName）
	if s := strField(raw, "source", "vendor", "product"); s != "" {
		alert.Source = s
	}

	// 时间：timestamp > occurred_at > event_time（Unix 秒或 RFC3339）
	alert.OccurredAt = parseTime(raw, "timestamp", "occurred_at", "event_time")

	// 扩展字段
	for _, k := range []string{"link", "url", "host", "src_ip", "dst_ip", "user", "process"} {
		if v := strField(raw, k); v != "" {
			alert.ExtraFields[k] = v
		}
	}

	return alert, nil
}

// strField 从 map 中按优先级取第一个非空字符串值。
func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// parseTime 尝试从 map 中解析时间字段，失败时返回零值。
func parseTime(m map[string]any, keys ...string) time.Time {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch val := v.(type) {
		case float64:
			return time.Unix(int64(val), 0)
		case string:
			for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
				if t, err := time.Parse(layout, val); err == nil {
					return t
				}
			}
		}
	}
	return time.Time{}
}
