package ops

import (
	"context"
	"strings"
	"testing"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/prompt/agents"
)

func TestInventoryTriggerOpsPlanningOnly(t *testing.T) {
	if !strings.Contains(agents.Ops, "Proposal") || strings.Contains(agents.Ops, "告知用户已触发") || strings.Contains(agents.Ops, "trigger_ops 触发后") {
		t.Fatalf("ChatOps Prompt 未与 trigger_ops 只读规划合同对齐")
	}
	proposals, err := buildTriggerOpsProposals([]TriggerOpsProposalInput{
		{ToolName: "update_event_status", ArgumentsJSON: `{"status":"processing","event_id":"event-1"}`},
		{ToolName: "block_ip", ArgumentsJSON: `{"reason":"confirmed attack","ip":"192.0.2.10"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 2 {
		t.Fatalf("proposal 数量 = %d, want 2", len(proposals))
	}
	if proposals[0].Risk != policy.RiskL1 || proposals[1].Risk != policy.RiskL2 {
		t.Fatalf("风险必须来自 Catalog: %+v", proposals)
	}
	if string(proposals[0].Arguments) != `{"event_id":"event-1","status":"processing"}` {
		t.Fatalf("参数未 canonicalize: %s", proposals[0].Arguments)
	}
	if proposals[0].ToolRevision == "" || proposals[0].ToolSchemaHash == "" || len(proposals[0].EffectSteps) == 0 {
		t.Fatalf("Proposal 缺少服务端 Catalog 元数据: %+v", proposals[0])
	}

	for name, message := range map[string]string{
		"query_events": "not a durable Mutation Tool",
		"unknown":      "unknown catalog tool",
	} {
		_, err = buildTriggerOpsProposals([]TriggerOpsProposalInput{{ToolName: name, ArgumentsJSON: `{}`}})
		if err == nil || !strings.Contains(err.Error(), message) {
			t.Errorf("%q 应 fail-closed，得到 %v", name, err)
		}
	}
	if _, err = buildTriggerOpsProposals([]TriggerOpsProposalInput{
		{ToolName: "create_report", ArgumentsJSON: `{}`},
		{ToolName: "create_report", ArgumentsJSON: `{}`},
	}); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("重复 Proposal 应 fail-closed: %v", err)
	}
	if _, err = buildTriggerOpsProposals([]TriggerOpsProposalInput{
		{ToolName: "create_report", ArgumentsJSON: `{"title":"a","title":"b"}`},
	}); err == nil || !strings.Contains(err.Error(), "duplicate object key") {
		t.Fatalf("参数重名 key 应 fail-closed: %v", err)
	}

	info, err := NewTriggerOpsTool().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := policy.ToolSchemaHash(info)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := policy.LookupCatalog("trigger_ops")
	if err != nil {
		t.Fatal(err)
	}
	if hash != entry.SchemaHash {
		t.Fatalf("trigger_ops schema hash 漂移: got %s want %s", hash, entry.SchemaHash)
	}
}
