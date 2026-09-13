package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestHasUnverifiedMutationClaim(t *testing.T) {
	cases := []struct {
		name       string
		answer     string
		facts      bool
		unverified bool
	}{
		{name: "tool claim without facts", answer: "已成功调用 `create_report` 工具，报告已写入报告库。", facts: false, unverified: true},
		{name: "tool claim with facts", answer: "已成功调用 `create_report` 工具，报告已写入报告库。", facts: true, unverified: false},
		{name: "save phrase without tool name", answer: "**报告状态**：已生成并保存至报告库。", facts: false, unverified: true},
		{name: "english claim", answer: "The report has been saved via create_report.", facts: false, unverified: true},
		// 真实联调样本：报告 Run 未调用 create_report 却声称已保存（Run c0f174cc / fa3e69cc）。
		{name: "observed report status claim", answer: "## 安全运营周报生成完成\n\n**报告状态**：已生成并保存至报告库\n**统计周期**：2026-09-07 ~ 2026-09-13", facts: false, unverified: true},
		{name: "observed replanner claim", answer: "已成功调用 `create_report` 工具，将《int2 联调周报》的正文内容保存到报告库中。\n\n**执行结果摘要：**", facts: false, unverified: true},
		{name: "negated claim", answer: "本轮未调用 create_report，因此报告没有保存。", facts: false, unverified: false},
		{name: "plain content", answer: "# 安全运营周报\n\n本周共处理 12 起事件，建议继续观察。", facts: false, unverified: false},
		{name: "empty answer", answer: "   ", facts: false, unverified: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasUnverifiedMutationClaim(tc.answer, tc.facts); got != tc.unverified {
				t.Fatalf("HasUnverifiedMutationClaim(%q, facts=%v)=%v want %v", tc.answer, tc.facts, got, tc.unverified)
			}
		})
	}
}

func TestMutationToolNamesComeFromCatalog(t *testing.T) {
	names := strings.Join(MutationToolNames(), ",")
	for _, want := range []string{"create_report", "block_ip", "notify_email"} {
		if !strings.Contains(names, want) {
			t.Fatalf("mutation tools %q missing %s", names, want)
		}
	}
	if strings.Contains(names, "query_events") || strings.Contains(names, "get_current_time") {
		t.Fatalf("L0 read tools must not be treated as mutations: %q", names)
	}
}

func TestRunMutationFactsDetectsEffectAndApproval(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "mutation_claim_facts")
	store := workflow.NewGORMStore(db)
	ctx := context.Background()

	if RunMutationFacts(ctx, store, "run-no-facts") {
		t.Fatal("empty run reported mutation facts")
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	effect := &mysql.AgentEffect{
		ID: "effect-claim", RunID: "run-effect", IdempotencyKey: "idem-claim", EffectRole: "primary",
		EffectStep: "primary", ProposalHash: strings.Repeat("a", 64), ToolName: "create_report",
		ToolRevision: "v1", ToolSchemaHash: strings.Repeat("b", 64), TargetHash: strings.Repeat("c", 64),
		EffectType: "transactional_db", Status: "succeeded", Version: 1, LeaseGeneration: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(effect).Error; err != nil {
		t.Fatalf("seed effect: %v", err)
	}
	if !RunMutationFacts(ctx, store, "run-effect") {
		t.Fatal("effect fact not detected")
	}
	if RunMutationFacts(ctx, store, "run-no-facts") {
		t.Fatal("unrelated run reported mutation facts")
	}

	approval := &mysql.AgentApproval{
		ID: "approval-claim", RunID: "run-approval", ToolName: "create_report", ToolRevision: "v1",
		ToolSchemaHash: strings.Repeat("d", 64), ProposalHash: strings.Repeat("e", 64),
		ProposalJSONRedacted: `{}`, RiskLevel: "L1", PolicyHash: strings.Repeat("f", 64),
		RuntimeCompatibilityHash: strings.Repeat("0", 64), RequestedBy: "user-1",
		Status: "pending", Version: 1, PreparingAt: now, CreatedAt: now,
	}
	if err := db.Create(approval).Error; err != nil {
		t.Fatalf("seed approval: %v", err)
	}
	if !RunMutationFacts(ctx, store, "run-approval") {
		t.Fatal("approval fact not detected")
	}
}
