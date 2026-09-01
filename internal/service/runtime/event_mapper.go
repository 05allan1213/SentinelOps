package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

type StatusProjection struct {
	Status      v1.RuntimeStatus
	RuntimeMode string
	DataQuality v1.DataQuality
	Known       bool
}

type RunFacts struct {
	Status      string
	RuntimeMode string
}

type AttemptFacts struct {
	Active          bool
	Recovery        bool
	Mode            string
	OperationActive bool
}

type EventFacts struct{ Type string }

// MapCanonicalStatus keeps unknown values invalid instead of fabricating active or success semantics.
func MapCanonicalStatus(raw, runtimeMode string) StatusProjection {
	result := StatusProjection{RuntimeMode: runtimeMode, DataQuality: v1.DataQualityComplete}
	switch raw {
	case workflow.RunStatusPending, workflow.RunStatusRunning, workflow.RunStatusWaitingApproval,
		workflow.RunStatusRetryableFailed, workflow.RunStatusParked, workflow.RunStatusReconciling,
		workflow.RunStatusSucceeded, workflow.RunStatusFailed, workflow.RunStatusCanceled:
		result.Status, result.Known = v1.RuntimeStatus(raw), true
		return result
	case workflow.RunStatusSuccess:
		if runtimeMode == workflow.RuntimeModeLegacy {
			result.Status, result.DataQuality, result.Known = v1.RuntimeStatusSucceeded, v1.DataQualityPartial, true
		} else {
			result.DataQuality = v1.DataQualityUnknown
		}
	default:
		result.DataQuality = v1.DataQualityUnknown
	}
	return result
}

// CurrentPhaseFromFacts is the only public phase calculator; zero facts produce unknown.
func CurrentPhaseFromFacts(run RunFacts, latestAttempt AttemptFacts, latestEvent EventFacts) v1.CurrentPhase {
	if !MapCanonicalStatus(run.Status, run.RuntimeMode).Known {
		return v1.CurrentPhaseUnknown
	}
	if run.Status == workflow.RunStatusSucceeded || run.Status == workflow.RunStatusSuccess && run.RuntimeMode == workflow.RuntimeModeLegacy {
		return v1.CurrentPhaseCompleted
	}
	if run.Status == workflow.RunStatusFailed || run.Status == workflow.RunStatusCanceled {
		return v1.CurrentPhaseFailed
	}
	if latestAttempt.OperationActive || latestAttempt.Active && (latestAttempt.Recovery || isRecoveryMode(latestAttempt.Mode)) {
		return v1.CurrentPhaseRecovering
	}
	if run.Status == workflow.RunStatusWaitingApproval || latestEvent.Type == workflow.EventApprovalPreparing || latestEvent.Type == workflow.EventApprovalRequested {
		return v1.CurrentPhaseWaitingApproval
	}
	if run.Status == workflow.RunStatusReconciling || latestEvent.Type == workflow.EventRunReconciling || latestEvent.Type == workflow.EventEffectReconciling {
		return v1.CurrentPhaseReconciling
	}
	if latestEvent.Type == workflow.EventAgentPlan || latestEvent.Type == workflow.EventAgentReplan {
		return v1.CurrentPhasePlanning
	}
	if run.Status == workflow.RunStatusRunning && isExecutionEvent(latestEvent.Type) {
		return v1.CurrentPhaseExecuting
	}
	return v1.CurrentPhaseUnknown
}

func isRecoveryMode(mode string) bool {
	switch mode {
	case "resume", "replay", "restore", "recovery":
		return true
	default:
		return false
	}
}

func isExecutionEvent(eventType string) bool {
	switch eventType {
	case workflow.EventAgentToolCall, workflow.EventAgentToolResult,
		workflow.EventEffectStarted, workflow.EventEffectSucceeded, workflow.EventEffectFailed,
		workflow.EventEffectUnknown, workflow.EventEffectResolved:
		return true
	default:
		return false
	}
}

type ProjectionState struct {
	Available     bool
	Complete      bool
	Reconstructed bool
	NotRun        bool
	ReasonCode    string
}

// MapResourceMeta keeps caller/auth/validation errors as errors; optional source failures are explicit partial metadata.
func MapResourceMeta(sourceErr error, state ProjectionState) (v1.ResourceMeta, error) {
	if sourceErr != nil {
		if errors.Is(sourceErr, ErrRuntimeNotFound) || errors.Is(sourceErr, gorm.ErrRecordNotFound) {
			return v1.ResourceMeta{}, ErrRuntimeNotFound
		}
		if errors.Is(sourceErr, ErrRuntimeForbidden) || errors.Is(sourceErr, ErrRuntimeContentExpansionDenied) || errors.Is(sourceErr, policy.ErrForbidden) || errors.Is(sourceErr, policy.ErrUnauthenticated) || errors.Is(sourceErr, ErrRuntimeInvalidFilter) || errors.Is(sourceErr, v1.ErrRuntimeRequestValidation) {
			return v1.ResourceMeta{}, sourceErr
		}
		if errors.Is(sourceErr, ErrRuntimeOperationConflict) || errors.Is(sourceErr, ErrRuntimeIdempotencyConflict) || errors.Is(sourceErr, ErrRuntimePrecondition) {
			return v1.ResourceMeta{}, sourceErr
		}
		state.Complete = false
		if state.ReasonCode == "" {
			state.ReasonCode = "source_error"
		}
	}
	meta := v1.ResourceMeta{ReasonCode: state.ReasonCode, NotRun: state.NotRun}
	if !state.Available {
		meta.Availability, meta.DataQuality = v1.AvailabilityUnavailable, v1.DataQualityUnknown
		return meta, nil
	}
	meta.Availability = v1.AvailabilityPartial
	switch {
	case state.Complete:
		meta.Availability, meta.DataQuality = v1.AvailabilityAvailable, v1.DataQualityComplete
	case state.Reconstructed:
		meta.DataQuality = v1.DataQualityReconstructed
	default:
		meta.DataQuality = v1.DataQualityPartial
	}
	return meta, nil
}

type HTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e HTTPError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// RuntimeHTTPError emits only stable messages and never returns err.Error().
func RuntimeHTTPError(err error) HTTPError {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrRuntimeNotFound
	} else if errors.Is(err, policy.ErrForbidden) || errors.Is(err, policy.ErrUnauthenticated) {
		err = ErrRuntimeForbidden
	}
	code := RuntimeErrorCode(err)
	status, message := 500, "internal runtime failure"
	switch code {
	case ErrorCodeRuntimeNotFound:
		status, message = 404, "runtime resource not found"
	case ErrorCodeRuntimeForbidden:
		status, message = 403, "runtime access forbidden"
	case ErrorCodeRuntimeOperationConflict:
		status, message = 409, "runtime operation conflict"
	case ErrorCodeRuntimeIdempotencyConflict:
		status, message = 409, "runtime idempotency conflict"
	case ErrorCodeRuntimePreconditionFailed:
		status, message = 422, "runtime precondition failed"
	case ErrorCodeRuntimeInvalidFilter:
		status, message = 400, "runtime filter is invalid"
	}
	return HTTPError{Status: status, Code: code, Message: message}
}

// MapRuntimeEvent maps every stored row to the frozen v1 DTO, even for unknown or malformed payloads.
func MapRuntimeEvent(event mysql.WorkflowEvent) v1.RuntimeEventDTO {
	redactor := policy.NewRedactor()
	result := v1.RuntimeEventDTO{
		Seq: event.Seq, RunID: event.RunID, EventType: event.EventType,
		TraceID: redactor.RedactText(event.TraceID), Attributes: map[string]string{}, CreatedAt: event.CreatedAt,
		ResourceMeta: v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete},
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(event.Payload), &root); err != nil || root == nil {
		result.ResourceMeta = partialEventMeta("malformed_event_payload")
		return result
	}
	data, canonical := root["data"].(map[string]any)
	if canonical {
		if schema, _ := root["schema"].(string); schema != workflow.EventEnvelopeSchema {
			result.ResourceMeta = partialEventMeta("unknown_event_schema")
		}
		result.Summary, _ = root["summary"].(string)
		result.Reference, _ = root["reference"].(string)
	} else {
		data = root
		result.ResourceMeta = partialEventMeta("legacy_event_payload")
	}
	if event.PayloadVersion != 0 && event.PayloadVersion != workflow.EventPayloadVersion {
		result.ResourceMeta = partialEventMeta("unknown_payload_version")
	}
	if !catalogContains(event.EventType) {
		result.ResourceMeta = partialEventMeta("unknown_event_type")
	}
	result.Summary = redactor.RedactText(result.Summary)
	result.Reference = redactor.RedactText(result.Reference)
	for key, value := range data {
		if eventAttributeAllowed(key) {
			if text, ok := safeAttributeString(redactor, key, value); ok {
				result.Attributes[key] = redactor.RedactText(text)
			} else {
				result.ResourceMeta = partialEventMeta("invalid_event_attribute")
			}
		}
	}
	if value, exists := data["attempt"]; exists {
		var valid bool
		result.Attempt, valid = nonnegativeInt(value)
		if !valid {
			result.ResourceMeta = partialEventMeta("invalid_event_correlation")
		}
	}
	if value, exists := firstExisting(data, "generation", "lease_generation"); exists {
		var valid bool
		result.Generation, valid = nonnegativeUint64(value)
		if !valid {
			result.ResourceMeta = partialEventMeta("invalid_event_correlation")
		}
	}
	if operationID, exists := data["operation_id"]; exists {
		value, valid := operationID.(string)
		if !valid || redactor.RedactText(value) != value {
			result.ResourceMeta = partialEventMeta("invalid_event_correlation")
		} else {
			result.OperationID = value
		}
	}
	if result.OperationID == "" && event.OperationID != nil {
		if redactor.RedactText(*event.OperationID) != *event.OperationID {
			result.ResourceMeta = partialEventMeta("invalid_event_correlation")
		} else {
			result.OperationID = *event.OperationID
		}
	}
	if event.CommandAction != nil && result.Attributes["operation_id"] == "" {
		result.Attributes["command_action"] = redactor.RedactText(*event.CommandAction)
	}
	return result
}

func partialEventMeta(reason string) v1.ResourceMeta {
	return v1.ResourceMeta{Availability: v1.AvailabilityPartial, DataQuality: v1.DataQualityUnknown, ReasonCode: reason}
}

func eventAttributeAllowed(key string) bool {
	switch strings.ToLower(key) {
	case "phase", "status", "reason", "reason_code", "cause", "source", "effect_id", "resolution", "resolved_by", "park_reason", "approval_id", "proposal_hash", "risk_level", "interrupt_id", "trace_quality", "tool_name", "executor", "query_hash", "attempt", "generation", "lease_generation", "execution_generation", "operation_id", "mode", "agent_name", "step_count", "retryable",
		"checkpoint_id", "checkpoint_key", "checkpoint_payload_sha256", "checkpoint_lease_generation",
		"effect_role", "effect_step", "effect_type", "parent_effect_id", "external_reference",
		"decision", "decision_reason", "kind", "state", "usage_quality", "runtime_version", "runtime_compatibility_hash",
		"tool_revision", "tool_schema_hash", "evidence_ids", "count", "duration", "duration_ms", "tokens", "input_tokens", "cached_input_tokens", "output_tokens", "reasoning_tokens", "cost_cny", "result_chars", "result_bytes", "documents", "context_chars", "budget_limit", "budget_used", "budget_remaining", "outcome",
		"subject", "reservation_identity", "metadata", "estimate", "actual":
		return true
	default:
		return false
	}
}

func catalogContains(eventType string) bool {
	for _, value := range workflow.VersionedEventCatalog() {
		if value == eventType {
			return true
		}
	}
	return false
}

func firstExisting(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func safeAttributeString(redactor policy.Redactor, key string, value any) (string, bool) {
	if key == "metadata" || key == "estimate" || key == "actual" {
		return safeBudgetObject(redactor, key, value)
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	case float64, bool:
		return fmt.Sprint(typed), true
	case []any:
		for _, item := range typed {
			switch item.(type) {
			case string, float64, bool, nil:
			default:
				return "", false
			}
		}
		redacted, err := redactor.Redact(typed)
		if err != nil {
			return "", false
		}
		encoded, err := json.Marshal(redacted)
		return string(encoded), err == nil
	default:
		return "", false
	}
}

func safeBudgetObject(redactor policy.Redactor, kind string, value any) (string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	filtered := make(map[string]any)
	for key, item := range object {
		if !budgetObjectFieldAllowed(kind, key) {
			continue
		}
		switch item.(type) {
		case string, float64, bool, nil:
			filtered[key] = item
		default:
			return "", false
		}
	}
	redacted, err := redactor.Redact(filtered)
	if err != nil {
		return "", false
	}
	encoded, err := policy.CanonicalJSON(redacted)
	return string(encoded), err == nil
}

func budgetObjectFieldAllowed(kind, key string) bool {
	switch kind {
	case "metadata":
		switch key {
		case "catalog_ref", "provider", "driver", "model_id", "profile", "snapshot_identity", "tool_name", "phase", "pricing_revision", "pricing_currency", "pricing_unit", "input_price", "cached_input_price", "output_price":
			return true
		}
	case "estimate":
		switch key {
		case "input_tokens", "output_tokens", "result_chars", "result_bytes", "documents", "context_chars", "concurrency", "cost_cny":
			return true
		}
	case "actual":
		switch key {
		case "input_tokens", "cached_input_tokens", "output_tokens", "reasoning_tokens", "result_chars", "result_bytes", "documents", "context_chars", "cost_cny":
			return true
		}
	}
	return false
}

func nonnegativeInt(value any) (int, bool) {
	if number, ok := value.(float64); ok && number >= 0 && number < math.Exp2(63) && !math.IsNaN(number) && !math.IsInf(number, 0) && number == math.Trunc(number) {
		return int(number), true
	}
	return 0, false
}

func nonnegativeUint64(value any) (uint64, bool) {
	if number, ok := value.(float64); ok && number >= 0 && number < math.Exp2(64) && !math.IsNaN(number) && !math.IsInf(number, 0) && number == math.Trunc(number) {
		return uint64(number), true
	}
	return 0, false
}
