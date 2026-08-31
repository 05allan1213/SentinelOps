package v1

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimeEnumsRejectUnknownValues(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"availability", Availability("not_observed").Valid()},
		{"data quality", DataQuality("excellent").Valid()},
		{"runtime status", RuntimeStatus("success").Valid()},
		{"current phase", CurrentPhase("done").Valid()},
		{"recovery action", RecoveryAction("unlock").Valid()},
		{"operation status", OperationStatus("complete").Valid()},
		{"sort direction", SortDirection("sideways").Valid()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				t.Fatal("unknown enum value was accepted")
			}
		})
	}
	if !AvailabilityAvailable.Valid() || !DataQualityComplete.Valid() || !RuntimeStatusSucceeded.Valid() ||
		!CurrentPhaseCompleted.Valid() || !RecoveryActionResume.Valid() || !OperationStatusAccepted.Valid() || !SortDirectionAsc.Valid() {
		t.Fatal("a documented enum value was rejected")
	}
}

func TestRuntimeRequestValidation(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	valid := []interface{ Valid() error }{
		ListRunsReq{Page: 1, PageSize: 100, Sort: "started_at", Direction: SortDirectionDesc, From: now},
		GetTimelineReq{RunID: "run-1", Page: 1, PageSize: 20, Sort: "created_at", Direction: SortDirectionAsc, EventTypes: []string{"run.created"}},
		RunEventsReq{RunID: "run-1", AfterSeq: 0}, GetAttemptsReq{RunID: "run-1", Page: 1, PageSize: 20},
		ExpandEvidenceReq{RunID: "run-1", EvidenceID: "ev-1", Include: "quote"},
		GetContextReq{RunID: "run-1", Include: "history"},
		RecoverRunReq{RunID: "run-1", Action: RecoveryActionResume, IdempotencyKey: "key", Reason: "operator request"},
	}
	for _, req := range valid {
		if err := req.Valid(); err != nil {
			t.Errorf("valid %T rejected: %v", req, err)
		}
	}
	invalid := []interface{ Valid() error }{
		ListRunsReq{Page: 0, PageSize: 101}, GetTimelineReq{RunID: "run-1", Page: 1, PageSize: 20, EventTypes: []string{"raw.provider"}},
		RunEventsReq{RunID: "run-1", AfterSeq: -1}, GetCheckpointsReq{RunID: "", Page: 1, PageSize: 20},
		ExpandEvidenceReq{RunID: "run-1", EvidenceID: "ev-1", Include: "content"},
		GetContextReq{RunID: "run-1", Include: "prompt"}, RecoverRunReq{RunID: "run-1", Action: "unlock", IdempotencyKey: "key", Reason: "reason"},
	}
	for _, req := range invalid {
		if err := req.Valid(); err == nil {
			t.Errorf("invalid %T accepted", req)
		}
	}
}

func TestParseRFC3339UTCNormalizesOffset(t *testing.T) {
	got, err := ParseRFC3339UTC("2026-08-31T12:00:00+08:00")
	if err != nil {
		t.Fatalf("ParseRFC3339UTC() error = %v", err)
	}
	if got == nil || got.Format(time.RFC3339) != "2026-08-31T04:00:00Z" {
		t.Fatalf("normalized timestamp = %v", got)
	}
	if _, err := ParseRFC3339UTC("2026-08-31 12:00:00"); err == nil {
		t.Fatal("invalid timestamp accepted")
	}
}

func TestRuntimeRoutesUseV1Namespace(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(ListRunsReq{}), reflect.TypeOf(GetRunReq{}), reflect.TypeOf(GetTimelineReq{}),
		reflect.TypeOf(RunEventsReq{}), reflect.TypeOf(GetAttemptsReq{}), reflect.TypeOf(GetCheckpointsReq{}),
		reflect.TypeOf(GetApprovalsReq{}), reflect.TypeOf(GetEffectsReq{}), reflect.TypeOf(GetEvidenceReq{}),
		reflect.TypeOf(ExpandEvidenceReq{}), reflect.TypeOf(GetContextReq{}), reflect.TypeOf(GetTracesReq{}),
		reflect.TypeOf(RecoverRunReq{}), reflect.TypeOf(GetOperationReq{}), reflect.TypeOf(GetCapabilitiesReq{}),
		reflect.TypeOf(GetSafetyReq{}), reflect.TypeOf(GetWorkerHealthReq{}), reflect.TypeOf(GetEvalReq{}),
		reflect.TypeOf(GetReleaseReq{}), reflect.TypeOf(GetRetentionReq{}),
	}
	for _, typ := range types {
		meta, ok := typ.FieldByName("Meta")
		if !ok {
			t.Fatalf("%s has no g.Meta", typ.Name())
		}
		path := meta.Tag.Get("path")
		if !strings.HasPrefix(path, "/runtime/v1/") {
			t.Errorf("%s path %q is outside /runtime/v1", typ.Name(), path)
		}
	}
}

func TestRuntimeDTOJSONNames(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(ResourceMeta{}), reflect.TypeOf(PageMeta{}), reflect.TypeOf(RunSummaryDTO{}),
		reflect.TypeOf(RunDetailDTO{}), reflect.TypeOf(RuntimeEventDTO{}), reflect.TypeOf(AttemptDTO{}),
		reflect.TypeOf(CheckpointDTO{}), reflect.TypeOf(ApprovalDTO{}), reflect.TypeOf(EffectDTO{}),
		reflect.TypeOf(EvidenceDTO{}), reflect.TypeOf(ContextDTO{}), reflect.TypeOf(TraceAggregateDTO{}),
		reflect.TypeOf(OperationDTO{}), reflect.TypeOf(CapabilityDTO{}), reflect.TypeOf(SafetyDTO{}),
	}
	for _, typ := range types {
		assertSnakeCaseJSONTags(t, typ)
	}
}

func TestRuntimeDTOHasNoOpaqueOrSecretFields(t *testing.T) {
	forbidden := []string{"checkpoint_blob", "authorization", "secret", "provider_response", "prompt_text", "tool_arguments"}
	types := []reflect.Type{
		reflect.TypeOf(ListRunsRes{}), reflect.TypeOf(GetRunRes{}), reflect.TypeOf(TimelineRes{}),
		reflect.TypeOf(AttemptsRes{}), reflect.TypeOf(CheckpointsRes{}), reflect.TypeOf(ApprovalsRes{}),
		reflect.TypeOf(EffectsRes{}), reflect.TypeOf(EvidenceRes{}), reflect.TypeOf(GetContextRes{}),
		reflect.TypeOf(TracesRes{}), reflect.TypeOf(OperationAcceptedRes{}), reflect.TypeOf(GetOperationRes{}),
		reflect.TypeOf(CapabilitiesRes{}), reflect.TypeOf(SafetyRes{}), reflect.TypeOf(WorkerHealthRes{}),
		reflect.TypeOf(EvalRes{}), reflect.TypeOf(ReleaseRes{}), reflect.TypeOf(RetentionRes{}),
	}
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			for _, banned := range forbidden {
				if strings.Contains(strings.ToLower(tag), banned) {
					t.Errorf("%s.%s exposes forbidden JSON field %q", typ.Name(), field.Name, tag)
				}
			}
			walk(field.Type)
		}
	}
	for _, typ := range types {
		walk(typ)
	}
}

func assertSnakeCaseJSONTags(t *testing.T, typ reflect.Type) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous || field.Tag.Get("json") == "" || field.Tag.Get("json") == "-" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != strings.ToLower(name) || strings.Contains(name, "-") {
			t.Errorf("%s.%s has non-snake-case JSON tag %q", typ.Name(), field.Name, name)
		}
	}
}
