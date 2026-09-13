package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
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
		mergeSafetyMeta(&itemMeta, s.validateApprovalCheckpointFact(ctx, run, rows[index]))
		item.ResourceMeta = itemMeta
		if itemMeta.DataQuality != v1.DataQualityComplete || itemMeta.Availability != v1.AvailabilityAvailable {
			mergeSafetyMeta(&meta, itemMeta)
		}
		items = append(items, item)
	}
	return v1.ApprovalsRes{
		Items:        items,
		Page:         v1.PageMeta{Page: page, PageSize: pageSize, Total: total, HasNext: runtimePageHasNext(page, pageSize, total)},
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
	items := make([]v1.EffectDTO, 0, len(rows))
	meta := completeSafetyMeta()
	parents := s.loadEffectParents(ctx, rows)
	for index := range rows {
		parent, parentChecked := parents[pointerString(rows[index].ParentEffectID)]
		eventResult, eventErr := s.Store.ListRuntimeEffectEventsBounded(ctx, run.ID, rows[index].ID)
		item, itemMeta := mapRuntimeEffectWithParentAndTruncation(rows[index], eventResult.Events, parent, parentChecked, eventResult.Truncated)
		if eventErr != nil {
			mergeSafetyMeta(&itemMeta, partialSafetyMeta("effect_event_source_error"))
		}
		item.ResourceMeta = itemMeta
		if itemMeta.DataQuality != v1.DataQualityComplete || itemMeta.Availability != v1.AvailabilityAvailable {
			mergeSafetyMeta(&meta, itemMeta)
		}
		items = append(items, item)
	}
	return v1.EffectsRes{
		Items:        items,
		Page:         v1.PageMeta{Page: page, PageSize: pageSize, Total: total, HasNext: runtimePageHasNext(page, pageSize, total)},
		ResourceMeta: meta,
	}, nil
}

// loadEffectParents re-reads every derived parent through the scoped DAO.  A
// list page is not a complete DAG: the stable primary may be on another page,
// and an untrusted ParentEffectID must never be accepted merely because it
// resembles the primary key.
func (s *RuntimeService) loadEffectParents(ctx context.Context, rows []mysql.AgentEffect) map[string]*mysql.AgentEffect {
	parents := make(map[string]*mysql.AgentEffect)
	if s == nil || s.Store == nil {
		return parents
	}
	for index := range rows {
		parentID := pointerString(rows[index].ParentEffectID)
		if rows[index].EffectRole != workflow.EffectRoleDerived || strings.TrimSpace(parentID) == "" {
			continue
		}
		if _, checked := parents[parentID]; checked {
			continue
		}
		parent, err := s.Store.GetRuntimeEffect(ctx, parentID)
		if err != nil {
			// A nil value is intentionally cached as a checked miss.  The
			// mapper turns it into partial metadata without leaking SQL errors.
			parents[parentID] = nil
			continue
		}
		parents[parentID] = parent
	}
	return parents
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
	eventResult, err := s.Store.ListRuntimeEffectEventsBounded(ctx, effect.RunID, effect.ID)
	if err != nil {
		return nil, MapRecoveryError(err)
	}
	history, _, _ := buildEffectHistoryWithMeta(*effect, eventResult.Events)
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
	eventSeq, eventMeta := latestApprovalEventFacts(row, events)
	mergeSafetyMeta(&meta, eventMeta)
	if eventSeq == 0 {
		mergeSafetyMeta(&meta, partialSafetyMeta("approval_event_not_observed"))
	}
	checkpointMeta := validateApprovalCheckpointProjection(row, events)
	mergeSafetyMeta(&meta, checkpointMeta)
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
	if row.CheckpointPayloadSHA256 != nil {
		item.CheckpointPayloadSHA256 = safeHash(*row.CheckpointPayloadSHA256, &meta)
	}
	if row.CheckpointLeaseGeneration != nil {
		item.CheckpointLeaseGeneration = *row.CheckpointLeaseGeneration
	}
	return item, meta
}

func mapRuntimeEffect(row mysql.AgentEffect, events []mysql.WorkflowEvent) (v1.EffectDTO, v1.ResourceMeta) {
	return mapRuntimeEffectWithParentAndTruncation(row, events, nil, false, false)
}

func mapRuntimeEffectWithParent(row mysql.AgentEffect, events []mysql.WorkflowEvent, parent *mysql.AgentEffect, parentChecked bool) (v1.EffectDTO, v1.ResourceMeta) {
	return mapRuntimeEffectWithParentAndTruncation(row, events, parent, parentChecked, false)
}

func mapRuntimeEffectWithParentAndTruncation(row mysql.AgentEffect, events []mysql.WorkflowEvent, parent *mysql.AgentEffect, parentChecked, truncated bool) (v1.EffectDTO, v1.ResourceMeta) {
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
	if row.EffectRole == workflow.EffectRoleDerived {
		if row.ParentEffectID == nil || strings.TrimSpace(*row.ParentEffectID) == "" {
			mergeSafetyMeta(&meta, partialSafetyMeta("derived_parent_missing"))
		} else {
			expectedPrimary, err := policy.EffectKey(row.RunID, row.ProposalHash, workflow.EffectStepPrimary)
			if err != nil || *row.ParentEffectID != expectedPrimary {
				mergeSafetyMeta(&meta, partialSafetyMeta("derived_parent_identity_mismatch"))
			}
			if parentChecked {
				if parent == nil || !validStablePrimaryEffect(*parent, row) {
					mergeSafetyMeta(&meta, partialSafetyMeta("derived_parent_mismatch"))
				}
			}
		}
	}
	history, historyOK, historyMeta, eventQualityComplete := buildEffectHistoryWithQuality(row, events)
	mergeSafetyMeta(&meta, historyMeta)
	if truncated {
		mergeSafetyMeta(&meta, partialSafetyMeta("effect_history_truncated"))
	}
	if !historyOK {
		mergeSafetyMeta(&meta, partialSafetyMeta("effect_history_not_observed"))
	}
	canonicalStatus := canonicalRuntimeEffectStatus(row.Status, history, historyOK, truncated, &meta, !eventQualityComplete)
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
		Status:                 safeText(canonicalStatus),
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

// canonicalRuntimeEffectStatus derives the public status from the newest
// associated Event.  AgentEffect.status is a mutable projection and therefore
// cannot overwrite an observed unknown/reconciling outcome (or claim success
// when no canonical history was observed).
func canonicalRuntimeEffectStatus(persisted string, history []v1.EffectHistoryDTO, observed, truncated bool, meta *v1.ResourceMeta, eventQualityIssue ...bool) string {
	if truncated {
		return workflow.EffectStatusUnknown
	}
	if !observed || len(history) == 0 {
		if meta != nil {
			mergeSafetyMeta(meta, partialSafetyMeta("effect_status_not_observed"))
		}
		return workflow.EffectStatusUnknown
	}
	canonical := history[len(history)-1].Status
	if _, ok := effectLedgerStatuses[canonical]; !ok {
		canonical = workflow.EffectStatusUnknown
	}
	// A canonical history that contains malformed, unknown, or conflicting
	// event facts cannot support a success conclusion.  Reconciling remains
	// visible because it is itself a fail-closed state; every other state is
	// reduced to unknown while retaining the partial metadata reason.  The
	// explicit flag is intentionally separate from row-level hash/identity
	// quality: an invalid ancillary projection must not hide a valid event state.
	if len(eventQualityIssue) > 0 && eventQualityIssue[0] {
		if meta != nil {
			mergeSafetyMeta(meta, partialSafetyMeta("effect_event_quality_incomplete"))
		}
		if canonical == workflow.EffectStatusReconciling {
			return canonical
		}
		return workflow.EffectStatusUnknown
	}
	if persisted != canonical {
		// The write that emits effect.started can be observed before the mutable
		// AgentEffect projection advances from pending to running.  Treat that
		// one expected ordering gap as non-conflicting; all other mismatches are
		// retained as partial and cannot produce a success conclusion.
		if !(persisted == workflow.EffectStatusPending && canonical == workflow.EffectStatusRunning) {
			if meta != nil {
				mergeSafetyMeta(meta, partialSafetyMeta("effect_status_projection_mismatch"))
			}
			if canonical == workflow.EffectStatusSucceeded {
				return workflow.EffectStatusUnknown
			}
		}
	}
	return canonical
}

// validateApprovalCheckpointFact re-checks the opaque Eino checkpoint binding
// against metadata calculated by MySQL.  Approval rows intentionally carry a
// copied fingerprint, so a list response must not treat that copy as proof
// that the current checkpoint still exists or belongs to this Run.
func (s *RuntimeService) validateApprovalCheckpointFact(ctx context.Context, run *mysql.WorkflowRun, approval mysql.AgentApproval) v1.ResourceMeta {
	meta := completeSafetyMeta()
	if run == nil || s == nil || s.Store == nil || s.Store.DB() == nil {
		return partialSafetyMeta("checkpoint_source_unavailable")
	}
	if strings.TrimSpace(approval.RunID) == "" || approval.RunID != run.ID {
		return partialSafetyMeta("approval_run_identity_mismatch")
	}
	checkpointID := pointerString(approval.CheckpointID)
	checkpointSHA := pointerString(approval.CheckpointPayloadSHA256)
	checkpointGeneration := uint64(0)
	if approval.CheckpointLeaseGeneration != nil {
		checkpointGeneration = *approval.CheckpointLeaseGeneration
	}
	if checkpointID == "" || checkpointSHA == "" || checkpointGeneration == 0 {
		// A preparing Approval is intentionally allowed to exist before the
		// checkpoint is committed.  Every other lifecycle state needs a complete
		// binding before it can be presented as current.
		if approval.Status == workflow.ApprovalStatusPreparing && checkpointID == "" && checkpointSHA == "" && checkpointGeneration == 0 {
			return meta
		}
		return partialSafetyMeta("approval_checkpoint_incomplete")
	}

	expectedCheckpointID, err := workflow.EinoCheckpointID(run.ID)
	if err != nil || checkpointID != expectedCheckpointID {
		return partialSafetyMeta("invalid_checkpoint_id")
	}
	if !validSHA256Hex(checkpointSHA) {
		return partialSafetyMeta("invalid_checkpoint_digest")
	}
	if run.RuntimeVersion == nil || strings.TrimSpace(*run.RuntimeVersion) == "" ||
		run.RuntimeCompatibilityHash == nil || !validSHA256Hex(*run.RuntimeCompatibilityHash) {
		return partialSafetyMeta("run_checkpoint_identity_missing")
	}

	fact, err := s.Store.GetRuntimeCheckpointFact(ctx, run.ID, checkpointID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return partialSafetyMeta("checkpoint_not_observed")
		}
		return partialSafetyMeta("checkpoint_source_error")
	}
	if fact == nil {
		return partialSafetyMeta("checkpoint_not_observed")
	}
	if fact.ID != deterministicCheckpointRowID(checkpointID) || fact.RunID != run.ID ||
		fact.CheckpointKey != checkpointID || fact.EinoCheckpointID == nil || *fact.EinoCheckpointID != checkpointID {
		return partialSafetyMeta("checkpoint_identity_mismatch")
	}
	if fact.PayloadSHA256 == nil || !validSHA256Hex(*fact.PayloadSHA256) || *fact.PayloadSHA256 != checkpointSHA ||
		fact.BlobSHA256 == nil || !validSHA256Hex(*fact.BlobSHA256) || *fact.BlobSHA256 != *fact.PayloadSHA256 {
		return partialSafetyMeta("checkpoint_digest_mismatch")
	}
	if fact.BlobLength == nil || *fact.BlobLength <= 0 {
		return partialSafetyMeta("checkpoint_blob_missing")
	}
	if fact.LeaseGeneration == nil || *fact.LeaseGeneration == 0 || *fact.LeaseGeneration != checkpointGeneration || *fact.LeaseGeneration > run.LeaseGeneration {
		return partialSafetyMeta("checkpoint_generation_mismatch")
	}
	if fact.RuntimeVersion == nil || *fact.RuntimeVersion != *run.RuntimeVersion {
		return partialSafetyMeta("checkpoint_runtime_version_mismatch")
	}
	if fact.RuntimeCompatibilityHash == nil || *fact.RuntimeCompatibilityHash != *run.RuntimeCompatibilityHash {
		return partialSafetyMeta("checkpoint_compatibility_mismatch")
	}
	if fact.CommittedAt == nil {
		return partialSafetyMeta("checkpoint_not_committed")
	}
	if fact.ExpiresAt != nil && !fact.ExpiresAt.After(time.Now().UTC()) {
		return partialSafetyMeta("checkpoint_expired")
	}
	return meta
}

func validSHA256Hex(value string) bool {
	return len(value) == sha256.Size*2 && strings.ToLower(value) == value && func() bool {
		_, err := hex.DecodeString(value)
		return err == nil
	}()
}

func deterministicCheckpointRowID(checkpointID string) string {
	digest := sha256.Sum256([]byte(checkpointID))
	return hex.EncodeToString(digest[:])
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
	if err != nil || expected != row.ID {
		return false
	}
	// GetRuntimeEffect returns the persisted key for compatibility, while list
	// projections return only SHA-256(idempotency_key).  Accept exactly those
	// two representations; an arbitrary digest must not pass identity checks.
	// The digest must be computed explicitly: effectKeyDigest returns 64-hex
	// input unchanged, so reusing it here would reject every list projection
	// (production always persists idempotency_key == effect id).
	digest := sha256.Sum256([]byte(row.ID))
	return row.IdempotencyKey == row.ID || row.IdempotencyKey == hex.EncodeToString(digest[:])
}

func validStablePrimaryEffect(parent, child mysql.AgentEffect) bool {
	if parent.RunID != child.RunID || parent.ProposalHash != child.ProposalHash ||
		parent.EffectRole != workflow.EffectRolePrimary || parent.EffectStep != workflow.EffectStepPrimary || parent.ParentEffectID != nil {
		return false
	}
	expected, err := policy.EffectKey(child.RunID, child.ProposalHash, workflow.EffectStepPrimary)
	return err == nil && parent.ID == expected && validRuntimeEffectIdentity(parent)
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

func latestApprovalEventFacts(row mysql.AgentApproval, events []mysql.WorkflowEvent) (uint64, v1.ResourceMeta) {
	meta := completeSafetyMeta()
	var seq uint64
	for _, event := range events {
		if !eventReferencesID(event, row.ID, "approval_id") {
			continue
		}
		parsed := parseSafetyEvent(event)
		mergeSafetyMeta(&meta, parsed.dto.ResourceMeta)
		if !approvalLifecycleEvent(event.EventType) {
			mergeSafetyMeta(&meta, partialSafetyMeta("unknown_approval_event"))
		}
		validateApprovalCheckpointEvent(row, parsed, &meta)
		if event.Seq > seq {
			seq = event.Seq
		}
	}
	return seq, meta
}

func approvalLifecycleEvent(eventType string) bool {
	switch eventType {
	case workflow.EventApprovalPreparing, workflow.EventApprovalRequested, workflow.EventApprovalDecided,
		workflow.EventApprovalExpired, workflow.EventApprovalInvalidated:
		return true
	default:
		return false
	}
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
	if strings.TrimSpace(id) == "" || safeIdentity(id) == "" {
		return false
	}
	event := parseSafetyEvent(row)
	if event.dto.Reference == id || event.dto.Attributes[attribute] == id {
		return true
	}
	value, ok := event.data[attribute].(string)
	if ok && value == id {
		return true
	}
	// Legacy and malformed payloads may not survive JSON decoding.  Matching a
	// validated, bounded ID is a conservative association aid; the event is
	// still marked partial by MapRuntimeEvent and never becomes trusted data.
	return strings.Contains(row.Payload, id)
}

func buildEffectHistory(effect mysql.AgentEffect, events []mysql.WorkflowEvent) ([]v1.EffectHistoryDTO, bool) {
	history, observed, _ := buildEffectHistoryWithMeta(effect, events)
	return history, observed
}

func buildEffectHistoryWithMeta(effect mysql.AgentEffect, events []mysql.WorkflowEvent) ([]v1.EffectHistoryDTO, bool, v1.ResourceMeta) {
	history, observed, meta, _ := buildEffectHistoryWithQuality(effect, events)
	return history, observed, meta
}

func buildEffectHistoryWithQuality(effect mysql.AgentEffect, events []mysql.WorkflowEvent) ([]v1.EffectHistoryDTO, bool, v1.ResourceMeta, bool) {
	result := make([]v1.EffectHistoryDTO, 0)
	meta := completeSafetyMeta()
	eventQualityComplete := true
	for _, row := range events {
		if !eventReferencesID(row, effect.ID, "effect_id") {
			continue
		}
		parsed := parseSafetyEvent(row)
		mergeSafetyMeta(&meta, parsed.dto.ResourceMeta)
		if parsed.dto.ResourceMeta.DataQuality != v1.DataQualityComplete || parsed.dto.ResourceMeta.Availability != v1.AvailabilityAvailable {
			eventQualityComplete = false
		}
		if !effectHistoryEvent(row.EventType) {
			mergeSafetyMeta(&meta, partialSafetyMeta("unknown_effect_event"))
			eventQualityComplete = false
		}
		status := safeEventStatus(row.EventType, parsed)
		if eventStatusConflict(row.EventType, parsed) {
			mergeSafetyMeta(&meta, partialSafetyMeta("effect_event_status_conflict"))
			eventQualityComplete = false
		}
		if _, ok := effectLedgerStatuses[status]; !ok {
			status = workflow.EffectStatusUnknown
		}
		actor := eventActor(parsed)
		reason := eventReason(parsed)
		if reason == "" && row.EventType == workflow.EventEffectResolved && effect.LastError != nil {
			reason = safeText(*effect.LastError)
		}
		if actor == "" && row.EventType == workflow.EventEffectResolved && effect.ResolvedBy != nil {
			actor = safeIdentity(*effect.ResolvedBy)
		}
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
	return result, len(result) > 0, meta, eventQualityComplete
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
		if value, ok := event.data["status"].(string); ok {
			switch value {
			case workflow.EffectStatusPending, workflow.EffectStatusSucceeded, workflow.EffectStatusUnknown:
				return value
			}
		}
		return workflow.EffectStatusUnknown
	default:
		return workflow.EffectStatusUnknown
	}
}

func eventStatusConflict(eventType string, event parsedSafetyEvent) bool {
	raw, exists := event.data["status"]
	if !exists {
		return false
	}
	value, ok := raw.(string)
	if !ok || value == "" {
		return true
	}
	if eventType == workflow.EventEffectResolved {
		return value != workflow.EffectStatusPending && value != workflow.EffectStatusSucceeded && value != workflow.EffectStatusUnknown
	}
	return value != safeEventStatus(eventType, parsedSafetyEvent{data: map[string]any{}})
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
			return safeEvidenceReference(typed)
		case []any:
			refs := make([]string, 0, len(typed))
			for _, item := range typed {
				if text, ok := item.(string); ok {
					if reference := safeEvidenceReference(text); reference != "" {
						refs = append(refs, reference)
					}
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
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "sha256:") && len(value) == len("sha256:")+sha256.Size*2 {
		digest := strings.TrimPrefix(value, "sha256:")
		if strings.ToLower(digest) == digest {
			if _, err := hex.DecodeString(digest); err == nil {
				return value
			}
		}
	}
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func safeEvidenceReference(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if json.Valid([]byte(value)) {
		return hashEvidenceReference(value)
	}
	return safeText(value)
}

func validateApprovalCheckpointProjection(row mysql.AgentApproval, events []mysql.WorkflowEvent) v1.ResourceMeta {
	meta := completeSafetyMeta()
	hasID := row.CheckpointID != nil && strings.TrimSpace(*row.CheckpointID) != ""
	hasSHA := row.CheckpointPayloadSHA256 != nil && strings.TrimSpace(*row.CheckpointPayloadSHA256) != ""
	hasGeneration := row.CheckpointLeaseGeneration != nil && *row.CheckpointLeaseGeneration != 0
	if !hasID && !hasSHA && !hasGeneration {
		if row.Status != workflow.ApprovalStatusPreparing {
			mergeSafetyMeta(&meta, partialSafetyMeta("approval_checkpoint_missing"))
		}
		return meta
	}
	if !hasID || !hasSHA || !hasGeneration {
		mergeSafetyMeta(&meta, partialSafetyMeta("approval_checkpoint_incomplete"))
	}
	if hasID {
		expected, err := workflow.EinoCheckpointID(row.RunID)
		if err != nil || *row.CheckpointID != expected {
			mergeSafetyMeta(&meta, partialSafetyMeta("invalid_checkpoint_id"))
		}
	}
	if row.CheckpointPayloadSHA256 != nil {
		if len(*row.CheckpointPayloadSHA256) != sha256.Size*2 || strings.ToLower(*row.CheckpointPayloadSHA256) != *row.CheckpointPayloadSHA256 {
			mergeSafetyMeta(&meta, partialSafetyMeta("invalid_checkpoint_digest"))
		} else if _, err := hex.DecodeString(*row.CheckpointPayloadSHA256); err != nil {
			mergeSafetyMeta(&meta, partialSafetyMeta("invalid_checkpoint_digest"))
		}
	}
	for _, event := range events {
		if !eventReferencesID(event, row.ID, "approval_id") {
			continue
		}
		validateApprovalCheckpointEvent(row, parseSafetyEvent(event), &meta)
	}
	return meta
}

func validateApprovalCheckpointEvent(row mysql.AgentApproval, event parsedSafetyEvent, meta *v1.ResourceMeta) {
	if meta == nil {
		return
	}
	if value, ok := event.data["checkpoint_id"].(string); ok && row.CheckpointID != nil && value != *row.CheckpointID {
		mergeSafetyMeta(meta, partialSafetyMeta("checkpoint_binding_mismatch"))
	}
	if value, ok := event.data["checkpoint_payload_sha256"].(string); ok && row.CheckpointPayloadSHA256 != nil && value != *row.CheckpointPayloadSHA256 {
		mergeSafetyMeta(meta, partialSafetyMeta("checkpoint_binding_mismatch"))
	}
	if value, ok := event.data["checkpoint_lease_generation"]; ok && row.CheckpointLeaseGeneration != nil {
		generation, valid := nonnegativeUint64(value)
		if !valid || generation != *row.CheckpointLeaseGeneration {
			mergeSafetyMeta(meta, partialSafetyMeta("checkpoint_binding_mismatch"))
		}
	}
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
