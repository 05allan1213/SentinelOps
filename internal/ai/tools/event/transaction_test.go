package event_test

import (
	"context"
	"testing"

	"SentinelOps/internal/ai/policy"
	aitools "SentinelOps/internal/ai/tools"

	"github.com/cloudwego/eino/components/tool"
)

func TestTransactionalEffectEventMutationReusesOpsRegistryEndpoint(t *testing.T) {
	entry, err := policy.LookupCatalog("update_event_status")
	if err != nil {
		t.Fatal(err)
	}
	if entry.EffectType != policy.EffectTransactionalDB {
		t.Fatalf("update_event_status Effect type=%q", entry.EffectType)
	}
	instance := aitools.Get("update_event_status")
	if _, ok := instance.(tool.InvokableTool); !ok {
		t.Fatal("update_event_status no longer reuses the existing invokable ops endpoint")
	}
	info, err := instance.Info(context.Background())
	if err != nil || info == nil || info.Name != entry.Name {
		t.Fatalf("update_event_status ToolInfo=%#v err=%v", info, err)
	}
}
