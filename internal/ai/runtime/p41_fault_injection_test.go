package runtime

import (
	"context"
	"testing"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/testutil/faultmatrix"
)

// TestFaultInjectionRepresentative 锁定恢复故障矩阵的三种结果，并用现有
// Recovery selector/官方 Runner 验证代表性的 Resume、Replay 和 parked 语义。
func TestFaultInjectionRepresentative(t *testing.T) {
	matrix, err := faultmatrix.Load("../../../manifest/ci/fault-matrix.yaml")
	if err != nil {
		t.Fatalf("load P41 fault matrix: %v", err)
	}
	if err := matrix.Validate(); err != nil {
		t.Fatalf("validate P41 fault matrix: %v", err)
	}

	const compatible = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		id         string
		facts      workflow.RecoveryFacts
		wantMode   workflow.RecoveryMode
		wantReason string
	}{
		{
			id: "hitl_checkpoint",
			facts: workflow.RecoveryFacts{
				RunID: "p41-hitl", RuntimeCompatibilityHash: compatible,
				Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointValid, ID: "sentinelops/p41-hitl"},
			},
			wantMode: workflow.RecoveryModeResume,
		},
		{
			id: "sigkill_no_checkpoint",
			facts: workflow.RecoveryFacts{
				RunID: "p41-replay", RuntimeCompatibilityHash: compatible, ImmutableQuery: "immutable p41 query",
				Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointMissing},
			},
			wantMode: workflow.RecoveryModeReplay,
		},
		{
			id: "approval_checkpoint_invalid",
			facts: workflow.RecoveryFacts{
				RunID: "p41-parked", RuntimeCompatibilityHash: compatible,
				HasPublishedApproval: true, Checkpoint: workflow.RecoveryCheckpoint{State: workflow.RecoveryCheckpointMissing},
			},
			wantMode: workflow.RecoveryModeParked, wantReason: workflow.ParkReasonCheckpointMissing,
		},
	} {
		t.Run(tc.id, func(t *testing.T) {
			entry, ok := matrix.Case(tc.id)
			if !ok {
				t.Fatalf("fault matrix does not define %q", tc.id)
			}
			decision, err := SelectRecovery(tc.facts, compatible)
			if err != nil {
				t.Fatalf("select recovery: %v", err)
			}
			if decision.Mode != tc.wantMode || decision.ParkReason != tc.wantReason {
				t.Fatalf("decision=%+v want mode=%s reason=%s", decision, tc.wantMode, tc.wantReason)
			}
			if got := faultmatrix.OutcomeForRecovery(string(decision.Mode), decision.ParkReason); got != entry.ExpectedOutcome {
				t.Fatalf("outcome=%q want matrix=%q", got, entry.ExpectedOutcome)
			}
		})
	}

	store := &p12MemoryCheckpointStore{values: map[string][]byte{}}
	agent := &p12RecoveryAgent{state: p12RegisteredRecoveryState{Marker: "p41"}}
	runner, err := NewDurableRunner(context.Background(), agent, store, false)
	if err != nil {
		t.Fatalf("new durable runner: %v", err)
	}
	attempt := p12Attempt("p41-replay", 2, "p41-trace-replay")
	execution, err := InvokeRecoveryRunner(context.Background(), runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "immutable p41 query",
	}, nil)
	if err != nil {
		t.Fatalf("invoke replay: %v", err)
	}
	p12DrainRuntimeEvents(t, execution.Events)
	if agent.runQuery != "immutable p41 query" || agent.runID != attempt.Run.ID {
		t.Fatalf("replay identity/query=%q/%q", agent.runQuery, agent.runID)
	}
}
