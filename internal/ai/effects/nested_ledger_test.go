package effects

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
)

func TestNoDirectWriteMutationEndpointRequiresEffectOrLegacyPermit(t *testing.T) {
	if err := RequireMutationRoute(context.Background()); !errors.Is(err, ErrMutationRouteRequired) {
		t.Fatalf("plain context error=%v, want ErrMutationRouteRequired", err)
	}
	if err := RequireMutationRoute(WithLegacyMutationContext(context.Background())); err != nil {
		t.Fatalf("gated legacy context error=%v", err)
	}
	ctx := withExecutionMetadata(context.Background(), ExecutionMetadata{
		EffectKey: "phase26-effect", EffectStep: "primary", EffectRole: "primary", EffectType: policy.EffectTransactionalDB,
	})
	if err := RequireMutationRoute(ctx); err != nil {
		t.Fatalf("Effect callback context error=%v", err)
	}
}

func TestNestedLedgerOnlyLeafMutationOwnsEffectPlan(t *testing.T) {
	outer, err := policy.LookupCatalog("ops_agent")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := policy.LookupCatalog("block_ip")
	if err != nil {
		t.Fatal(err)
	}
	if outer.Risk != policy.RiskL0 || outer.EffectType != policy.EffectNone || len(outer.EffectSteps) != 0 {
		t.Fatalf("outer AgentTool would create a Ledger: %+v", outer)
	}
	dag, err := buildDAG("run-phase26", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", leaf)
	if err != nil {
		t.Fatal(err)
	}
	primary := 0
	for _, step := range dag {
		if step.Role == "primary" {
			primary++
		}
	}
	if primary != 1 {
		t.Fatalf("leaf Effect DAG primary count=%d, want 1; dag=%+v", primary, dag)
	}
}
