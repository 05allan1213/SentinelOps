package tools_test

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
	toolsreport "SentinelOps/internal/ai/tools/report"
)

func TestAuthDisabledRejectsBusinessWrites(t *testing.T) {
	t.Parallel()
	tool := toolsreport.NewCreateReportTool()
	ctx := policy.WithIdentity(context.Background(), policy.DisabledIdentity())
	_, err := tool.InvokableRun(ctx, `{"title":"blocked","content":"blocked","type":"custom"}`)
	if !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("auth-disabled Agent mutation reached endpoint: %v", err)
	}
}
