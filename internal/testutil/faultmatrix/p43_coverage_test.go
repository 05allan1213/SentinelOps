package faultmatrix

import "testing"

// TestP43FaultMatrixHasAllExecutableCases 锁定 P43 必须逐项执行的 18 个故障 ID。
func TestP43FaultMatrixHasAllExecutableCases(t *testing.T) {
	matrix, err := Load("../../../manifest/ci/fault-matrix.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = matrix.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"hitl_checkpoint", "sigterm_drain", "sigkill_no_checkpoint", "sigkill_valid_checkpoint",
		"external_pending_crash", "external_result_persist_crash", "effect_succeeded_tool_return_crash",
		"lease_lost", "approval_checkpoint_invalid", "no_checkpoint_no_dependency",
		"mcp_init_disconnect", "mcp_call_disconnect", "model_stream_read_failure",
		"mysql_checkpoint_transient", "mysql_approval_publish_transient", "mysql_effect_cas_transient",
		"mysql_completion_transient", "budget_exhausted_during_retry",
	}
	if len(matrix.Cases) != len(want) {
		t.Fatalf("fault cases=%d want=%d", len(matrix.Cases), len(want))
	}
	for _, id := range want {
		if _, ok := matrix.Case(id); !ok {
			t.Errorf("fault matrix does not define %q", id)
		}
	}
}
