// redact 使用现有 P06 Redactor 清理 CI 日志和报告，不保留 Secret 原值。
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"SentinelOps/internal/ai/policy"
)

func main() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<20))
	if err != nil {
		fmt.Fprintln(os.Stderr, "read CI payload:", err)
		os.Exit(1)
	}
	redacted := policy.NewRedactor().RedactText(string(data))
	for _, name := range []string{
		"SENTINELOPS_MODEL_API_KEY",
		"SENTINELOPS_EVAL_DSN",
		"SENTINELOPS_EVAL_AUTHORIZATION",
		"SENTINELOPS_JWT_SECRET",
		"SENTINELOPS_ADMIN_PASSWORD",
	} {
		if value := os.Getenv(name); value != "" {
			redacted = strings.ReplaceAll(redacted, value, "[REDACTED]")
		}
	}
	if _, err := io.WriteString(os.Stdout, redacted); err != nil {
		fmt.Fprintln(os.Stderr, "write redacted CI payload:", err)
		os.Exit(1)
	}
}
