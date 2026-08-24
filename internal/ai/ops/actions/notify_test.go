package actions

import (
	"context"
	"strings"
	"testing"
)

func TestConfigRejectsPlaintextSMTPParameter(t *testing.T) {
	_, err := (&EmailAction{}).Execute(context.Background(), map[string]string{
		"smtp_host": "smtp.example.test",
		"smtp_pass": "plaintext-is-forbidden",
	})
	if err == nil || !strings.Contains(err.Error(), "smtp_pass") {
		t.Fatalf("Execute() error = %v, want plaintext SMTP parameter rejection", err)
	}
}
