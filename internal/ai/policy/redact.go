package policy

import (
	"encoding/json"
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

var (
	pemPrivateKeyPattern       = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	sensitiveHeaderPattern     = regexp.MustCompile(`(?im)^(Authorization|Proxy-Authorization|Cookie|Set-Cookie|X-API-Key|X-Auth-Token|MCP-Authorization)([ \t]*:[ \t]*)[^\r\n]*`)
	sensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|auth[_-]?token|password|passwd|secret|dsn|smtp[_-]?password)([ \t]*[:=][ \t]*)[^\s,;]+`)
	authorizationValuePattern  = regexp.MustCompile(`(?i)\b(Bearer|Basic)[ \t]+[A-Za-z0-9._~+/=-]+`)
	uriCredentialPattern       = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^:/@\s]+:)[^@\s]+@`)
	mysqlCredentialPattern     = regexp.MustCompile(`([A-Za-z0-9._-]+:)[^@\s]+@(tcp\(|unix\(|[^\s/]+/)`)
	cloudKeyPattern            = regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b|\bLTAI[A-Za-z0-9]{12,}\b|\bsk-[A-Za-z0-9_-]{16,}\b`)
)

// Redactor 是日志与所有持久化/观测后端共享的无状态脱敏器。
type Redactor struct{}

// NewRedactor 返回可并发复用的统一 Redactor。
func NewRedactor() Redactor {
	return Redactor{}
}

// Redact 递归脱敏结构化值，并保留非敏感字段。
func (Redactor) Redact(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	return redactDecoded(decoded, ""), nil
}

// RedactJSON 返回与身份 Hash 输入分离的规范化脱敏 JSON。
func (redactor Redactor) RedactJSON(value any) ([]byte, error) {
	redacted, err := redactor.Redact(value)
	if err != nil {
		return nil, err
	}
	return CanonicalJSON(redacted)
}

// RedactText 脱敏日志、错误或第三方观测属性中的常见凭证形式。
func (Redactor) RedactText(value string) string {
	value = pemPrivateKeyPattern.ReplaceAllString(value, redactedValue)
	value = sensitiveHeaderPattern.ReplaceAllString(value, `$1$2`+redactedValue)
	value = sensitiveAssignmentPattern.ReplaceAllString(value, `$1$2`+redactedValue)
	value = authorizationValuePattern.ReplaceAllString(value, `$1 `+redactedValue)
	value = uriCredentialPattern.ReplaceAllString(value, `$1`+redactedValue+`@`)
	value = mysqlCredentialPattern.ReplaceAllString(value, `$1`+redactedValue+`@$2`)
	return cloudKeyPattern.ReplaceAllString(value, redactedValue)
}

func redactDecoded(value any, key string) any {
	if isSensitiveKey(key) && value != nil {
		return redactedValue
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for childKey, child := range typed {
			result[childKey] = redactDecoded(child, childKey)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = redactDecoded(child, "")
		}
		return result
	case string:
		return NewRedactor().RedactText(typed)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	for _, exact := range []string{
		"authorization", "proxy_authorization", "cookie", "set_cookie",
		"password", "passwd", "pwd", "secret", "token", "api_key", "apikey", "dsn",
		"private_key", "client_secret", "credential", "credential_ref", "mcp_header",
	} {
		if normalized == exact {
			return true
		}
	}
	for _, suffix := range []string{"_password", "_passwd", "_secret", "_token", "_api_key", "_access_key", "_dsn", "_private_key", "_credential", "_credential_ref"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	if strings.Contains(normalized, "_secret_") {
		return true
	}
	return false
}

func containsSecretMaterial(value string) bool {
	return NewRedactor().RedactText(value) != value
}
