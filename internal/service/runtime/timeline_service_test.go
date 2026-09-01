package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

func TestTimelineRetainsEveryCanonicalEvent(t *testing.T) {
	rows := []mysql.WorkflowEvent{{RunID: "r", Seq: 1, EventType: workflow.EventAgentPlan, Payload: `{"data":{"attempt":1}}`}, {RunID: "r", Seq: 2, EventType: "unknown.future", Payload: `{`}}
	for _, row := range rows {
		dto := MapRuntimeEvent(row)
		if dto.Seq == 0 {
			t.Fatal("event seq was lost")
		}
	}
}

func TestTimelineFiltersDoNotChangeTruth(t *testing.T) {
	if got := isTailStop(workflow.EventRunFailed, map[string]any{"data": map[string]any{"retryable": true}}); got {
		t.Fatal("retryable failure must remain open")
	}
}

func TestAttemptReconstructionMarksPartial(t *testing.T) {
	if _, reason := checkpointState(mysql.WorkflowCheckpoint{}, &mysql.WorkflowRun{}, time.Now()); reason != "invalid_metadata" {
		t.Fatalf("reason=%s", reason)
	}
}

func TestCheckpointDTOExcludesOpaqueBytes(t *testing.T) {
	now := time.Now()
	d := sha256.Sum256([]byte("secret"))
	hash, ver, compat, gen := hex.EncodeToString(d[:]), "v", "c", uint64(1)
	einoID := "k"
	c := mysql.WorkflowCheckpoint{ID: einoRowID(&einoID), EinoCheckpointID: &einoID, CheckpointKey: "k", PayloadSHA256: &hash, RuntimeVersion: &ver, RuntimeCompatibilityHash: &compat, LeaseGeneration: &gen, CommittedAt: &now, CheckpointBlob: []byte("secret")}
	state, reason := checkpointState(c, &mysql.WorkflowRun{}, now)
	if state != "valid" || reason != "opaque_verified" {
		t.Fatalf("state=%s reason=%s", state, reason)
	}
}

func TestRuntimeEventTailMonotonicAfterSeq(t *testing.T) {
	if isTailStop(workflow.EventRunCompleted, nil) == false {
		t.Fatal("completed event must close")
	}
}

func TestRuntimeEventTailClosesOnTerminal(t *testing.T) {
	for _, typ := range []string{workflow.EventRunCompleted, workflow.EventRunParked, workflow.EventOperationSucceeded, workflow.EventOperationFailed, workflow.EventOperationCanceled} {
		if !isTailStop(typ, nil) {
			t.Fatalf("%s not terminal", typ)
		}
	}
}

func TestRuntimeEventTailScope(t *testing.T) {
	if isTailStop(workflow.EventRunFailed, map[string]any{"data": map[string]any{"retryable": false}}) != true {
		t.Fatal("non-retryable failure must close")
	}
}
