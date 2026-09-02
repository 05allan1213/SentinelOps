// Package runtime exposes the read-only Runtime query surface and the
// asynchronous Recovery command endpoint.
//
// The controller owns HTTP concerns only.  In particular, it never invokes an
// Agent, Tool, Effect executor, or Worker loop.  Recovery acceptance is a
// durable command write; execution remains owned by the Worker.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	apiRuntime "SentinelOps/api/runtime"
	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	runtimesvc "SentinelOps/internal/service/runtime"
	"SentinelOps/utility/sse"

	"github.com/gogf/gf/v2/frame/g"
)

// ControllerV1 is the HTTP adapter for the frozen Runtime v1 contract.
type ControllerV1 struct {
	service *runtimesvc.RuntimeService
}

// NewV1 binds one RuntimeService to all Runtime routes.  The service is
// intentionally concrete so route registration cannot accidentally introduce
// a second persistence or execution implementation.
func NewV1(service *runtimesvc.RuntimeService) *ControllerV1 {
	return &ControllerV1{service: service}
}

var _ apiRuntime.IRuntimeV1 = (*ControllerV1)(nil)

func (c *ControllerV1) ListRuns(ctx context.Context, req *v1.ListRunsReq) (*v1.ListRunsRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	from, to, err := parseTimeRange(req.From, req.To)
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListRuns(ctx, workflowRuntimeRunFilter(req, from, to))
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) GetRun(ctx context.Context, req *v1.GetRunReq) (*v1.GetRunRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	item, err := c.service.GetRun(ctx, req.RunID)
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &v1.GetRunRes{Item: item, ResourceMeta: item.ResourceMeta}, nil
}

func (c *ControllerV1) GetTimeline(ctx context.Context, req *v1.GetTimelineReq) (*v1.TimelineRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := validateTimelineQuery(req); err != nil {
		return nil, c.fail(ctx, err)
	}
	from, to, err := parseTimeRange(req.From, req.To)
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListTimeline(ctx, req.RunID, runtimesvc.TimelineFilter{
		EventTypes: req.EventTypes, Attempt: req.Attempt, Generation: req.Generation,
		From: from, To: to, Page: req.Page, PageSize: req.PageSize,
		Sort: req.Sort, Direction: string(req.Direction),
	})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

// RunEvents is the only streaming Runtime endpoint.  It writes the same
// RuntimeEventDTO emitted by the JSON Timeline endpoint into each SSE data
// frame, preserving the durable sequence as the SSE id.
func (c *ControllerV1) RunEvents(ctx context.Context, req *v1.RunEventsReq) (*v1.RunEventsRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	// Do the ownership/resource check before committing the SSE 200 headers.
	// TailRuntimeEvents repeats the check before entering its polling loop.
	if _, err := c.service.GetRun(ctx, req.RunID); err != nil {
		return nil, c.fail(ctx, err)
	}
	request := g.RequestFromCtx(ctx)
	if request == nil {
		return nil, c.fail(ctx, errors.New("runtime SSE request is not initialized"))
	}
	client := sse.NewClient(request)
	err := c.service.TailRuntimeEvents(ctx, req.RunID, req.AfterSeq, func(event v1.RuntimeEventDTO) error {
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		client.SendEvent(int64(event.Seq), safeSSEEventType(event.EventType), string(payload))
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil
		}
		// The stream headers have already been committed.  Keep the wire format
		// safe and stable for a mid-stream read failure; the initial ownership
		// and validation failures still use the normal JSON error envelope.
		httpErr := runtimesvc.RuntimeHTTPError(err)
		payload, _ := json.Marshal(map[string]string{"code": httpErr.Code, "message": httpErr.Message})
		client.SendEvent(0, "error", string(payload))
		client.Done()
		return nil, nil
	}
	client.Done()
	return nil, nil
}

func (c *ControllerV1) GetAttempts(ctx context.Context, req *v1.GetAttemptsReq) (*v1.AttemptsRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListAttempts(ctx, req.RunID, v1.PageRequest{Page: req.Page, PageSize: req.PageSize})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) GetCheckpoints(ctx context.Context, req *v1.GetCheckpointsReq) (*v1.CheckpointsRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListCheckpoints(ctx, req.RunID, v1.PageRequest{Page: req.Page, PageSize: req.PageSize})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) GetApprovals(ctx context.Context, req *v1.GetApprovalsReq) (*v1.ApprovalsRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListApprovals(ctx, req.RunID, v1.PageRequest{Page: req.Page, PageSize: req.PageSize})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) GetEffects(ctx context.Context, req *v1.GetEffectsReq) (*v1.EffectsRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListEffects(ctx, req.RunID, runtimesvc.EffectFilter{
		Status: req.Status, EffectRole: req.EffectRole, EffectStep: req.EffectStep,
		Attempt: req.Attempt, Generation: req.Generation, Page: req.Page, PageSize: req.PageSize,
	})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) GetEvidence(ctx context.Context, req *v1.GetEvidenceReq) (*v1.EvidenceRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListEvidence(ctx, req.RunID, v1.PageRequest{Page: req.Page, PageSize: req.PageSize})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) ExpandEvidence(ctx context.Context, req *v1.ExpandEvidenceReq) (*v1.ExpandEvidenceRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	item, err := c.service.ExpandEvidenceQuote(ctx, req.RunID, req.EvidenceID, req.Include)
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &v1.ExpandEvidenceRes{Item: item, ResourceMeta: item.ResourceMeta}, nil
}

func (c *ControllerV1) GetContext(ctx context.Context, req *v1.GetContextReq) (*v1.GetContextRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	// Run detail already builds the metadata-only Context projection and keeps
	// raw history/snapshot bytes out of the response.  The v1 Context route is a
	// narrow view of that same projection, so it cannot diverge in field or
	// redaction semantics.
	detail, err := c.service.GetRun(ctx, req.RunID)
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	if req.Include == string(runtimesvc.ContentKindHistory) {
		// The current v1 ContextDTO is metadata-only.  Still enforce the explicit
		// content gate when a caller asks for history so the query parameter can
		// never become an authorization-free no-op as the DTO evolves.
		if err := runtimesvc.AuthorizeRuntimeContent(ctx, detail.ContextSummary.Identity.UserID, runtimesvc.ContentKindHistory); err != nil {
			return nil, c.fail(ctx, err)
		}
	}
	summary := detail.ContextSummary
	item := v1.ContextDTO{
		Identity:                 summary.Identity,
		SessionRevisionUsed:      summary.SessionRevisionUsed,
		SessionRevisionCommitted: summary.SessionRevisionCommitted,
		SummaryHash:              summary.SummaryHash,
		HistoryCount:             summary.HistoryCount,
		BudgetLimitsHash:         summary.BudgetLimitsHash,
		DeadlineAt:               summary.DeadlineAt,
		RuntimeVersion:           summary.RuntimeVersion,
		RuntimeCompatibilityHash: summary.RuntimeCompatibilityHash,
		PolicyHash:               summary.PolicyHash,
		ConfigHash:               summary.ConfigHash,
		GateKeys:                 summary.GateKeys,
		ResourceMeta:             summary.ResourceMeta,
	}
	return &v1.GetContextRes{Item: item, ResourceMeta: item.ResourceMeta}, nil
}

func (c *ControllerV1) GetTraces(ctx context.Context, req *v1.GetTracesReq) (*v1.TracesRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.ListTraceAggregates(ctx, req.RunID, v1.PageRequest{Page: req.Page, PageSize: req.PageSize})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) RecoverRun(ctx context.Context, req *v1.RecoverRunReq) (*v1.OperationAcceptedRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	input, err := runtimesvc.ValidateRecoveryRequest(ctx, req.RunID, v1.RecoveryCommandRequest{
		Action: req.Action, IdempotencyKey: req.IdempotencyKey, Reason: req.Reason,
		ExpectedGeneration: req.ExpectedGeneration, ExpectedCompatibilityHash: req.ExpectedCompatibilityHash,
	})
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	// AcceptRecoveryOperation appends operation.accepted atomically and returns.
	// It never calls a Runner, Agent, Tool, or Effect executor.
	record, err := c.service.Store.AcceptRecoveryOperation(ctx, input)
	if err != nil {
		return nil, c.fail(ctx, runtimesvc.MapRecoveryError(err))
	}
	item := mapOperation(record.Operation, record.IdempotentReplay)
	setStatus(ctx, http.StatusAccepted)
	return &v1.OperationAcceptedRes{Operation: item, ResourceMeta: item.ResourceMeta}, nil
}

func (c *ControllerV1) GetOperation(ctx context.Context, req *v1.GetOperationReq) (*v1.GetOperationRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	operation, err := c.service.Store.LoadRuntimeOperation(ctx, req.OperationID)
	if err != nil {
		return nil, c.fail(ctx, runtimesvc.MapRecoveryError(err))
	}
	item := mapOperation(operation, false)
	return &v1.GetOperationRes{Item: item, ResourceMeta: item.ResourceMeta}, nil
}

func (c *ControllerV1) GetCapabilities(ctx context.Context, req *v1.GetCapabilitiesReq) (*v1.CapabilitiesRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if c.service == nil {
		meta := unavailableNotRunMeta()
		return &v1.CapabilitiesRes{Items: []v1.CapabilityDTO{}, Page: unavailablePage(), ResourceMeta: meta}, nil
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.GetCapabilities(ctx)
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) GetSafety(ctx context.Context, req *v1.GetSafetyReq) (*v1.SafetyRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if c.service == nil {
		meta := unavailableNotRunMeta()
		return &v1.SafetyRes{Item: v1.SafetyDTO{ResourceMeta: meta}, ResourceMeta: meta}, nil
	}
	if err := c.requireService(ctx); err != nil {
		return nil, err
	}
	res, err := c.service.GetSafety(ctx)
	if err != nil {
		return nil, c.fail(ctx, err)
	}
	return &res, nil
}

func (c *ControllerV1) GetWorkerHealth(ctx context.Context, req *v1.GetWorkerHealthReq) (*v1.WorkerHealthRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if c.service != nil {
		res, err := c.service.GetWorkerHealth(ctx)
		if err != nil {
			return nil, c.fail(ctx, err)
		}
		return &res, nil
	}
	meta := unavailableNotRunMeta()
	return &v1.WorkerHealthRes{Items: []v1.WorkerObservationDTO{}, Page: unavailablePage(), ResourceMeta: meta}, nil
}

func (c *ControllerV1) GetEval(ctx context.Context, req *v1.GetEvalReq) (*v1.EvalRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	defaultPage(&req.Page, &req.PageSize)
	if err := req.Valid(); err != nil {
		return nil, c.fail(ctx, err)
	}
	meta := unavailableNotRunMeta()
	return &v1.EvalRes{Item: v1.EvalDTO{Suite: req.Suite, ResourceMeta: meta}, ResourceMeta: meta}, nil
}

func (c *ControllerV1) GetRelease(ctx context.Context, req *v1.GetReleaseReq) (*v1.ReleaseRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	meta := unavailableNotRunMeta()
	return &v1.ReleaseRes{Item: v1.ReleaseDTO{ResourceMeta: meta}, ResourceMeta: meta}, nil
}

func (c *ControllerV1) GetRetention(ctx context.Context, req *v1.GetRetentionReq) (*v1.RetentionRes, error) {
	if req == nil {
		return nil, c.fail(ctx, v1.ErrRuntimeRequestValidation)
	}
	if c.service != nil {
		res, err := c.service.GetRetention(ctx)
		if err != nil {
			return nil, c.fail(ctx, err)
		}
		return &res, nil
	}
	meta := unavailableNotRunMeta()
	return &v1.RetentionRes{Item: v1.RetentionDTO{ResourceMeta: meta}, ResourceMeta: meta}, nil
}

func workflowRuntimeRunFilter(req *v1.ListRunsReq, from, to *time.Time) mysql.RuntimeRunFilter {
	return mysql.RuntimeRunFilter{
		Status: string(req.Status), SessionID: req.SessionID, Agent: req.Agent,
		From: from, To: to, Scope: req.Scope, IncludeLegacy: req.IncludeLegacy,
		Sort: req.Sort, Direction: string(req.Direction), Page: req.Page, PageSize: req.PageSize,
	}
}

func parseTimeRange(fromValue, toValue string) (*time.Time, *time.Time, error) {
	from, err := v1.ParseRFC3339UTC(fromValue)
	if err != nil {
		return nil, nil, err
	}
	to, err := v1.ParseRFC3339UTC(toValue)
	if err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, v1.ErrRuntimeRequestValidation
	}
	return from, to, nil
}

// validateTimelineQuery narrows the shared Run sort allowlist to the two
// orderings that Timeline actually supports.  Keeping this check at the HTTP
// boundary prevents an unsupported sort from silently becoming the default
// sequence ordering in RuntimeService.
func validateTimelineQuery(req *v1.GetTimelineReq) error {
	if req == nil {
		return v1.ErrRuntimeRequestValidation
	}
	if req.Attempt < 0 {
		return v1.ErrRuntimeRequestValidation
	}
	if req.Sort != "" && req.Sort != "seq" && req.Sort != "created_at" {
		return v1.ErrRuntimeRequestValidation
	}
	return nil
}

func defaultPage(page, pageSize *int) {
	if page != nil && *page == 0 {
		*page = 1
	}
	if pageSize != nil && *pageSize == 0 {
		*pageSize = 50
	}
}

func unavailableNotRunMeta() v1.ResourceMeta {
	return v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: "not_observed", NotRun: true}
}

func unavailablePage() v1.PageMeta {
	return v1.PageMeta{Page: 1, PageSize: 50, Total: 0, HasNext: false}
}

func mapOperation(operation workflow.Operation, idempotentReplay bool) v1.OperationDTO {
	status := v1.OperationStatus(operation.Status)
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	if !status.Valid() || !v1.RecoveryAction(operation.Action).Valid() {
		meta = v1.ResourceMeta{Availability: v1.AvailabilityPartial, DataQuality: v1.DataQualityUnknown, ReasonCode: "unknown_operation_state"}
	}
	reason := operation.ResultReason
	if reason == "" {
		reason = operation.Reason
	}
	return v1.OperationDTO{
		OperationID: operation.OperationID, RunID: operation.RunID,
		Action: v1.RecoveryAction(operation.Action), Status: status,
		Terminal: operation.Terminal, IdempotentReplay: idempotentReplay,
		AcceptedAt: operation.AcceptedAt, StartedAt: operation.StartedAt,
		FinishedAt: operation.FinishedAt, Reason: reason, ErrorCode: operation.ErrorCode,
		CorrelationSeq: operation.CorrelationSeq, ResourceMeta: meta,
	}
}

func safeSSEEventType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "runtime.event"
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '-' || char == '_' {
			continue
		}
		return "runtime.event"
	}
	return value
}

func (c *ControllerV1) fail(ctx context.Context, err error) error {
	if err == nil {
		err = errors.New("runtime request failed")
	}
	httpErr := runtimesvc.RuntimeHTTPError(err)
	setStatus(ctx, httpErr.Status)
	return httpErr
}

func (c *ControllerV1) requireService(ctx context.Context) error {
	if c == nil || c.service == nil || c.service.Store == nil || c.service.Store.DB() == nil {
		return c.fail(ctx, errors.New("runtime service is not initialized"))
	}
	return nil
}

func setStatus(ctx context.Context, status int) {
	if request := g.RequestFromCtx(ctx); request != nil {
		request.Response.WriteStatus(status)
	}
}
