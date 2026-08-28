package report

import (
	"context"
	"testing"
)

func TestTransactionalEffectCreateReportKeepsOriginalInvokableEndpoint(t *testing.T) {
	instance := NewCreateReportTool()
	info, err := instance.Info(context.Background())
	if err != nil || info == nil || info.Name != "create_report" {
		t.Fatalf("create_report ToolInfo=%#v err=%v", info, err)
	}
}
