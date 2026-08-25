package ops

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestTransactionalEffectUpdateEventStatusKeepsOriginalActionEndpoint(t *testing.T) {
	instance := NewUpdateEventStatusTool()
	if _, ok := instance.(tool.InvokableTool); !ok {
		t.Fatal("update_event_status is not the existing invokable endpoint")
	}
	info, err := instance.Info(context.Background())
	if err != nil || info == nil || info.Name != "update_event_status" {
		t.Fatalf("update_event_status ToolInfo=%#v err=%v", info, err)
	}
}
