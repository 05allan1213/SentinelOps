package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

// EffectFilter is the Runtime service's read-only Effect ledger filter.  The
// DAO owns the identical shape so the service can validate it before any
// database access and the DAO can still be used by non-HTTP callers.
type EffectFilter = mysql.RuntimeEffectFilter

var (
	approvalLifecycleStatuses = map[string]struct{}{
		workflow.ApprovalStatusPreparing:   {},
		workflow.ApprovalStatusPending:     {},
		workflow.ApprovalStatusApproved:    {},
		workflow.ApprovalStatusRejected:    {},
		workflow.ApprovalStatusExpired:     {},
		workflow.ApprovalStatusInvalidated: {},
	}
	effectLedgerStatuses = map[string]struct{}{
		workflow.EffectStatusPending:     {},
		workflow.EffectStatusRunning:     {},
		workflow.EffectStatusSucceeded:   {},
		workflow.EffectStatusFailed:      {},
		workflow.EffectStatusUnknown:     {},
		workflow.EffectStatusReconciling: {},
	}
)

// ListApprovals returns every persisted Approval state for the owned Run.
// Unlike the action queue, Runtime deliberately includes preparing and all
// terminal states so an operator can explain the complete lifecycle.
func (s *RuntimeService) ListApprovals(ctx context.Context, runID string, p v1.PageRequest) (v1.ApprovalsRes, error) {
	run, err := s.loadSafetyRun(ctx, runID)
	if err != nil {
		return v1.ApprovalsRes{}, err
	}
	page, pageSize := normalizeSafetyPage(p)
	rows, total, err := s.Store.ListRuntimeApprovals(ctx, run.ID, page, pageSize)
	if err != nil {
		return v1.ApprovalsRes{}, err
	}
	events, err := s.Store.ListRuntimeEventsForRun(ctx, run.ID)
	if err != nil {
		return v1.ApprovalsRes{}, err
	}

	items := make([]v1.ApprovalDTO, 0, len(rows))
	meta := completeSafetyMeta()
	for index := range rows {
		item, itemMeta := mapRuntimeApproval(rows[index], events)
		item.ResourceMeta = itemMeta
		if itemMeta.DataQuality != v1.DataQualityComplete || itemMeta.Availability != v1.AvailabilityAvailable {
			mergeSafetyMeta(&meta, itemMeta)
		}
		items = append(items, item)
	}
	return v1.ApprovalsRes{
		Items:        items,
		Page:         v1.PageMeta{Page: page, PageSize: pageSize, Total: total, HasNext: int64(page*pageSize) < total},
		ResourceMeta: meta,
	}, nil
}

// ListEffects returns the metadata-only Effect ledger.  It never invokes a
// reconciliation primitive or an Action executor; those remain exclusively
// behind the existing /ops/v1 resolve and accept-unknown paths.
func (s *RuntimeService) ListEffects(ctx context.Context, runID string, f EffectFilter) (v1.EffectsRes, error) {
	run, err := s.loadSafetyRun(ctx, runID)
	if err != nil {
		return v1.EffectsRes{}, err
	}
	if err := validateEffectFilter(f); err != nil {
		return v1.EffectsRes{}, err
	}
	page, pageSize := normalizeSafetyPage(v1.PageRequest{Page: f.Page, PageSize: f.PageSize})
	f.Page, f.PageSize = page, pageSize
	rows, total, err := s.Store.ListRuntimeEffects(ctx, run.ID, f)
	if err != nil {
		return v1.EffectsRes{}, err
	}
	events, err := s.Store.ListRuntimeEventsForRun(ctx, run.ID)
	if err != nil {
		return v1.EffectsRes{}, err
	}

	items := make([]v1.EffectDTO, 0, len(rows))
	meta := completeSafetyMeta()
	for index := range rows {
		item, itemMeta := mapRuntimeEffect(rows[index], events)
		item.ResourceMeta = itemMeta
		if itemMeta.DataQuality != v1.DataQualityComplete || itemMeta.Availability != v1.AvailabilityAvailable {
			mergeSafetyMeta(&meta, itemMeta)
		}
		items = append(items, item)
	}
	return v1.EffectsRes{
		Items:        items,
		Page:         v1.PageMeta{Page: page, PageSize: pageSize, Total: total, HasNext: int64(page*pageSize) < total},
		ResourceMeta: meta,
	}, nil
}

// EffectHistory returns the canonical, bounded history of one Effect.  The
// effect ID is not an authorization substitute: GetRuntimeEffect joins the
// owner Run and applies the current Identity Scope before any event is read.
func (s *RuntimeService) EffectHistory(ctx context.Context, effectID string) ([]v1.EffectHistoryDTO, error) {
	if s == nil || s.Store == nil {
		return nil, ErrRuntimeNotFound
	}
	effect, err := s.Store.GetRuntimeEffect(ctx, effectID)
	if err != nil {
		return nil, MapRecoveryError(err)
	}
	events, err := s.Store.ListRuntimeEffectEvents(ctx, effect.RunID, effect.ID)
	if err != nil {
		return nil, MapRecoveryError(err)
	}
	history, _ := buildEffectHistory(*effect, events)
	return history, nil
}

// GetEffectHistory is an explicit alias for service consumers that prefer a
// getter name; both paths retain the same scoped, read-only implementation.
func (s *RuntimeService) GetEffectHistory(ctx context.Context, effectID string) ([]v1.EffectHistoryDTO, error) {
	return s.EffectHistory(ctx, effectID)
}

func (s *RuntimeService) loadSafetyRun(ctx context.Context, runID string) (*mysql.WorkflowRun, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(runID) == "" {
		return nil, ErrRuntimeNotFound
	}
	run, err := s.Store.GetRuntimeRun(ctx, runID)
	if err != nil {
		return nil, MapRecoveryError(err)
	}
	if err := AuthorizeRun(ctx, run.UserID); err != nil {
		return nil, err
	}
	return run, nil
}

func normalizeSafetyPage(p v1.PageRequest) (int, int) {
	page, size := p.Page, p.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func validateEffectFilter(f EffectFilter) error {
	if f.Page < 0 || f.PageSize < 0 || f.PageSize > 100 {
		return ErrRuntimeInvalidFilter
	}
	if f.Attempt < 0 {
		return ErrRuntimeInvalidFilter
	}
	if f.Status != "" {
		if _, ok := effectLedgerStatuses[f.Status]; !ok {
			return ErrRuntimeInvalidFilter
		}
	}
	if f.EffectRole != "" && f.EffectRole != workflow.EffectRolePrimary && f.EffectRole != workflow.EffectRoleDerived {
		return ErrRuntimeInvalidFilter
	}
	if len(f.EffectStep) > 128 || strings.TrimSpace(f.EffectStep) != f.EffectStep {
		return ErrRuntimeInvalidFilter
	}
	return nil
}

func completeSafetyMeta() v1.ResourceMeta {
	return v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
}

func partialSafetyMeta(reason string) v1.ResourceMeta {
	return v1.ResourceMeta{Availability: v1.AvailabilityPartial, DataQuality: v1.DataQualityPartial, ReasonCode: reason}
}

func mergeSafetyMeta(target *v1.ResourceMeta, source v1.ResourceMeta) {
	if target == nil {
		return
	}
	if source.Availability != v1.AvailabilityAvailable {
		target.Availability = v1.AvailabilityPartial
	}
	if source.DataQuality == v1.DataQualityUnknown {
		target.DataQuality = v1.DataQualityUnknown
	} else if source.DataQuality != v1.DataQualityComplete && target.DataQuality != v1.DataQualityUnknown {
		target.DataQuality = v1.DataQualityPartial
	}
	if target.ReasonCode == "" {
		target.ReasonCode = source.ReasonCode
	}
}

func mapRuntimeApproval(row mysql.AgentApproval, events []mysql.WorkflowEvent) (v1.ApprovalDTO, v1.ResourceMeta) {
	meta := completeSafetyMeta()
	if !validRuntimeApprovalIdentity(row) {
		return v1.ApprovalDTO{ID: safeIdentity(row.ID), RunID: safeIdentity(row.RunID), Status: row.Status}, partialSafetyMeta("invalid_approval_identity")
	}
	proposal, proposalOK := mapRedactedProposal(row.ProposalJSONRedacted)
	if !proposalOK {
		meta = partialSafetyMeta("invalid_redacted_proposal")
	}
	if _, ok := approvalLifecycleStatuses[row.Status]; !ok {
		mergeSafetyMeta(&meta, partialSafetyMeta("unknown_approval_status"))
	}
	eventSeq := latestApprovalEventSeq(row.ID, events)
	if eventSeq == 0 {
		mergeSafetyMeta(&meta, partialSafetyMeta("approval_event_not_observed"))
	}
	item := v1.ApprovalDTO{
		ID:                       safeIdentity(row.ID),
		RunID:                    safeIdentity(row.RunID),
		ToolName:                 safeText(row.ToolName),
		ToolRevision:             safeText(row.ToolRevision),
		RiskLevel:                safeText(row.RiskLevel),
		ProposalHash:             safeHash(row.ProposalHash, &meta),
		RequestedBy:              safeIdentity(row.RequestedBy),
		Status:                   safeText(row.Status),
		Version:                  safeVersion(row.Version, &meta),
		ToolSchemaHash:           safeHash(row.ToolSchemaHash, &meta),
		PolicyHash:               safeHash(row.PolicyHash, &meta),
		RuntimeCompatibilityHash: safeHash(row.RuntimeCompatibilityHash, &meta),
		Proposal:                 proposal,
		EventSeq:                 eventSeq,
	}
	if row.DecidedBy != nil {
		item.DecidedBy = safeIdentity(*row.DecidedBy)
	}
	if row.DecisionReason != nil {
		item.DecisionReason = safeText(*row.DecisionReason)
	}
	item.PublishedAt = row.PublishedAt
	item.ExpiresAt = row.ExpiresAt
	item.DecidedAt = row.DecidedAt
	if row.CheckpointID != nil {
		item.CheckpointID = safeIdentity(*row.CheckpointID)
	}
	if row.CheckpointLeaseGeneration != nil {
		item.CheckpointLeaseGeneration = *row.CheckpointLeaseGeneration
	}
	return item, meta
}

func mapRuntimeEffect(row mysql.AgentEffect, events []mysql.WorkflowEvent) (v1.EffectDTO, v1.ResourceMeta) {
	meta := completeSafetyMeta()
	if !validRuntimeEffectIdentity(row) {
		return v1.EffectDTO{ID: safeIdentity(row.ID), RunID: safeIdentity(row.RunID), Status: safeText(row.Status)}, partialSafetyMeta("invalid_effect_identity")
	}
	if _, ok := effectLedgerStatuses[row.Status]; !ok {
		mergeSafetyMeta(&meta, partialSafetyMeta("unknown_effect_status"))
	}
	if row.EffectRole != workflow.EffectRolePrimary && row.EffectRole != workflow.EffectRoleDerived {
		mergeSafetyMeta(&meta, partialSafetyMeta("unknown_effect_role"))
	}
	if row.EffectRole == workflow.EffectRolePrimary && row.ParentEffectID != nil {
		mergeSafetyMeta(&meta, partialSafetyMeta("primary_parent_mismatch"))
	}
	if row.EffectRole == workflow.EffectRoleDerived && row.ParentEffectID == nil {
		mergeSafetyMeta(&meta, partialSafetyMeta("derived_parent_missing"))
	}
	history, historyOK := buildEffectHistory(row, events)
	if !historyOK {
		mergeSafetyMeta(&meta, partialSafetyMeta("effect_history_not_observed"))
	}
	item := v1.EffectDTO{
		ID:                     safeIdentity(row.ID),
		RunID:                  safeIdentity(row.RunID),
		EffectRole:             safeText(row.EffectRole),
		EffectStep:             safeText(row.EffectStep),
		ProposalHash:           safeHash(row.ProposalHash, &meta),
		ToolName:               safeText(row.ToolName),
		ToolRevision:           safeText(row.ToolRevision),
		ToolSchemaHash:         safeHash(row.ToolSchemaHash, &meta),
		TargetHash:             safeHash(row.TargetHash, &meta),
		EffectType:             safeText(row.EffectType),
		Status:                 safeText(row.Status),
		Version:                safeVersion(row.Version, &meta),
		LeaseGeneration:        row.LeaseGeneration,
		Attempt:                int(row.Attempt),
		ReconciliationAttempts: int(row.ReconciliationAttempts),
		CreatedAt:              row.CreatedAt,
		History:                history,
	}
	item.IdempotencyKeyDigest = effectKeyDigest(row.IdempotencyKey)
	if row.ParentEffectID != nil {
		item.ParentEffectID = safeIdentity(*row.ParentEffectID)
	}
	if row.ExternalReference != nil {
		item.ExternalReference = safeText(*row.ExternalReference)
	}
	if row.Resolution != nil {
		item.Resolution = safeText(*row.Resolution)
	}
	if row.ResolvedBy != nil {
		item.ResolvedBy = safeIdentity(*row.ResolvedBy)
	}
	item.UpdatedAt = &row.UpdatedAt
	return item, meta
}

func validRuntimeApprovalIdentity(row mysql.AgentApproval) bool {
	expected, err := policy.ApprovalID(row.RunID, row.ProposalHash)
	return err == nil && expected == row.ID
}

func validRuntimeEffectIdentity(row mysql.AgentEffect) bool {
	if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.RunID) == "" || strings.TrimSpace(row.EffectStep) == "" {
		return false
	}
	expected, err := policy.EffectKey(row.RunID, row.ProposalHash, row.EffectStep)
	return err == nil && expected == row.ID && row.IdempotencyKey == row.ID
}

func mapRedactedProposal(raw string) (map[string]string, bool) {
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil || object == nil {
		return nil, false
	}
	redacted, err := policy.NewRedactor().RedactJSON(object)
	if err != nil {
		return nil, false
	}
	if err := json.Unmarshal(redacted, &object); err != nil {
		return nil, false
	}
	result := make(map[string]string, len(object))
	valid := true
	for key, value := range object {
		if len(key) > 128 {
			valid = false
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil || len(encoded) > workflow.MaxEventTextBytes {
			valid = false
			continue
		}
		if stringValue, ok := value.(string); ok {
			result[key] = safeText(stringValue)
		} else {
			result[key] = string(encoded)
		}
	}
	return result, valid
}

func safeIdentity(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 || policy.NewRedactor().RedactText(value) != value {
		return ""
	}
	return value
}

func safeText(value string) string {
	value = policy.NewRedactor().RedactText(value)
	if len(value) > workflow.MaxEventTextBytes {
		value = value[:workflow.MaxEventTextBytes]
	}
	return value
}

func safeHash(value string, meta *v1.ResourceMeta) string {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		if meta != nil {
			mergeSafetyMeta(meta, partialSafetyMeta("invalid_digest"))
		}
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		if meta != nil {
			mergeSafetyMeta(meta, partialSafetyMeta("invalid_digest"))
		}
		return ""
	}
	return value
}

func safeVersion(value uint64, meta *v1.ResourceMeta) int {
	if value > uint64(math.MaxInt) {
		if meta != nil {
			mergeSafetyMeta(meta, partialSafetyMeta("version_overflow"))
		}
		return math.MaxInt
	}
	return int(value)
}

func effectKeyDigest(value string) string {
	if len(value) == sha256.Size*2 && strings.ToLower(value) == value {
		if _, err := hex.DecodeString(value); err == nil {
			return value
		}
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func latestApprovalEventSeq(approvalID string, events []mysql.WorkflowEvent) uint64 {
	var seq uint64
	for _, event := range events {
		switch event.EventType {
		case workflow.EventApprovalPreparing, workflow.EventApprovalRequested, workflow.EventApprovalDecided,
			workflow.EventApprovalExpired, workflow.EventApprovalInvalidated:
		default:
			continue
		}
		if eventReferencesID(event, approvalID, "approval_id") && event.Seq > seq {
			seq = event.Seq
		}
	}
	return seq
}

type parsedSafetyEvent struct {
	row    mysql.WorkflowEvent
	dto    v1.RuntimeEventDTO
	data   map[string]any
	parsed bool
}

func parseSafetyEvent(row mysql.WorkflowEvent) parsedSafetyEvent {
	parsed := parsedSafetyEvent{row: row, dto: MapRuntimeEvent(row), data: map[string]any{}}
	var root map[string]any
	if err := json.Unmarshal([]byte(row.Payload), &root); err != nil || root == nil {
		return parsed
	}
	data, ok := root["data"].(map[string]any)
	if !ok {
		data = root
	}
	parsed.data = data
	parsed.parsed = true
	return parsed
}

func eventReferencesID(row mysql.WorkflowEvent, id, attribute string) bool {
	event := parseSafetyEvent(row)
	if event.dto.Reference == id || event.dto.Attributes[attribute] == id {
		return true
	}
	value, ok := event.data[attribute].(string)
	return ok && value == id
}

func buildEffectHistory(effect mysql.AgentEffect, events []mysql.WorkflowEvent) ([]v1.EffectHistoryDTO, bool) {
	result := make([]v1.EffectHistoryDTO, 0)
	for _, row := range events {
		if !effectHistoryEvent(row.EventType) || !eventReferencesID(row, effect.ID, "effect_id") {
			continue
		}
		parsed := parseSafetyEvent(row)
		status := safeEventStatus(row.EventType, parsed)
		if _, ok := effectLedgerStatuses[status]; !ok {
			status = workflow.EffectStatusUnknown
		}
		actor := eventActor(parsed)
		reason := eventReason(parsed)
		evidence := eventEvidenceReference(parsed)
		if evidence == "" && row.EventType == workflow.EventEffectResolved && effect.ResolutionEvidenceRedacted != nil {
			evidence = hashEvidenceReference(*effect.ResolutionEvidenceRedacted)
		}
		result = append(result, v1.EffectHistoryDTO{Seq: row.Seq, EventType: row.EventType, Status: status, ActorID: actor, Reason: reason, EvidenceReference: evidence, CreatedAt: row.CreatedAt})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Seq != result[j].Seq {
			return result[i].Seq < result[j].Seq
		}
		return result[i].EventType < result[j].EventType
	})
	// A history is a canonical Event projection.  Do not manufacture a row
	// from the mutable AgentEffect status when Events are absent.
	return result, len(result) > 0
}

func effectHistoryEvent(eventType string) bool {
	switch eventType {
	case workflow.EventEffectStarted, workflow.EventEffectSucceeded, workflow.EventEffectFailed,
		workflow.EventEffectUnknown, workflow.EventEffectReconciling, workflow.EventEffectResolved,
		workflow.EventRunReconciling, workflow.EventRunParked:
		return true
	default:
		return false
	}
}

func safeEventStatus(eventType string, event parsedSafetyEvent) string {
	if value, ok := event.data["status"].(string); ok && value != "" {
		return value
	}
	switch eventType {
	case workflow.EventEffectStarted:
		return workflow.EffectStatusRunning
	case workflow.EventEffectSucceeded:
		return workflow.EffectStatusSucceeded
	case workflow.EventEffectFailed:
		return workflow.EffectStatusFailed
	case workflow.EventEffectUnknown, workflow.EventRunParked:
		return workflow.EffectStatusUnknown
	case workflow.EventEffectReconciling, workflow.EventRunReconciling:
		return workflow.EffectStatusReconciling
	case workflow.EventEffectResolved:
		return workflow.EffectStatusUnknown
	default:
		return workflow.EffectStatusUnknown
	}
}

func eventActor(event parsedSafetyEvent) string {
	if event.row.ActorID != nil {
		return safeIdentity(*event.row.ActorID)
	}
	if value, ok := event.data["resolved_by"].(string); ok {
		return safeIdentity(value)
	}
	return ""
}

func eventReason(event parsedSafetyEvent) string {
	if event.row.ReasonRedacted != nil {
		return safeText(*event.row.ReasonRedacted)
	}
	for _, key := range []string{"reason", "reason_code", "decision_reason", "cause"} {
		if value, ok := event.data[key].(string); ok {
			return safeText(value)
		}
	}
	return ""
}

func eventEvidenceReference(event parsedSafetyEvent) string {
	for _, key := range []string{"evidence_reference", "evidence_id", "evidence_ids"} {
		value, ok := event.data[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			return safeText(typed)
		case []any:
			refs := make([]string, 0, len(typed))
			for _, item := range typed {
				if text, ok := item.(string); ok && safeIdentity(text) != "" {
					refs = append(refs, safeIdentity(text))
				}
			}
			if len(refs) > 0 {
				sort.Strings(refs)
				return strings.Join(refs, ",")
			}
		}
	}
	return ""
}

func hashEvidenceReference(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
