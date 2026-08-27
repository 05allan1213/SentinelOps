//go:build p43_fault_matrix

package runtime

import (
	"testing"

	"SentinelOps/internal/testutil/faultmatrix"
)

// TestP43FaultMatrixRuntimeCases 逐项把 Runtime 故障声明绑定到既有生产路径测试。
func TestP43FaultMatrixRuntimeCases(t *testing.T) {
	matrix, err := faultmatrix.Load("../../../manifest/ci/fault-matrix.yaml")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		id      string
		outcome string
		proof   func(*testing.T)
	}{
		{"hitl_checkpoint", faultmatrix.ResumePass, TestTwoPhaseApprovalRunnerPublishesOnlyAfterCheckpointEvent},
		{"sigterm_drain", faultmatrix.ResumePass, TestWorkerSafePointCancelCommitsCheckpointForHandoff},
		{"sigkill_no_checkpoint", faultmatrix.ReplayPass, TestResumeAndReplayUseOfficialRunnerAndSafeSessionIdentity},
		{"sigkill_valid_checkpoint", faultmatrix.ResumePass, TestResumeAndReplayUseOfficialRunnerAndSafeSessionIdentity},
		{"approval_checkpoint_invalid", faultmatrix.Parked, TestParkedOnOpaqueCheckpointDecodeFailure},
		{"no_checkpoint_no_dependency", faultmatrix.ReplayPass, TestResumeAndReplayUseOfficialRunnerAndSafeSessionIdentity},
		{"model_stream_read_failure", faultmatrix.ReplayPass, TestStreamFailureSettlesPhysicalCallAsFailed},
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
