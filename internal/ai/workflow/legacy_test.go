package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestLegacyRuntimeContractNeverBecomesDurableEligible(t *testing.T) {
	db := newP07Database(t, "legacy_contract")
	now := time.Now().Truncate(time.Millisecond)
	validJSON := `{}`
	fixtures := []mysql.WorkflowRun{
		{ID: "legacy", WorkflowKey: "test", RuntimeMode: RuntimeModeLegacy, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: ptrString("v1"), RuntimeCompatibilityHash: ptrString("hash"), StartedAt: now},
		{ID: "null-input", WorkflowKey: "test", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, RuntimeVersion: ptrString("v1"), RuntimeCompatibilityHash: ptrString("hash"), StartedAt: now},
		{ID: "null-version", WorkflowKey: "test", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeCompatibilityHash: ptrString("hash"), StartedAt: now},
		{ID: "null-hash", WorkflowKey: "test", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: ptrString("v1"), StartedAt: now},
		{ID: "durable", WorkflowKey: "test", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: ptrString("v1"), RuntimeCompatibilityHash: ptrString("hash"), StartedAt: now},
	}
	if err := db.Create(&fixtures).Error; err != nil {
		t.Fatalf("create runtime fixtures: %v", err)
	}

	var ids []string
	if err := applyDurableRuntimeContract(db.Model(&mysql.WorkflowRun{})).Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatalf("query durable eligible runs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "durable" {
		t.Fatalf("durable eligible ids = %v, want [durable]", ids)
	}
}

func TestCutoverTerminalizesSelectedLegacyRunsWithAudit(t *testing.T) {
	db := newP07Database(t, "cutover")
	now := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	activeSelected := "session-selected"
	activeUnselected := "session-unselected"
	validJSON := `{}`
	runs := []mysql.WorkflowRun{
		{ID: "legacy-terminal", WorkflowKey: "test", RuntimeMode: RuntimeModeLegacy, Status: RunStatusSuccess, StartedAt: now},
		{ID: "legacy-selected", WorkflowKey: "test", RuntimeMode: RuntimeModeLegacy, Status: RunStatusRunning, ActiveSessionKey: &activeSelected, LastEventSeq: 2, StartedAt: now},
		{ID: "legacy-unselected", WorkflowKey: "test", RuntimeMode: RuntimeModeLegacy, Status: RunStatusParked, ActiveSessionKey: &activeUnselected, StartedAt: now},
		{ID: "durable", WorkflowKey: "test", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: ptrString("v1"), RuntimeCompatibilityHash: ptrString("hash"), StartedAt: now},
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatalf("create cutover fixtures: %v", err)
	}
	if err := db.Exec(`INSERT INTO workflow_checkpoints
		(id, run_id, checkpoint_key, snapshot_json, created_at)
		VALUES (?, ?, ?, JSON_OBJECT('legacy', true), ?)`, "legacy-checkpoint", "legacy-selected", "legacy", now).Error; err != nil {
		t.Fatalf("create legacy checkpoint: %v", err)
	}

	store := NewGORMStore(db)
	before, err := store.LegacyCutoverStats(context.Background())
	if err != nil {
		t.Fatalf("read pre-cutover stats: %v", err)
	}
	if before.LegacyTerminal != 1 || before.CutoverTerminalized != 0 || before.DurableEligible != 1 {
		t.Fatalf("pre-cutover stats = %+v", before)
	}

	result, err := store.TerminalizeLegacyRuns(context.Background(), []string{"legacy-selected"})
	if err != nil {
		t.Fatalf("terminalize selected legacy run: %v", err)
	}
	if result.Terminalized != 1 {
		t.Fatalf("terminalized = %d, want 1", result.Terminalized)
	}

	var selected mysql.WorkflowRun
	if err := db.First(&selected, "id = ?", "legacy-selected").Error; err != nil {
		t.Fatalf("read selected run: %v", err)
	}
	if selected.Status != RunStatusFailed || selected.ParkReason == nil || *selected.ParkReason != LegacyCutoverParkReason {
		t.Fatalf("selected terminal state = status %q park_reason %v", selected.Status, selected.ParkReason)
	}
	if selected.ActiveSessionKey != nil || selected.FinishedAt == nil || selected.LastEventSeq != 3 {
		t.Fatalf("selected release = active %v finished %v seq %d", selected.ActiveSessionKey, selected.FinishedAt, selected.LastEventSeq)
	}

	var event mysql.WorkflowEvent
	if err := db.First(&event, "run_id = ? AND seq = ?", "legacy-selected", 3).Error; err != nil {
		t.Fatalf("read cutover audit event: %v", err)
	}
	if event.EventType != "run.failed" || event.PayloadVersion != 1 {
		t.Fatalf("audit event = type %q version %d", event.EventType, event.PayloadVersion)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if payload["park_reason"] != LegacyCutoverParkReason || payload["from_status"] != RunStatusRunning || payload["to_status"] != RunStatusFailed {
		t.Fatalf("audit payload = %v", payload)
	}

	var unselected mysql.WorkflowRun
	if err := db.First(&unselected, "id = ?", "legacy-unselected").Error; err != nil {
		t.Fatalf("read unselected run: %v", err)
	}
	if unselected.Status != RunStatusParked || unselected.ActiveSessionKey == nil {
		t.Fatalf("unselected legacy run was changed: status %q active %v", unselected.Status, unselected.ActiveSessionKey)
	}

	var checkpoint struct {
		EinoCheckpointID *string
		CheckpointBlob   []byte
	}
	if err := db.Raw(`SELECT eino_checkpoint_id, checkpoint_blob FROM workflow_checkpoints WHERE id = ?`, "legacy-checkpoint").Scan(&checkpoint).Error; err != nil {
		t.Fatalf("read legacy checkpoint: %v", err)
	}
	if checkpoint.EinoCheckpointID != nil || checkpoint.CheckpointBlob != nil {
		t.Fatalf("legacy checkpoint was converted: id=%v blob=%v", checkpoint.EinoCheckpointID, checkpoint.CheckpointBlob)
	}

	after, err := store.LegacyCutoverStats(context.Background())
	if err != nil {
		t.Fatalf("read post-cutover stats: %v", err)
	}
	if after.LegacyTerminal != 2 || after.CutoverTerminalized != 1 || after.DurableEligible != 1 {
		t.Fatalf("post-cutover stats = %+v", after)
	}
}

func TestCutoverAuditFailureRollsBackTerminalization(t *testing.T) {
	db := newP07Database(t, "cutover_rollback")
	now := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	active := "session-rollback"
	run := mysql.WorkflowRun{ID: "legacy-rollback", WorkflowKey: "test", RuntimeMode: RuntimeModeLegacy, Status: RunStatusRunning, ActiveSessionKey: &active, LastEventSeq: 0, StartedAt: now}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create rollback fixture: %v", err)
	}
	if err := db.Create(&mysql.WorkflowEvent{RunID: run.ID, Seq: 1, EventType: "legacy.existing", Payload: `{}`}).Error; err != nil {
		t.Fatalf("create conflicting event: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_cutover_audit BEFORE INSERT ON workflow_events
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reject cutover audit'`).Error; err != nil {
		t.Fatalf("create audit failure trigger: %v", err)
	}

	_, err := NewGORMStore(db).TerminalizeLegacyRuns(context.Background(), []string{run.ID})
	if err == nil {
		t.Fatal("terminalize succeeded despite audit event conflict")
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("read rolled-back run: %v", err)
	}
	if got.Status != RunStatusRunning || got.ActiveSessionKey == nil || got.FinishedAt != nil || got.LastEventSeq != 0 {
		t.Fatalf("cutover transaction partially committed: %+v", got)
	}
}

func TestActiveSessionContractRetainsEveryNonTerminalState(t *testing.T) {
	for _, status := range []string{
		RunStatusPending,
		RunStatusRunning,
		RunStatusWaitingApproval,
		RunStatusRetryableFailed,
		RunStatusParked,
		RunStatusReconciling,
	} {
		if !RunOccupiesSession(status) {
			t.Errorf("non-terminal status %q released active_session_key", status)
		}
	}
	for _, status := range []string{RunStatusSuccess, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled} {
		if RunOccupiesSession(status) {
			t.Errorf("terminal status %q retained active_session_key", status)
		}
	}
	if !RunOccupiesSession("unknown_future_status") {
		t.Error("unknown status did not fail closed by retaining active_session_key")
	}
}

func TestCutoverRejectsNonLegacyOrUnselectedRuns(t *testing.T) {
	db := newP07Database(t, "cutover_reject")
	now := time.Now().Truncate(time.Millisecond)
	validJSON := `{}`
	durable := mysql.WorkflowRun{ID: "durable-only", WorkflowKey: "test", RuntimeMode: RuntimeModeDurableV1, Status: RunStatusPending, ImmutableInputJSON: &validJSON, RuntimeVersion: ptrString("v1"), RuntimeCompatibilityHash: ptrString("hash"), StartedAt: now}
	if err := db.Create(&durable).Error; err != nil {
		t.Fatalf("create durable fixture: %v", err)
	}
	_, err := NewGORMStore(db).TerminalizeLegacyRuns(context.Background(), []string{durable.ID})
	if !errors.Is(err, ErrLegacyCutoverSelectionMismatch) {
		t.Fatalf("terminalize durable error = %v, want selection mismatch", err)
	}
	var got mysql.WorkflowRun
	if err := db.First(&got, "id = ?", durable.ID).Error; err != nil {
		t.Fatalf("read durable run: %v", err)
	}
	if got.Status != RunStatusPending {
		t.Fatalf("durable run status = %q, want pending", got.Status)
	}
	if !errors.Is(db.First(&mysql.WorkflowEvent{}, "run_id = ?", durable.ID).Error, gorm.ErrRecordNotFound) {
		t.Fatal("cutover wrote an event for a durable run")
	}
}
