package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"SentinelOps/internal/dao/mysql"
)

func TestOperationIdentityStableAndKeyOpaque(t *testing.T) {
	vectorID, vectorDigest, err := OperationIdentity("run-vector-20260831", "idempotency-key-vector-0001")
	if err != nil {
		t.Fatalf("hard-coded vector: %v", err)
	}
	if vectorID != "op-8e25d05df3fcebdfe0801bc8388c3e72c5c2cdfa287dfcf4441081295178a697" || vectorDigest != "184ee2106447891a7297e366a984aea0eea17354af7d4018fe1094da242b6446" {
		t.Fatalf("hard-coded vector = %q/%q", vectorID, vectorDigest)
	}

	runID := "run-operation-identity"
	key := "opaque-client-key-0001"
	identity, digest, err := OperationIdentity(runID, key)
	if err != nil {
		t.Fatalf("operation identity: %v", err)
	}
	inner := sha256.Sum256([]byte(key))
	domainInput := append([]byte(OperationIdentityDomain+"\x00"+runID+"\x00"), inner[:]...)
	wantOuter := sha256.Sum256(domainInput)
	wantID := "op-" + hex.EncodeToString(wantOuter[:])
	if identity != wantID || digest != hex.EncodeToString(inner[:]) {
		t.Fatalf("identity/digest = %q/%q, want %q/%q", identity, digest, wantID, hex.EncodeToString(inner[:]))
	}
	again, _, err := OperationIdentity(runID, key)
	if err != nil || again != identity {
		t.Fatalf("stable identity = %q err=%v, want %q", again, err, identity)
	}
	other, _, err := OperationIdentity(runID, "opaque-client-key-0002")
	if err != nil || other == identity {
		t.Fatalf("other identity = %q err=%v", other, err)
	}
	if strings.Contains(identity, key) || strings.Contains(digest, key) {
		t.Fatal("raw key leaked into derived identity")
	}
	for _, invalid := range []string{"short", strings.Repeat("x", 129)} {
		if _, _, err := OperationIdentity(runID, invalid); !errors.Is(err, ErrInvalidOperationInput) {
			t.Fatalf("invalid key length error = %v", err)
		}
	}
}

func TestOperationFingerprintChangesOnMaterialInput(t *testing.T) {
	base := OperationRequestInput{
		RunID: "run-operation-fingerprint", Action: OperationActionResume,
		Reason: "recover after operator review", ExpectedGeneration: 7,
		ExpectedCompatibilityHash: strings.Repeat("a", 64),
	}
	fingerprint, err := OperationRequestFingerprint(base)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	canonical := `{"action":"resume","expected_compatibility_hash":"` + strings.Repeat("a", 64) + `","expected_generation":7,"reason":"recover after operator review","run_id":"run-operation-fingerprint"}`
	want := sha256.Sum256([]byte(canonical))
	if fingerprint != hex.EncodeToString(want[:]) {
		t.Fatalf("fingerprint = %q, want %q", fingerprint, hex.EncodeToString(want[:]))
	}
	variants := []OperationRequestInput{base, base, base, base, base}
	variants[0].RunID += "-other"
	variants[1].Action = OperationActionRestore
	variants[2].Reason += "."
	variants[3].ExpectedGeneration++
	variants[4].ExpectedCompatibilityHash = strings.Repeat("b", 64)
	for index, variant := range variants {
		got, err := OperationRequestFingerprint(variant)
		if err != nil {
			t.Fatalf("variant %d: %v", index, err)
		}
		if got == fingerprint {
			t.Fatalf("variant %d did not change fingerprint", index)
		}
	}
	invalid := base
	invalid.ExpectedGeneration = 0
	if _, err := OperationRequestFingerprint(invalid); !errors.Is(err, ErrInvalidOperationInput) {
		t.Fatalf("zero generation error = %v", err)
	}
	invalid = base
	invalid.ExpectedCompatibilityHash = strings.Repeat("A", 64)
	if _, err := OperationRequestFingerprint(invalid); !errors.Is(err, ErrInvalidOperationInput) {
		t.Fatalf("uppercase compatibility error = %v", err)
	}
	cancel := base
	cancel.Action = OperationActionCancel
	cancel.ExpectedGeneration = 0
	cancel.ExpectedCompatibilityHash = ""
	if _, err := OperationRequestFingerprint(cancel); err != nil {
		t.Fatalf("cancel fingerprint: %v", err)
	}
	blankReason := base
	blankReason.Reason = " \t\n "
	if _, err := OperationRequestFingerprint(blankReason); !errors.Is(err, ErrInvalidOperationInput) {
		t.Fatalf("blank reason error = %v", err)
	}
}

func TestOperationEventTransitionMatrix(t *testing.T) {
	valid := [][]string{
		{EventOperationAccepted},
		{EventOperationAccepted, EventOperationRejected},
		{EventOperationAccepted, EventOperationStarted},
		{EventOperationAccepted, EventOperationStarted, EventOperationSucceeded},
		{EventOperationAccepted, EventOperationStarted, EventOperationFailed},
		{EventOperationAccepted, EventOperationStarted, EventOperationCanceled},
	}
	for _, sequence := range valid {
		events := fixtureOperationRows("run-operation-state", "op-state", OperationActionResume, sequence...)
		operation, err := DeriveOperation(events)
		if err != nil {
			t.Fatalf("valid sequence %v: %v", sequence, err)
		}
		if operation.Status != operationStatusForEvent(sequence[len(sequence)-1]) {
			t.Fatalf("sequence %v status = %q", sequence, operation.Status)
		}
	}
	invalid := []struct {
		name   string
		events []mysql.WorkflowEvent
	}{
		{"missing accepted", fixtureOperationRows("run-operation-state", "op-state", OperationActionResume, EventOperationStarted)},
		{"skips started", fixtureOperationRows("run-operation-state", "op-state", OperationActionResume, EventOperationAccepted, EventOperationSucceeded)},
		{"after terminal", fixtureOperationRows("run-operation-state", "op-state", OperationActionResume, EventOperationAccepted, EventOperationRejected, EventOperationStarted)},
		{"cross run", append(fixtureOperationRows("run-operation-state", "op-state", OperationActionResume, EventOperationAccepted), fixtureOperationRows("run-other", "op-state", OperationActionResume, EventOperationStarted)[0])},
		{"cross action", append(fixtureOperationRows("run-operation-state", "op-state", OperationActionResume, EventOperationAccepted), fixtureOperationRows("run-operation-state", "op-state", OperationActionReplay, EventOperationStarted)[0])},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DeriveOperation(test.events); !errors.Is(err, ErrInvalidOperationTransition) {
				t.Fatalf("derive error = %v", err)
			}
		})
	}
}

func TestOperationEventMetadataIsRedacted(t *testing.T) {
	db := newP07Database(t, "operation_metadata")
	store, ctx, run := fixture08CreateRun(t, db, "operation-metadata")
	key := "opaque-operation-key-1234"
	operationID, digest, err := OperationIdentity(run.ID, key)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	request := OperationRequestInput{RunID: run.ID, Action: OperationActionResume, Reason: "Authorization: Bearer raw-secret", ExpectedGeneration: 1, ExpectedCompatibilityHash: strings.Repeat("a", 64)}
	fingerprint, err := OperationRequestFingerprint(request)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	accepted, err := (OperationEventInput{Type: EventOperationAccepted, OperationID: operationID, Action: request.Action, IdempotencyKeyDigest: digest, RequestFingerprint: fingerprint, ActorID: "admin", Reason: request.Reason}).WorkflowEventInput()
	if err != nil {
		t.Fatalf("accepted input: %v", err)
	}
	lease := fixture08CurrentLease(t, db, run.ID)
	if err := store.TransitionRunWithEvent(ctx, RunTransition{RunID: run.ID, ExpectedStatus: RunStatusPending, TargetStatus: RunStatusRunning, Lease: lease, Event: accepted}); err != nil {
		t.Fatalf("append accepted: %v", err)
	}
	started, err := (OperationEventInput{Type: EventOperationStarted, OperationID: operationID, Action: request.Action, CorrelationSeq: 1}).WorkflowEventInput()
	if err != nil {
		t.Fatalf("started input: %v", err)
	}
	if err := store.AppendRunEvent(ctx, lease, started); err != nil {
		t.Fatalf("append started: %v", err)
	}

	var rows []mysql.WorkflowEvent
	if err := db.Where("operation_id = ?", operationID).Order("seq").Find(&rows).Error; err != nil {
		t.Fatalf("load operation rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("operation row count = %d", len(rows))
	}
	acceptedRow, startedRow := rows[0], rows[1]
	if acceptedRow.IdempotencyKeyDigest == nil || *acceptedRow.IdempotencyKeyDigest != digest || acceptedRow.RequestFingerprint == nil || *acceptedRow.RequestFingerprint != fingerprint {
		t.Fatalf("accepted identity metadata = %#v", acceptedRow)
	}
	if acceptedRow.ReasonRedacted == nil || strings.Contains(*acceptedRow.ReasonRedacted, "raw-secret") || !strings.Contains(*acceptedRow.ReasonRedacted, "[REDACTED]") {
		t.Fatalf("accepted reason = %v", acceptedRow.ReasonRedacted)
	}
	if startedRow.IdempotencyKeyDigest != nil || startedRow.RequestFingerprint != nil || startedRow.ActorID != nil || startedRow.ReasonRedacted != nil {
		t.Fatalf("started carried acceptance metadata: %#v", startedRow)
	}
	for _, row := range rows {
		if strings.Contains(row.Payload, key) || strings.Contains(row.Payload, "raw-secret") {
			t.Fatalf("unsafe operation payload = %s", row.Payload)
		}
		if !strings.Contains(row.Payload, operationID) {
			t.Fatalf("operation_id missing from safe payload = %s", row.Payload)
		}
	}

	loaded, err := store.LoadOperation(ctx, operationID)
	if err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if loaded.Status != OperationStatusRunning || loaded.AcceptedSeq != 2 || loaded.StartedSeq != 3 {
		t.Fatalf("loaded operation = %#v", loaded)
	}
	active, err := store.ListActiveOperationsForRun(ctx, run.ID)
	if err != nil || len(active) != 1 || active[0].OperationID != operationID {
		t.Fatalf("active operations = %#v err=%v", active, err)
	}
}

func TestOperationCorrelationTracksLifecycleRunEvent(t *testing.T) {
	rows := fixtureOperationRows("run-operation-correlation", "op-correlation", OperationActionResume,
		EventOperationAccepted, EventOperationStarted, EventOperationSucceeded)
	rows[0].Seq = 10
	rows[1].Seq = 20
	rows[2].Seq = 30
	startedCorrelation, terminalCorrelation := uint64(7), uint64(29)
	rows[1].CorrelationSeq = &startedCorrelation
	rows[2].CorrelationSeq = &terminalCorrelation
	operation, err := DeriveOperation(rows)
	if err != nil {
		t.Fatalf("derive correlated lifecycle: %v", err)
	}
	if operation.CorrelationSeq != terminalCorrelation {
		t.Fatalf("terminal correlation = %d, want %d", operation.CorrelationSeq, terminalCorrelation)
	}
	invalid := append([]mysql.WorkflowEvent(nil), rows...)
	notEarlier := invalid[2].Seq
	invalid[2].CorrelationSeq = &notEarlier
	if _, err := DeriveOperation(invalid); !errors.Is(err, ErrInvalidOperationTransition) {
		t.Fatalf("non-earlier terminal correlation error = %v", err)
	}
}

func TestLegacyEventInsertUnaffected(t *testing.T) {
	db := newP07Database(t, "operation_legacy")
	store, ctx, run := fixture08CreateRun(t, db, "operation-legacy")
	lease := fixture08CurrentLease(t, db, run.ID)
	if err := store.TransitionRunWithEvent(ctx, RunTransition{RunID: run.ID, ExpectedStatus: RunStatusPending, TargetStatus: RunStatusRunning, Lease: lease, Event: WorkflowEventInput{Type: EventRunClaimed}}); err != nil {
		t.Fatalf("claim legacy run: %v", err)
	}
	if err := store.AppendRunEvent(ctx, lease, WorkflowEventInput{Type: EventAgentPlan, Payload: EventPayload{Summary: "legacy event"}}); err != nil {
		t.Fatalf("append legacy event: %v", err)
	}
	var row mysql.WorkflowEvent
	if err := db.Where("run_id = ? AND event_type = ?", run.ID, EventAgentPlan).First(&row).Error; err != nil {
		t.Fatalf("read legacy event: %v", err)
	}
	if row.OperationID != nil || row.CommandAction != nil || row.IdempotencyKeyDigest != nil || row.RequestFingerprint != nil || row.ActorID != nil || row.ReasonRedacted != nil || row.CorrelationSeq != nil {
		t.Fatalf("legacy event unexpectedly has operation metadata: %#v", row)
	}
}

func TestOperationMetadataPairingRejectsSmuggling(t *testing.T) {
	validID := "op-" + strings.Repeat("a", 64)
	validDigest := strings.Repeat("b", 64)
	validFingerprint := strings.Repeat("c", 64)
	for _, test := range []struct {
		name  string
		input WorkflowEventInput
	}{
		{"operation without metadata", WorkflowEventInput{Type: EventOperationStarted}},
		{"legacy with metadata", WorkflowEventInput{Type: EventAgentPlan, Operation: &OperationEventMetadata{OperationID: validID}}},
		{"accepted missing identity", WorkflowEventInput{Type: EventOperationAccepted, Operation: &OperationEventMetadata{OperationID: validID, CommandAction: OperationActionResume}}},
		{"started carries digest", WorkflowEventInput{Type: EventOperationStarted, Operation: &OperationEventMetadata{OperationID: validID, CommandAction: OperationActionResume, IdempotencyKeyDigest: validDigest, CorrelationSeq: 1}}},
		{"invalid operation id", WorkflowEventInput{Type: EventOperationAccepted, Operation: &OperationEventMetadata{OperationID: "op-bad", CommandAction: OperationActionResume, IdempotencyKeyDigest: validDigest, RequestFingerprint: validFingerprint, ActorID: "actor", Reason: "reason"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := marshalDurableEvent(test.input); !errors.Is(err, ErrInvalidOperationInput) {
				t.Fatalf("marshal error = %v", err)
			}
		})
	}
}

func fixtureOperationRows(runID, operationID, action string, eventTypes ...string) []mysql.WorkflowEvent {
	if !operationIDPattern.MatchString(operationID) {
		operationID = "op-" + strings.Repeat("a", 64)
	}
	rows := make([]mysql.WorkflowEvent, 0, len(eventTypes))
	for index, eventType := range eventTypes {
		opID, commandAction := operationID, action
		row := mysql.WorkflowEvent{RunID: runID, Seq: uint64(index + 1), EventType: eventType, OperationID: &opID, CommandAction: &commandAction}
		if eventType == EventOperationAccepted {
			digest, fingerprint := strings.Repeat("b", 64), strings.Repeat("c", 64)
			actor, reason := "actor", "accepted"
			row.IdempotencyKeyDigest, row.RequestFingerprint, row.ActorID, row.ReasonRedacted = &digest, &fingerprint, &actor, &reason
		} else {
			correlation := uint64(index)
			row.CorrelationSeq = &correlation
		}
		rows = append(rows, row)
	}
	return rows
}

func operationStatusForEvent(eventType string) string {
	if eventType == EventOperationStarted {
		return OperationStatusRunning
	}
	return strings.TrimPrefix(eventType, "operation.")
}
