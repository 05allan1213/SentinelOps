package effects

import (
	"strings"
	"testing"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
)

func TestMilvusDerivedEffectUsesExistingCatalogDAG(t *testing.T) {
	entry, err := policy.LookupCatalog("save_intelligence")
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.EffectSteps) != 2 || entry.EffectSteps[0] != workflow.EffectStepPrimary || entry.EffectSteps[1] != "milvus_index" {
		t.Fatalf("catalog effect steps=%v", entry.EffectSteps)
	}
	proposalHash := strings.Repeat("a", 64)
	dag, err := buildDAG("run-31", proposalHash, entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(dag) != 2 || dag[1].Role != workflow.EffectRoleDerived || dag[1].ParentKey != dag[0].Key || dag[1].Step != "milvus_index" {
		t.Fatalf("dag=%#v", dag)
	}
	again, err := buildDAG("run-31", proposalHash, entry)
	if err != nil || dag[1].Key != again[1].Key {
		t.Fatalf("derived key is not stable: first=%#v again=%#v err=%v", dag, again, err)
	}
}
