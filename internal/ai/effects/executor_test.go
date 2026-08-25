package effects

import (
	"reflect"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestEffectIdentityExecutorInputIgnoresToolCallID(t *testing.T) {
	entry, err := policy.LookupCatalog("save_intelligence")
	if err != nil {
		t.Fatal(err)
	}
	base := TransactionalRequest{
		ToolName: entry.Name, ToolRevision: entry.Revision, ToolSchemaHash: entry.SchemaHash,
		ArgumentsJSON: `{"cve_id":"CVE-2026-0001","title":"stable"}`,
		EffectSteps:   entry.EffectSteps,
	}
	first := base
	first.ToolCallIDObserved = "call-before-replay"
	second := base
	second.ToolCallIDObserved = "call-after-replay"
	left, err := buildTransitionInput(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := buildTransitionInput(second)
	if err != nil {
		t.Fatal(err)
	}
	left.ToolCallIDObserved = ""
	right.ToolCallIDObserved = ""
	if !reflect.DeepEqual(left, right) || left.TargetHash == "" || len(left.Derived) != 1 || left.Derived[0].Step != "milvus_index" {
		t.Fatalf("tool_call_id changed Effect identity input: left=%#v right=%#v", left, right)
	}
}

func TestTransactionalEffectExecutorRejectsExternalCatalogEntries(t *testing.T) {
	entry, err := policy.LookupCatalog("block_ip")
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildTransitionInput(TransactionalRequest{
		ToolName: entry.Name, ToolRevision: entry.Revision, ToolSchemaHash: entry.SchemaHash,
		ArgumentsJSON: `{"ip":"192.0.2.1"}`, EffectSteps: entry.EffectSteps,
	})
	if err == nil {
		t.Fatal("P24 external Effect entered P23 Executor")
	}
}
