package policy

import (
	"strings"
	"testing"
)

func TestRedactorCoversStructuredAndTextSecrets(t *testing.T) {
	redactor := NewRedactor()
	input := map[string]any{
		"headers": map[string]any{
			"Authorization": "Bearer auth-plaintext",
			"Cookie":        "session=cookie-plaintext",
			"X-Request-ID":  "request-123",
		},
		"api_key":       "env:MODEL_API_KEY",
		"dsn":           "user:dsn-plaintext@tcp(db:3306)/sentinel",
		"smtp_password": "smtp-plaintext",
		"mcp": map[string]any{
			"env": map[string]any{
				"AWS_SECRET_ACCESS_KEY": "cloud-plaintext",
				"AWS_REGION":            "cn-hangzhou",
			},
		},
		"payload": "-----BEGIN PRIVATE KEY-----\npem-plaintext\n-----END PRIVATE KEY-----",
	}

	got, err := redactor.RedactJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	redacted := string(got)
	for _, secret := range []string{
		"auth-plaintext", "cookie-plaintext", "MODEL_API_KEY", "dsn-plaintext",
		"smtp-plaintext", "cloud-plaintext", "pem-plaintext",
	} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("RedactJSON() leaked %q: %s", secret, redacted)
		}
	}
	for _, safe := range []string{"request-123", "cn-hangzhou"} {
		if !strings.Contains(redacted, safe) {
			t.Fatalf("RedactJSON() removed non-secret %q: %s", safe, redacted)
		}
	}

	text := "Authorization: Bearer text-token\nCookie: session=text-cookie\napi_key=sk-12345678901234567890"
	masked := redactor.RedactText(text)
	for _, secret := range []string{"text-token", "text-cookie", "sk-12345678901234567890"} {
		if strings.Contains(masked, secret) {
			t.Fatalf("RedactText() leaked %q: %s", secret, masked)
		}
	}
}

func TestRedactorIsIdempotent(t *testing.T) {
	redactor := NewRedactor()
	once := redactor.RedactText("password=plaintext Authorization: Bearer token-value")
	twice := redactor.RedactText(once)
	if once != twice {
		t.Fatalf("RedactText() is not idempotent: once=%q twice=%q", once, twice)
	}
}
