package workflow

import (
	"testing"

	"SentinelOps/internal/testutil/faultmatrix"
)

// TestP43FaultMatrixStoreCases 逐项把持久化故障声明绑定到唯一 GORMStore 路径。
func TestP43FaultMatrixStoreCases(t *testing.T) {
	matrix, err := faultmatrix.Load("../../../manifest/ci/fault-matrix.yaml")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		id      string
		outcome string
		proof   func(*testing.T)
	}{
		{"external_pending_crash", faultmatrix.Parked, TestUnknownEffectExpiredRunningExternalBecomesParked},
		{"external_result_persist_crash", faultmatrix.Parked, TestUnknownWindowAfterSucceededEndpointLedgerFailureParksRun},
		{"effect_succeeded_tool_return_crash", faultmatrix.ResumePass, TestExternalEffectSucceededIsReusedAfterToolReturnCrash},
		{"lease_lost", faultmatrix.ResumePass, TestStaleGenerationCannotWriteTruth},
		{"mysql_checkpoint_transient", faultmatrix.ResumePass, TestEinoCheckpointStaleGenerationCannotOverwrite},
		{"mysql_approval_publish_transient", faultmatrix.Parked, TestTwoPhaseApprovalPublicationRollsBackIfEitherEventFails},
		{"mysql_effect_cas_transient", faultmatrix.Parked, TestEffectRollbackLeavesNoPartialTruthAndNeverUnknown},
		{"mysql_completion_transient", faultmatrix.ResumePass, TestCompleteRunTransactionFailureRollsBackEverything},
		{"budget_exhausted_during_retry", faultmatrix.Parked, TestBudgetCrashReservationPreservedAcrossLeaseHandoff},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			entry, ok := matrix.Case(tc.id)
			if !ok {
				t.Fatalf("fault matrix does not define %q", tc.id)
			}
			if entry.ExpectedOutcome != tc.outcome {
				t.Fatalf("outcome=%q want=%q", entry.ExpectedOutcome, tc.outcome)
			}
			tc.proof(t)
		})
	}
}
