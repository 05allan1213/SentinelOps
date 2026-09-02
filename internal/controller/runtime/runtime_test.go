package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apiRuntime "SentinelOps/api/runtime"
	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/workflow"
	service "SentinelOps/internal/service/runtime"
)

func TestRuntimeControllerImplementsFrozenInterface(t *testing.T) {
	var controller apiRuntime.IRuntimeV1 = NewV1(nil)
	_ = controller
}

func TestRuntimeControllerHViewsAreExplicitlyUnavailable(t *testing.T) {
	controller := NewV1(nil)

	capabilities, err := controller.GetCapabilities(context.Background(), &v1.GetCapabilitiesReq{})
	if err != nil || capabilities.Availability != v1.AvailabilityUnavailable || !capabilities.NotRun || capabilities.ReasonCode != "not_observed" {
		t.Fatalf("capabilities=%+v err=%v", capabilities, err)
	}
	safety, err := controller.GetSafety(context.Background(), &v1.GetSafetyReq{})
	if err != nil || safety.Availability != v1.AvailabilityUnavailable || !safety.NotRun || safety.Item.Availability != v1.AvailabilityUnavailable {
		t.Fatalf("safety=%+v err=%v", safety, err)
	}
	worker, err := controller.GetWorkerHealth(context.Background(), &v1.GetWorkerHealthReq{})
	if err != nil || worker.Availability != v1.AvailabilityUnavailable || !worker.NotRun || len(worker.Items) != 0 {
		t.Fatalf("worker=%+v err=%v", worker, err)
	}
	eval, err := controller.GetEval(context.Background(), &v1.GetEvalReq{Page: 1, PageSize: 50, Suite: "runtime"})
	if err != nil || eval.Availability != v1.AvailabilityUnavailable || !eval.NotRun || eval.Item.Suite != "runtime" {
		t.Fatalf("eval=%+v err=%v", eval, err)
	}
	release, err := controller.GetRelease(context.Background(), &v1.GetReleaseReq{})
	if err != nil || release.Availability != v1.AvailabilityUnavailable || !release.NotRun {
		t.Fatalf("release=%+v err=%v", release, err)
	}
	retention, err := controller.GetRetention(context.Background(), &v1.GetRetentionReq{})
	if err != nil || retention.Availability != v1.AvailabilityUnavailable || !retention.NotRun {
		t.Fatalf("retention=%+v err=%v", retention, err)
	}
}

func TestRuntimeControllerH03ViewsUseServiceReadModels(t *testing.T) {
	controller := NewV1(service.NewRuntimeService(nil))

	eval, err := controller.GetEval(context.Background(), &v1.GetEvalReq{Page: 1, PageSize: 50, Suite: "runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Item.Suite != service.EvalSuiteAgent || !eval.Item.NotRun || eval.Item.ReasonCode != "eval_not_executed" {
		t.Fatalf("eval=%+v", eval)
	}

	release, err := controller.GetRelease(context.Background(), &v1.GetReleaseReq{})
	if err != nil {
		t.Fatal(err)
	}
	if release.Item.RuntimeVersion == "" || release.Item.GrayState != nil || release.Item.RollbackState != nil || !release.NotRun || release.ReasonCode != "not_observed" {
		t.Fatalf("release=%+v", release)
	}
}

func TestRuntimeControllerHViewsRejectNilRequests(t *testing.T) {
	controller := NewV1(nil)
	checks := []struct {
		name string
		call func() error
	}{
		{name: "capabilities", call: func() error { _, err := controller.GetCapabilities(context.Background(), nil); return err }},
		{name: "safety", call: func() error { _, err := controller.GetSafety(context.Background(), nil); return err }},
		{name: "worker-health", call: func() error { _, err := controller.GetWorkerHealth(context.Background(), nil); return err }},
		{name: "release", call: func() error { _, err := controller.GetRelease(context.Background(), nil); return err }},
		{name: "retention", call: func() error { _, err := controller.GetRetention(context.Background(), nil); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil || !strings.Contains(err.Error(), service.ErrorCodeRuntimeInvalidFilter) {
				t.Fatalf("error=%v, want stable 400 runtime validation", err)
			}
		})
	}
}

func TestRuntimeControllerTimeRangeIsUTCAndOrdered(t *testing.T) {
	from, to, err := parseTimeRange("2026-09-01T00:00:00+08:00", "2026-09-01T02:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if !from.Equal(time.Date(2026, 8, 31, 16, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("from=%s to=%s", from, to)
	}
	if _, _, err := parseTimeRange("2026-09-01T02:00:00Z", "2026-09-01T01:00:00Z"); !errors.Is(err, v1.ErrRuntimeRequestValidation) {
		t.Fatalf("unordered range error=%v", err)
	}
}

func TestRuntimeControllerRejectsUnsupportedTimelineFilters(t *testing.T) {
	controller := NewV1(nil)
	for name, req := range map[string]*v1.GetTimelineReq{
		"negative attempt": {RunID: "run-1", Page: 1, PageSize: 50, Attempt: -1},
		"unsupported sort": {RunID: "run-1", Page: 1, PageSize: 50, Sort: "attempt"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := controller.GetTimeline(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), service.ErrorCodeRuntimeInvalidFilter) {
				t.Fatalf("GetTimeline() error=%v, want request validation", err)
			}
		})
	}
}

func TestRuntimeControllerUninitializedStoreReturnsStableError(t *testing.T) {
	controller := NewV1(service.NewRuntimeService(nil))
	_, err := controller.ListRuns(context.Background(), &v1.ListRunsReq{})
	if err == nil || !strings.Contains(err.Error(), service.ErrorCodeRuntimeInternal) {
		t.Fatalf("ListRuns() error=%v, want stable internal runtime error", err)
	}
}

func TestRuntimeControllerErrorDoesNotExposeUnderlyingMessage(t *testing.T) {
	controller := NewV1(nil)
	err := controller.fail(context.Background(), errors.New("password=super-secret"))
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), service.ErrorCodeRuntimeInternal) {
		t.Fatalf("error=%v", err)
	}
}

func TestRuntimeControllerOperationMappingPreservesTerminalFacts(t *testing.T) {
	now := time.Now().UTC()
	op := workflow.Operation{
		OperationID: "op-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		RunID:       "run-1", Action: workflow.OperationActionReplay,
		Status: workflow.OperationStatusSucceeded, Terminal: true,
		AcceptedAt: now, FinishedAt: &now, ResultReason: "completed", CorrelationSeq: 7,
	}
	dto := mapOperation(op, false)
	if dto.Status != v1.OperationStatusSucceeded || !dto.Terminal || dto.CorrelationSeq != 7 || dto.Reason != "completed" {
		t.Fatalf("dto=%+v", dto)
	}
	if dto.Availability != v1.AvailabilityAvailable || dto.DataQuality != v1.DataQualityComplete {
		t.Fatalf("dto metadata=%+v", dto.ResourceMeta)
	}

	dto = mapOperation(op, true)
	if !dto.IdempotentReplay {
		t.Fatal("idempotent replay marker was lost")
	}
}

func TestRuntimeControllerSSEEventTypeIsHeaderSafe(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "effect.succeeded", want: "effect.succeeded"},
		{value: "operation/failed", want: "runtime.event"},
		{value: "", want: "runtime.event"},
	} {
		if got := safeSSEEventType(test.value); got != test.want {
			t.Fatalf("safeSSEEventType(%q)=%q want %q", test.value, got, test.want)
		}
	}
}
