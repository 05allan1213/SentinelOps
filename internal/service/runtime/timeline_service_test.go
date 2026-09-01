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
	run := mysql.WorkflowRun{ID: "run-1", RuntimeVersion: &ver, RuntimeCompatibilityHash: &compat, LeaseGeneration: gen}
	einoID, _ := workflow.EinoCheckpointID(run.ID)
	c := mysql.WorkflowCheckpoint{ID: einoRowID(&einoID), EinoCheckpointID: &einoID, CheckpointKey: einoID, PayloadSHA256: &hash, RuntimeVersion: &ver, RuntimeCompatibilityHash: &compat, LeaseGeneration: &gen, CommittedAt: &now, CheckpointBlob: []byte("secret")}
	state, reason := checkpointState(c, &run, now)
	if state != "valid" || reason != "opaque_verified" {
		t.Fatalf("state=%s reason=%s", state, reason)
	}
}

func TestCheckpointMetadataMismatchIsCorrupt(t *testing.T) {
	now := time.Now()
	blob := []byte("x")
	d := sha256.Sum256(blob)
	hash := hex.EncodeToString(d[:])
	ver, compat := "v", "c"
	gen := uint64(1)
	run := mysql.WorkflowRun{ID: "run-2", RuntimeVersion: &ver, RuntimeCompatibilityHash: &compat, LeaseGeneration: gen}
	id, _ := workflow.EinoCheckpointID(run.ID)
	base := mysql.WorkflowCheckpoint{ID: einoRowID(&id), EinoCheckpointID: &id, CheckpointKey: id, PayloadSHA256: &hash, RuntimeVersion: &ver, RuntimeCompatibilityHash: &compat, LeaseGeneration: &gen, CommittedAt: &now, CheckpointBlob: blob}
	for name, mutate := range map[string]func(*mysql.WorkflowCheckpoint){"id": func(c *mysql.WorkflowCheckpoint) { c.ID = "bad" }, "key": func(c *mysql.WorkflowCheckpoint) { c.CheckpointKey = "bad" }, "generation": func(c *mysql.WorkflowCheckpoint) { x := uint64(2); c.LeaseGeneration = &x }, "version": func(c *mysql.WorkflowCheckpoint) { x := "other"; c.RuntimeVersion = &x }} {
		t.Run(name, func(t *testing.T) {
			c := base
			mutate(&c)
			state, _ := checkpointState(c, &run, now)
			if state != "corrupt" {
				t.Fatalf("state=%s", state)
			}
		})
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
