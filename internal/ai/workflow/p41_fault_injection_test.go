package workflow

import (
	"testing"

	"SentinelOps/internal/testutil/faultmatrix"
)

// TestFaultInjectionRepresentative 约束 parked/非终态 Session 占用、终态
// 释放和 generation fence 的声明式故障结果。
func TestFaultInjectionRepresentative(t *testing.T) {
	matrix, err := faultmatrix.Load("../../../manifest/ci/fault-matrix.yaml")
	if err != nil {
		t.Fatalf("load P41 fault matrix: %v", err)
	}
	if err := matrix.Validate(); err != nil {
		t.Fatalf("validate P41 fault matrix: %v", err)
	}

	for _, status := range []string{RunStatusPending, RunStatusRunning, RunStatusWaitingApproval, RunStatusRetryableFailed, RunStatusParked, RunStatusReconciling} {
		if !RunOccupiesSession(status) {
			t.Fatalf("status %q released active Session", status)
		}
	}
	for _, status := range []string{RunStatusSuccess, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled} {
		if RunOccupiesSession(status) {
			t.Fatalf("terminal status %q still occupies active Session", status)
		}
	}

	if err := ValidateRunTransition(RunStatusRunning, RunStatusParked, TransitionIntentDefault, ParkReasonEffectUnknown); err != nil {
		t.Fatalf("effect unknown parking denied: %v", err)
	}
	if err := ValidateRunTransition(RunStatusRunning, RunStatusSuccess, TransitionIntentDefault, ""); err == nil {
		t.Fatal("legacy success spelling was accepted for a durable Run")
	}

	for _, id := range []string{
		"sigterm_drain", "sigkill_no_checkpoint", "sigkill_valid_checkpoint",
		"external_result_persist_crash", "lease_lost", "approval_checkpoint_invalid",
		"no_checkpoint_no_dependency",
	} {
		entry, ok := matrix.Case(id)
		if !ok {
			t.Fatalf("fault matrix missing %q", id)
		}
		if entry.RunIDUnchanged != true || entry.AttemptRotates != true {
			t.Fatalf("case %q identity invariants=%+v", id, entry)
		}
	}
}
