package report

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestTransactionalEffectCreateReportKeepsOriginalInvokableEndpoint(t *testing.T) {
	instance := NewCreateReportTool()
	if _, ok := instance.(tool.InvokableTool); !ok {
		t.Fatal("create_report no longer exposes the original invokable endpoint callback")
	}
	info, err := instance.Info(context.Background())
	if err != nil || info == nil || info.Name != "create_report" {
		t.Fatalf("create_report ToolInfo=%#v err=%v", info, err)
	}
}
