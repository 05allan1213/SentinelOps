package ingest

import (
	"fmt"
	"strings"
)

// ParseCEF 解析 ArcSight CEF 格式告警。
// CEF 格式：CEF:Version|Device Vendor|Device Product|Device Version|Signature ID|Name|Severity|Extension
func ParseCEF(line string, sourceName string) (*NormalizedAlert, error) {
	// 去掉可能的 syslog 前缀（<PRI>timestamp hostname）
	if idx := strings.Index(line, "CEF:"); idx > 0 {
		line = line[idx:]
	}
	if !strings.HasPrefix(line, "CEF:") {
		return nil, fmt.Errorf("非 CEF 格式")
	}

	// 分割头部（前7个 | 分隔字段）
	parts := strings.SplitN(line, "|", 8)
	if len(parts) < 7 {
		return nil, fmt.Errorf("CEF 头部字段不足: %d", len(parts))
	}

	vendor := parts[1]
	product := parts[2]
	name := strings.TrimSpace(parts[5])
	severity := strings.TrimSpace(parts[6])

	alert := &NormalizedAlert{
		Title:        name,
		Source:       fmt.Sprintf("%s/%s", vendor, product),
		IngestSource: "cef",
		Severity:     normalizeSeverity(severity),
		RawPayload:   line,
		ExtraFields:  map[string]string{},
	}
	if sourceName != "" {
		alert.Source = sourceName
	}

	// 解析扩展字段（key=value，value 中 \= 转义）
	if len(parts) == 8 {
		ext := parseCEFExtension(parts[7])
		alert.Content = ext["msg"]
		for _, k := range []string{"src", "dst", "shost", "dhost", "fname", "request", "cs1", "cs2"} {
			if v := ext[k]; v != "" {
				alert.ExtraFields[k] = v
			}
		}
		if v := ext["src"]; v != "" {
			alert.ExtraFields["src_ip"] = v
		}
		if v := ext["dst"]; v != "" {
			alert.ExtraFields["dst_ip"] = v
		}
	}

	if alert.Title == "" {
		return nil, fmt.Errorf("CEF 告警名称为空")
	}
	return alert, nil
}

// parseCEFExtension 解析 CEF 扩展字段字符串为 map。
func parseCEFExtension(ext string) map[string]string {
	result := map[string]string{}
	tokens := strings.Fields(ext)
	var curKey, curVal string
	for _, tok := range tokens {
		if idx := strings.Index(tok, "="); idx > 0 {
			if curKey != "" {
				result[curKey] = strings.ReplaceAll(curVal, "\\=", "=")
			}
			curKey = tok[:idx]
			curVal = tok[idx+1:]
		} else if curKey != "" {
			curVal += " " + tok
		}
	}
	if curKey != "" {
		result[curKey] = strings.ReplaceAll(curVal, "\\=", "=")
	}
	return result
}

// ParseLEEF 解析 IBM QRadar LEEF 格式告警。
// LEEF 格式：LEEF:Version|Vendor|Product|Version|EventID|key\tvalue\t...
func ParseLEEF(line string, sourceName string) (*NormalizedAlert, error) {
	if idx := strings.Index(line, "LEEF:"); idx > 0 {
		line = line[idx:]
	}
	if !strings.HasPrefix(line, "LEEF:") {
		return nil, fmt.Errorf("非 LEEF 格式")
	}

	parts := strings.SplitN(line, "|", 6)
	if len(parts) < 5 {
		return nil, fmt.Errorf("LEEF 头部字段不足: %d", len(parts))
	}

	vendor := parts[1]
	product := parts[2]
	eventID := strings.TrimSpace(parts[4])
	if eventID == "" {
		return nil, fmt.Errorf("LEEF EventID 为空")
	}

	alert := &NormalizedAlert{
		Title:        eventID,
		Source:       fmt.Sprintf("%s/%s", vendor, product),
		IngestSource: "leef",
		RawPayload:   line,
		ExtraFields:  map[string]string{},
	}
	if sourceName != "" {
		alert.Source = sourceName
	}

	// 解析属性（tab 分隔的 key=value）
	if len(parts) == 6 {
		attrs := parseLEEFAttrs(parts[5])
		alert.Content = attrs["msg"]
		alert.Severity = normalizeSeverity(attrs["sev"])
		if v := attrs["src"]; v != "" {
			alert.ExtraFields["src_ip"] = v
		}
		if v := attrs["dst"]; v != "" {
			alert.ExtraFields["dst_ip"] = v
		}
		if v := attrs["usrName"]; v != "" {
			alert.ExtraFields["user"] = v
		}
	}

	if alert.Severity == "" {
		alert.Severity = "info"
	}
	return alert, nil
}

func parseLEEFAttrs(s string) map[string]string {
	result := map[string]string{}
	sep := "\t"
	if !strings.Contains(s, "\t") {
		sep = " "
	}
	for _, pair := range strings.Split(s, sep) {
		if idx := strings.Index(pair, "="); idx > 0 {
			result[pair[:idx]] = pair[idx+1:]
		}
	}
	return result
}
