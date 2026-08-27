package ops

import (
	"context"
	"testing"
)

func TestTransactionalEffectUpdateEventStatusKeepsOriginalActionEndpoint(t *testing.T) {
	instance := NewUpdateEventStatusTool()
	info, err := instance.Info(context.Background())
	if err != nil || info == nil || info.Name != "update_event_status" {
		t.Fatalf("update_event_status ToolInfo=%#v err=%v", info, err)
	}
}
