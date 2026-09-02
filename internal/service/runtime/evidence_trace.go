package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

const (
	maxRuntimeEvidenceQuoteRunes = 8000

	evidenceReasonEventNotObserved      = "evidence_event_not_observed"
	evidenceReasonEventSourceError      = "evidence_event_source_error"
	evidenceReasonInvalidEvent          = "invalid_evidence_event"
	evidenceReasonLegacyEvent           = "legacy_evidence_event"
	evidenceReasonUnknownEvidenceSource = "evidence_source_not_observed"
	evidenceReasonEvidenceIdentity      = "evidence_identity_mismatch"
	evidenceReasonEvidenceScope         = "evidence_scope_denied"
	evidenceReasonEvidenceUnavailable   = "evidence_source_unavailable"
	evidenceReasonCitationMismatch      = "citation_identity_mismatch"
	evidenceReasonCitationSourceUnavail = "citation_source_unavailable"
	evidenceReasonExpired               = "evidence_expired"
	traceReasonSourceUnavailable        = "trace_source_unavailable"
	traceReasonSourceError              = "trace_source_error"
	traceReasonNotObserved              = "trace_not_observed"
	traceReasonQualityUnknown           = "trace_quality_unknown"
	traceReasonIncomplete               = "trace_incomplete"
	traceReasonRawUnavailable           = "trace_detail_unavailable"
	traceReasonInvalidTags              = "invalid_trace_tags"
	traceReasonInvalidTraceIdentity     = "invalid_trace_identity"
	traceReasonNodeCountUnavailable     = "trace_node_count_unavailable"
)

// runtimeEvidenceEventFacts is the only durable evidence membership source
// used by Runtime queries.  In particular, EventEvidenceCited is not treated
// as answer citation truth: older Workers wrote all retrieved IDs there.
type runtimeEvidenceEventFacts struct {
	IDs         []string
	RetrievedAt map[string]time.Time
	Observed    bool
	Meta        v1.ResourceMeta
}

type runtimeCitationFact struct {
	RunID         string
	EvidenceID    string
	SourceVersion string
	ContentHash   string
	AccessScope   string
	Indexed       uint64
	Complete      bool
}

type runtimeEvidenceIdentity struct {
	EvidenceID    string
	SourceType    string
	SourceID      string
	BaseID        string
	DocumentID    string
	ChunkID       string
	SourceVersion string
	ContentHash   string
	AccessScope   string
	Indexed       uint64
}

type runtimeEvidenceScore struct {
	Vector *float64
	Rerank *float64
}

type runtimeTraceFacts struct {
	RunID           string
	Attempt         uint
	LeaseGeneration uint64
	RuntimeVersion  string
	TraceQuality    string
	ServerUserID    string
	Valid           bool
}

// ListEvidence returns metadata for Evidence actually recorded as retrieved
// by this durable Run.  The default projection intentionally excludes quote,
// preview and all prompt/output/tool payloads.  Since Evidence IDs are
// one-way stable hashes, current knowledge rows are reconstructed and checked;
// an event ID alone is never enough to fabricate a complete EvidenceRef.
func (s *RuntimeService) ListEvidence(ctx context.Context, runID string, p v1.PageRequest) (v1.EvidenceRes, error) {
	run, err := s.loadEvidenceRun(ctx, runID)
	if err != nil {
		return v1.EvidenceRes{}, err
	}
	page, pageSize := normalizeSafetyPage(p)
	basePage := v1.PageMeta{Page: page, PageSize: pageSize}

	events, err := s.Store.ListRuntimeEventsForRun(ctx, run.ID)
	if err != nil {
		return v1.EvidenceRes{Page: basePage, ResourceMeta: unavailableRuntimeMeta(evidenceReasonEventSourceError)}, nil
	}
	facts := collectRuntimeEvidenceEvents(events)
	if !facts.Observed {
		return v1.EvidenceRes{Page: basePage, ResourceMeta: facts.MetaOr(evidenceReasonEventNotObserved)}, nil
	}

	sources, err := s.Store.ListRuntimeEvidenceSources(ctx)
	if err != nil {
		return v1.EvidenceRes{Page: basePage, ResourceMeta: unavailableRuntimeMeta(evidenceReasonEventSourceError)}, nil
	}
	sourceByID, duplicateIDs := runtimeEvidenceSourceMap(sources)

	citations, citationMeta := collectRuntimeCitations(run.ID, run.OutputPayload)
	scores, scoreMeta := s.collectRuntimeEvidenceScores(ctx, run.ID)
	meta := facts.MetaOr("")
	mergeSafetyMeta(&meta, citationMeta)
	mergeSafetyMeta(&meta, scoreMeta)

	identity, identityErr := runtimeIdentityFromContext(ctx)
	if identityErr != nil {
		return v1.EvidenceRes{}, ErrRuntimeForbidden
	}
	items := make([]v1.EvidenceDTO, 0, len(facts.IDs))
	for _, evidenceID := range facts.IDs {
		source, ok := sourceByID[evidenceID]
		if !ok {
			mergeSafetyMeta(&meta, partialSafetyMeta(evidenceReasonUnknownEvidenceSource))
			continue
		}
		if duplicateIDs[evidenceID] {
			mergeSafetyMeta(&meta, partialSafetyMeta(evidenceReasonEvidenceIdentity))
			continue
		}
		if !runtimeAccessAllows(identity, source.AccessScope()) {
			// Omit metadata outside the current Scope.  Returning the hash or
			// source ID would disclose the existence of a private document.
			mergeSafetyMeta(&meta, partialSafetyMeta(evidenceReasonEvidenceScope))
			continue
		}
		ref, identityOK, identityReason := reconstructRuntimeEvidenceIdentity(evidenceID, source)
		if !identityOK {
			mergeSafetyMeta(&meta, partialSafetyMeta(identityReason))
			continue
		}
		// EvidenceRef.Validate is deliberately strict about the immutable
		// identity fields and retrieval timestamp.  The list projection does not
		// load source text, so use a constant metadata marker solely for this
		// in-memory contract check; it is never copied into the DTO.
		metadataRef := runtimeEvidenceRef(ref, facts.RetrievedAt[evidenceID], "[metadata-only]")
		if err := metadataRef.Validate(); err != nil {
			mergeSafetyMeta(&meta, partialSafetyMeta(evidenceReasonEvidenceIdentity))
			continue
		}
		item := mapRuntimeEvidenceMetadata(ref, facts.RetrievedAt[evidenceID], source, scores[evidenceID])
		itemMeta := completeSafetyMeta()
		if evidenceExpired(metadataRef.RetrievedAt, time.Now().UTC()) {
			mergeSafetyMeta(&itemMeta, partialSafetyMeta(evidenceReasonExpired))
		}
		if !runtimeEvidenceSourceAvailable(source) {
			item.QuoteAvailable = false
			itemMeta = partialSafetyMeta(runtimeEvidenceSourceReason(source))
		}
		if citation, exists := citations[evidenceID]; exists {
			if !citationMatchesRuntimeEvidence(citation, run.ID, ref) {
				mergeSafetyMeta(&itemMeta, partialSafetyMeta(evidenceReasonCitationMismatch))
				mergeSafetyMeta(&meta, partialSafetyMeta(evidenceReasonCitationMismatch))
			} else if citation.Complete {
				// Re-validate answer citations against the current source and
				// expiry window.  A historical/expired citation remains visible as
				// metadata, but is not presented as currently grounded.
				citationScope := runtimeCitationScope(identity, ref.AccessScope)
				if err := evidence.ValidateCitation(runtimeCitation(citation), evidence.RunEvidence{RunID: run.ID, Scope: citationScope, Refs: map[string]evidence.EvidenceRef{evidenceID: metadataRef}}, time.Now().UTC()); err != nil {
					mergeSafetyMeta(&itemMeta, partialSafetyMeta(runtimeCitationReason(err)))
					mergeSafetyMeta(&meta, partialSafetyMeta(runtimeCitationReason(err)))
				} else {
					item.AnswerReferences = []string{evidenceID}
				}
			}
		}
		if score := scores[evidenceID]; score.Vector == nil && score.Rerank == nil {
			// Scores are optional trace metadata.  Missing scores do not make
			// the Evidence identity invalid, but are truthfully partial.
			mergeSafetyMeta(&itemMeta, partialSafetyMeta("retrieval_scores_not_observed"))
		}
		if duplicateIDs[evidenceID] {
			mergeSafetyMeta(&itemMeta, partialSafetyMeta(evidenceReasonEvidenceIdentity))
		}
		item.ResourceMeta = itemMeta
		mergeSafetyMeta(&meta, itemMeta)
		items = append(items, item)
	}
	if len(items) == 0 && len(facts.IDs) > 0 {
		mergeSafetyMeta(&meta, partialSafetyMeta(evidenceReasonUnknownEvidenceSource))
	}

	start := runtimePageOffset(page, pageSize)
	if start > len(items) {
		start = len(items)
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return v1.EvidenceRes{
		Items:        items[start:end],
		Page:         v1.PageMeta{Page: page, PageSize: pageSize, Total: int64(len(items)), HasNext: runtimePageHasNext(page, pageSize, int64(len(items)))},
		ResourceMeta: meta,
	}, nil
}

// ExpandEvidenceQuote returns a bounded, redacted quote only for an explicit
// include=quote request.  The variadic form keeps old in-process callers that
// predate the include parameter source-compatible; an omitted include is
// still rejected, so it can never accidentally expand content.
func (s *RuntimeService) ExpandEvidenceQuote(ctx context.Context, runID, evidenceID string, include ...string) (v1.EvidenceContentDTO, error) {
	if len(include) != 1 || include[0] != "quote" {
		return v1.EvidenceContentDTO{}, ErrRuntimeInvalidFilter
	}
	run, err := s.loadEvidenceRun(ctx, runID)
	if err != nil {
		return v1.EvidenceContentDTO{}, err
	}
	if err := AuthorizeRuntimeContent(ctx, run.UserID, ContentKindEvidenceQuote); err != nil {
		return v1.EvidenceContentDTO{}, err
	}
	if !validRuntimeEvidenceID(evidenceID) {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}

	events, err := s.Store.ListRuntimeEventsForRun(ctx, run.ID)
	if err != nil {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	facts := collectRuntimeEvidenceEvents(events)
	retrievedAt, observed := facts.RetrievedAt[evidenceID]
	if !facts.Observed || !observed {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	sources, err := s.Store.ListRuntimeEvidenceSources(ctx)
	if err != nil {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	sourceByID, duplicateIDs := runtimeEvidenceSourceMap(sources)
	source, ok := sourceByID[evidenceID]
	if !ok || duplicateIDs[evidenceID] {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	identity, err := runtimeIdentityFromContext(ctx)
	if err != nil || !runtimeAccessAllows(identity, source.AccessScope()) {
		return v1.EvidenceContentDTO{}, ErrRuntimeContentExpansionDenied
	}
	ref, identityOK, _ := reconstructRuntimeEvidenceIdentity(evidenceID, source)
	if !identityOK || !runtimeEvidenceSourceAvailable(source) {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}

	// Re-read the source text only after Run ownership, explicit content
	// permission, current Scope and stable ID checks have all passed.
	current, err := s.Store.GetRuntimeEvidenceSource(ctx, source.ChunkID)
	if err != nil {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	currentRef, currentOK, _ := reconstructRuntimeEvidenceIdentity(evidenceID, *current)
	if !currentOK || !runtimeEvidenceSourceAvailable(*current) || currentRef != ref {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	if strings.TrimSpace(current.ContentPreview) == "" {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}

	evidenceRef := runtimeEvidenceRef(ref, retrievedAt.UTC(), current.ContentPreview)
	if err := evidenceRef.Validate(); err != nil {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	runScope := runtimeCitationScope(identity, ref.AccessScope)
	citationFacts, _ := collectRuntimeCitations(run.ID, run.OutputPayload)
	citation, exists := citationFacts[evidenceID]
	if !exists || !citation.Complete {
		// Expansion is allowed for retrieved Evidence even when the final
		// answer did not cite it, but a malformed citation must not be used to
		// bypass the identity checks below.
		if exists {
			return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
		}
		citation = runtimeCitationFact{RunID: run.ID, EvidenceID: evidenceID, SourceVersion: ref.SourceVersion, ContentHash: ref.ContentHash, AccessScope: ref.AccessScope, Indexed: ref.Indexed, Complete: true}
	}
	if !citationMatchesRuntimeEvidence(citation, run.ID, ref) {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}
	if err := evidence.ValidateCitation(runtimeCitation(citation), evidence.RunEvidence{RunID: run.ID, Scope: runScope, Refs: map[string]evidence.EvidenceRef{evidenceID: evidenceRef}}, time.Now().UTC()); err != nil {
		return v1.EvidenceContentDTO{}, ErrRuntimeNotFound
	}

	redacted := policy.NewRedactor().RedactText(current.ContentPreview)
	redactionApplied := redacted != current.ContentPreview
	redacted = boundRuntimeEvidenceQuote(redacted)
	return v1.EvidenceContentDTO{
		EvidenceID: evidenceID, Quote: redacted, ContentHash: ref.ContentHash,
		SourceVersion: ref.SourceVersion, AccessScope: ref.AccessScope,
		RedactionApplied: redactionApplied,
		ResourceMeta:     completeSafetyMeta(),
	}, nil
}

// ListTraceAggregates returns one metadata-only row per persisted Trace ID,
// ordered by Attempt then Trace ID.  It deliberately does not call
// GetRunByWorkflowRunID, which only exposes the latest Trace and would lose
// Resume/Replay Attempts.  Incomplete traces stay visible for diagnosis but
// are never represented as complete evidence quality.
func (s *RuntimeService) ListTraceAggregates(ctx context.Context, runID string, p v1.PageRequest) (v1.TracesRes, error) {
	run, err := s.loadEvidenceRun(ctx, runID)
	if err != nil {
		return v1.TracesRes{}, err
	}
	page, pageSize := normalizeSafetyPage(p)
	basePage := v1.PageMeta{Page: page, PageSize: pageSize}
	traces := s.traceDAO()
	if traces == nil {
		return v1.TracesRes{Page: basePage, ResourceMeta: unavailableRuntimeMeta(traceReasonSourceUnavailable)}, nil
	}
	rows, err := traces.ListRunsByWorkflowRunID(ctx, run.ID)
	if err != nil {
		return v1.TracesRes{Page: basePage, ResourceMeta: unavailableRuntimeMeta(traceReasonSourceError)}, nil
	}
	if len(rows) == 0 {
		return v1.TracesRes{Page: basePage, ResourceMeta: unavailableRuntimeMeta(traceReasonNotObserved)}, nil
	}

	attemptFacts := s.runtimeAttemptTraceFacts(ctx, run.ID)
	items := make([]v1.TraceAggregateDTO, 0, len(rows))
	meta := completeSafetyMeta()
	for _, row := range rows {
		facts := parseRuntimeTraceFacts(row.Tags)
		itemMeta := completeSafetyMeta()
		if !facts.Valid {
			itemMeta = partialSafetyMeta(traceReasonInvalidTags)
		}
		facts = enrichRuntimeTraceFacts(facts, row.TraceID, attemptFacts)
		if facts.RunID == "" {
			facts.RunID = run.ID
		}
		if facts.TraceQuality == "" {
			facts.TraceQuality = "unknown"
		}
		quality, qualityMeta := mapRuntimeTraceQuality(facts.TraceQuality)
		mergeSafetyMeta(&itemMeta, qualityMeta)
		if row.Status == "running" {
			mergeSafetyMeta(&itemMeta, partialSafetyMeta(traceReasonQualityUnknown))
		}
		nodeCount, countErr := traces.CountNodesByTraceID(ctx, row.TraceID)
		if countErr != nil {
			nodeCount = 0
			mergeSafetyMeta(&itemMeta, partialSafetyMeta(traceReasonNodeCountUnavailable))
		}
		traceID := safeIdentity(row.TraceID)
		if traceID == "" {
			mergeSafetyMeta(&itemMeta, partialSafetyMeta(traceReasonInvalidTraceIdentity))
		}
		if itemMeta.ReasonCode == "" {
			// The legacy Trace detail endpoint includes raw prompt/response and
			// has no equivalent Runtime content permission.  Do not emit a link.
			itemMeta.ReasonCode = traceReasonRawUnavailable
			itemMeta.Availability = v1.AvailabilityPartial
		}
		items = append(items, v1.TraceAggregateDTO{
			TraceID: traceID, Attempt: boundedRuntimeAttempt(facts.Attempt), Status: safeText(row.Status),
			TraceQuality: quality, DurationMs: maxRuntimeInt64(row.DurationMs),
			InputTokens: maxRuntimeInt64(int64(row.TotalInputTokens)), OutputTokens: maxRuntimeInt64(int64(row.TotalOutputTokens)),
			CostCNY: nonnegativeRuntimeFloat(row.EstimatedCostCNY), NodeCount: boundedRuntimeInt64ToInt(nodeCount),
			RawAvailable: false, DetailURL: "", ResourceMeta: itemMeta,
		})
		mergeSafetyMeta(&meta, itemMeta)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Attempt != items[j].Attempt {
			return items[i].Attempt < items[j].Attempt
		}
		return items[i].TraceID < items[j].TraceID
	})
	start := runtimePageOffset(page, pageSize)
	if start > len(items) {
		start = len(items)
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return v1.TracesRes{
		Items:        items[start:end],
		Page:         v1.PageMeta{Page: page, PageSize: pageSize, Total: int64(len(items)), HasNext: runtimePageHasNext(page, pageSize, int64(len(items)))},
		ResourceMeta: meta,
	}, nil
}

func (s *RuntimeService) loadEvidenceRun(ctx context.Context, runID string) (*mysql.WorkflowRun, error) {
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

func (s *RuntimeService) traceDAO() *mysql.TraceDAO {
	if s == nil {
		return nil
	}
	if s.TraceDAO != nil {
		return s.TraceDAO
	}
	if s.Store == nil || s.Store.DB() == nil {
		return nil
	}
	return mysql.NewTraceDAOWithDB(s.Store.DB())
}

func collectRuntimeEvidenceEvents(events []mysql.WorkflowEvent) runtimeEvidenceEventFacts {
	facts := runtimeEvidenceEventFacts{RetrievedAt: make(map[string]time.Time), Meta: completeSafetyMeta()}
	seen := make(map[string]struct{})
	for _, row := range events {
		if row.EventType != workflow.EventEvidenceRetrieved {
			continue
		}
		facts.Observed = true
		ids, canonical, ok, reason := decodeRuntimeEvidenceEvent(row)
		if !ok {
			mergeSafetyMeta(&facts.Meta, partialSafetyMeta(reason))
			continue
		}
		if !canonical {
			mergeSafetyMeta(&facts.Meta, partialSafetyMeta(evidenceReasonLegacyEvent))
		}
		for _, id := range ids {
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				facts.IDs = append(facts.IDs, id)
			}
			// Events are read in sequence order.  Keep the first canonical
			// retrieval timestamp, rather than using time.Now() on query.
			if _, exists := facts.RetrievedAt[id]; !exists {
				facts.RetrievedAt[id] = row.CreatedAt.UTC()
			}
		}
	}
	sort.Strings(facts.IDs)
	if facts.Observed && len(facts.IDs) == 0 {
		mergeSafetyMeta(&facts.Meta, partialSafetyMeta(evidenceReasonInvalidEvent))
	}
	return facts
}

func decodeRuntimeEvidenceEvent(row mysql.WorkflowEvent) ([]string, bool, bool, string) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(row.Payload), &root); err != nil || root == nil {
		return nil, false, false, evidenceReasonInvalidEvent
	}
	canonical := false
	data := root
	if raw, exists := root["data"]; exists {
		canonical = true
		var schema string
		if json.Unmarshal(root["schema"], &schema) != nil || schema != workflow.EventEnvelopeSchema {
			return nil, canonical, false, evidenceReasonInvalidEvent
		}
		if json.Unmarshal(raw, &data) != nil || data == nil {
			return nil, canonical, false, evidenceReasonInvalidEvent
		}
	}
	rawIDs, exists := data["evidence_ids"]
	if !exists {
		return nil, canonical, false, evidenceReasonInvalidEvent
	}
	var values []json.RawMessage
	if json.Unmarshal(rawIDs, &values) != nil {
		return nil, canonical, false, evidenceReasonInvalidEvent
	}
	ids := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	valid := true
	for _, raw := range values {
		var id string
		if json.Unmarshal(raw, &id) != nil || !validRuntimeEvidenceID(id) {
			valid = false
			continue
		}
		id = strings.TrimSpace(id)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if !valid {
		return ids, canonical, false, evidenceReasonInvalidEvent
	}
	return ids, canonical, true, ""
}

func validRuntimeEvidenceID(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "evidence-v1:") || len(value) != len("evidence-v1:")+sha256.Size*2 {
		return false
	}
	encoded := strings.TrimPrefix(value, "evidence-v1:")
	if strings.ToLower(encoded) != encoded {
		return false
	}
	_, err := hex.DecodeString(encoded)
	return err == nil
}

func runtimeEvidenceSourceMap(sources []mysql.RuntimeEvidenceSource) (map[string]mysql.RuntimeEvidenceSource, map[string]bool) {
	result := make(map[string]mysql.RuntimeEvidenceSource)
	duplicates := make(map[string]bool)
	for _, source := range sources {
		ref, ok, _ := reconstructRuntimeEvidenceIdentity("", source)
		if !ok {
			continue
		}
		if previous, exists := result[ref.EvidenceID]; exists && previous.ChunkID != source.ChunkID {
			duplicates[ref.EvidenceID] = true
			continue
		}
		result[ref.EvidenceID] = source
	}
	return result, duplicates
}

func reconstructRuntimeEvidenceIdentity(evidenceID string, source mysql.RuntimeEvidenceSource) (runtimeEvidenceIdentity, bool, string) {
	chunkID := strings.TrimSpace(source.ChunkID)
	docID := strings.TrimSpace(source.DocID)
	baseID := strings.TrimSpace(source.BaseID)
	if chunkID == "" || docID == "" || baseID == "" {
		return runtimeEvidenceIdentity{}, false, evidenceReasonEvidenceIdentity
	}
	contentHash, ok := mergeRuntimeSourceField(source.ChunkContentHash, source.DocContentHash)
	if !ok {
		return runtimeEvidenceIdentity{}, false, evidenceReasonEvidenceIdentity
	}
	sourceVersion, ok := mergeRuntimeSourceField(source.ChunkSourceVersion, source.DocSourceVersion)
	if !ok {
		return runtimeEvidenceIdentity{}, false, evidenceReasonEvidenceIdentity
	}
	accessScope, ok := mergeRuntimeSourceField(source.ChunkAccessScope, source.DocAccessScope)
	if !ok {
		return runtimeEvidenceIdentity{}, false, evidenceReasonEvidenceIdentity
	}
	indexedVersion, ok := mergeRuntimeIndexedVersion(source.ChunkIndexedVersion, source.DocIndexedVersion)
	if !ok || indexedVersion == 0 {
		return runtimeEvidenceIdentity{}, false, evidenceReasonEvidenceIdentity
	}
	if len(contentHash) != sha256.Size*2 || strings.ToLower(contentHash) != contentHash {
		return runtimeEvidenceIdentity{}, false, evidenceReasonEvidenceIdentity
	}
	if _, err := hex.DecodeString(contentHash); err != nil {
		return runtimeEvidenceIdentity{}, false, evidenceReasonEvidenceIdentity
	}
	identity := runtimeEvidenceIdentity{
		SourceType: "knowledge_chunk", SourceID: chunkID, BaseID: baseID, DocumentID: docID,
		ChunkID: chunkID, SourceVersion: sourceVersion, ContentHash: contentHash,
		AccessScope: accessScope, Indexed: indexedVersion,
	}
	identity.EvidenceID = evidence.StableEvidenceID(identity.SourceType, identity.SourceID, identity.BaseID, identity.DocumentID, identity.ChunkID, identity.SourceVersion, identity.ContentHash)
	if evidenceID != "" && identity.EvidenceID != evidenceID {
		return identity, false, evidenceReasonEvidenceIdentity
	}
	return identity, true, ""
}

func mergeRuntimeSourceField(chunk, doc string) (string, bool) {
	chunk, doc = strings.TrimSpace(chunk), strings.TrimSpace(doc)
	if chunk != "" && doc != "" && chunk != doc {
		return "", false
	}
	if chunk != "" {
		return chunk, true
	}
	return doc, doc != ""
}

func mergeRuntimeIndexedVersion(chunk, doc uint64) (uint64, bool) {
	if chunk != 0 && doc != 0 && chunk != doc {
		return 0, false
	}
	if chunk != 0 {
		return chunk, true
	}
	return doc, doc != 0
}

func runtimeEvidenceSourceAvailable(source mysql.RuntimeEvidenceSource) bool {
	return source.ChunkEnabled && source.DocEnabled && strings.EqualFold(strings.TrimSpace(source.IndexStatus), "completed") && source.DocDeletedAt == nil && source.ContentAvailable
}

func runtimeEvidenceSourceReason(source mysql.RuntimeEvidenceSource) string {
	if source.DocDeletedAt != nil {
		return "evidence_document_deleted"
	}
	if !source.ChunkEnabled || !source.DocEnabled {
		return "evidence_source_disabled"
	}
	if !strings.EqualFold(strings.TrimSpace(source.IndexStatus), "completed") {
		return "evidence_index_incomplete"
	}
	if !source.ContentAvailable {
		return "evidence_quote_unavailable"
	}
	return evidenceReasonEvidenceUnavailable
}

func mapRuntimeEvidenceMetadata(ref runtimeEvidenceIdentity, retrievedAt time.Time, source mysql.RuntimeEvidenceSource, score runtimeEvidenceScore) v1.EvidenceDTO {
	var retrieved *time.Time
	if !retrievedAt.IsZero() {
		value := retrievedAt.UTC()
		retrieved = &value
	}
	item := v1.EvidenceDTO{
		EvidenceID: ref.EvidenceID, SourceType: ref.SourceType, SourceID: ref.SourceID,
		BaseID: ref.BaseID, DocumentID: ref.DocumentID, ChunkID: ref.ChunkID,
		SourceVersion: ref.SourceVersion, ContentHash: ref.ContentHash, AccessScope: ref.AccessScope,
		IndexedVersion: formatRuntimeIndexedVersion(ref.Indexed), RetrievedAt: retrieved,
		QuoteAvailable: runtimeEvidenceSourceAvailable(source),
	}
	if score.Vector != nil {
		value := *score.Vector
		item.VectorScore = &value
	}
	if score.Rerank != nil {
		value := *score.Rerank
		item.RerankScore = &value
	}
	return item
}

func runtimeEvidenceRef(identity runtimeEvidenceIdentity, retrievedAt time.Time, quote string) evidence.EvidenceRef {
	return evidence.EvidenceRef{
		EvidenceID: identity.EvidenceID, SourceType: identity.SourceType, SourceID: identity.SourceID,
		BaseID: identity.BaseID, DocumentID: identity.DocumentID, ChunkID: identity.ChunkID,
		SourceVersion: identity.SourceVersion, ContentHash: identity.ContentHash,
		AccessScope: identity.AccessScope, IndexedVersion: identity.Indexed,
		RetrievedAt: retrievedAt, Quote: quote,
	}
}

func formatRuntimeIndexedVersion(value uint64) string {
	// The public v1 DTO intentionally keeps this field as a string for wire
	// compatibility with the existing Runtime contract.  Use decimal digits
	// without a lossy float conversion.
	return fmt.Sprintf("%d", value)
}

func collectRuntimeCitations(runID, outputPayload string) (map[string]runtimeCitationFact, v1.ResourceMeta) {
	result := make(map[string]runtimeCitationFact)
	meta := completeSafetyMeta()
	if strings.TrimSpace(outputPayload) == "" {
		return result, meta
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(outputPayload), &root); err != nil || root == nil {
		return result, partialSafetyMeta("invalid_output_payload")
	}
	raw, exists := root["citations"]
	if !exists {
		return result, meta
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return result, partialSafetyMeta("invalid_answer_citations")
	}
	for _, entry := range entries {
		var citation map[string]json.RawMessage
		if json.Unmarshal(entry, &citation) != nil || citation == nil {
			mergeSafetyMeta(&meta, partialSafetyMeta("invalid_answer_citations"))
			continue
		}
		fact := runtimeCitationFact{Complete: true}
		fact.RunID = rawString(citation["run_id"])
		fact.EvidenceID = rawString(citation["evidence_id"])
		fact.SourceVersion = rawString(citation["source_version"])
		fact.ContentHash = rawString(citation["content_hash"])
		fact.AccessScope = rawString(citation["access_scope"])
		fact.Indexed = rawUint64(citation["indexed_version"])
		if fact.RunID != runID || !validRuntimeEvidenceID(fact.EvidenceID) || fact.SourceVersion == "" || fact.ContentHash == "" || fact.AccessScope == "" {
			fact.Complete = false
			mergeSafetyMeta(&meta, partialSafetyMeta("invalid_answer_citation"))
		}
		if previous, exists := result[fact.EvidenceID]; exists && !sameRuntimeCitation(previous, fact) {
			previous.Complete = false
			result[fact.EvidenceID] = previous
			mergeSafetyMeta(&meta, partialSafetyMeta(evidenceReasonCitationMismatch))
			continue
		}
		if fact.EvidenceID != "" {
			result[fact.EvidenceID] = fact
		}
	}
	return result, meta
}

func sameRuntimeCitation(a, b runtimeCitationFact) bool {
	return a.RunID == b.RunID && a.EvidenceID == b.EvidenceID && a.SourceVersion == b.SourceVersion && a.ContentHash == b.ContentHash && a.AccessScope == b.AccessScope && a.Indexed == b.Indexed
}

func citationMatchesRuntimeEvidence(citation runtimeCitationFact, runID string, ref runtimeEvidenceIdentity) bool {
	return citation.Complete && citation.RunID == runID && citation.EvidenceID == ref.EvidenceID && citation.SourceVersion == ref.SourceVersion && citation.ContentHash == ref.ContentHash && citation.AccessScope == ref.AccessScope && citation.Indexed == ref.Indexed
}

func runtimeCitation(c runtimeCitationFact) evidence.Citation {
	return evidence.Citation{RunID: c.RunID, EvidenceID: c.EvidenceID, SourceVersion: c.SourceVersion, ContentHash: c.ContentHash, AccessScope: c.AccessScope, IndexedVersion: c.Indexed}
}

func evidenceExpired(retrievedAt, now time.Time) bool {
	return !retrievedAt.IsZero() && !now.IsZero() && now.Sub(retrievedAt) > evidence.DefaultEvidenceMaxAge
}

func runtimeCitationReason(err error) string {
	switch {
	case errors.Is(err, evidence.ErrExpiredEvidence):
		return evidenceReasonExpired
	case errors.Is(err, evidence.ErrInvalidCitation):
		return evidenceReasonCitationMismatch
	default:
		return evidenceReasonCitationMismatch
	}
}

func runtimeIdentityFromContext(ctx context.Context) (policy.Identity, error) {
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return policy.Identity{}, err
	}
	return identity, nil
}

func runtimeEvidenceScope(identity policy.Identity) evidence.Scope {
	accessScope := "user:" + identity.UserID
	return evidence.Scope{UserID: identity.UserID, Role: string(identity.Role), AccessScope: accessScope}
}

// runtimeCitationScope gives the shared Evidence validator the exact server
// scope for an admin request.  evidence.Scope intentionally has no wildcard:
// an admin's global policy is represented here only after the Run and source
// have already passed the service/DAO authorization checks.
func runtimeCitationScope(identity policy.Identity, accessScope string) evidence.Scope {
	scope := runtimeEvidenceScope(identity)
	if identity.Scope.All {
		scope.AccessScope = strings.TrimSpace(accessScope)
	}
	return scope
}

func runtimeAccessAllows(identity policy.Identity, accessScope string) bool {
	if identity.Scope.All {
		return true
	}
	return runtimeEvidenceScope(identity).Allows(accessScope)
}

func (s *RuntimeService) collectRuntimeEvidenceScores(ctx context.Context, runID string) (map[string]runtimeEvidenceScore, v1.ResourceMeta) {
	result := make(map[string]runtimeEvidenceScore)
	traces := s.traceDAO()
	if traces == nil {
		return result, partialSafetyMeta("retrieval_scores_not_observed")
	}
	rows, err := traces.ListRunsByWorkflowRunID(ctx, runID)
	if err != nil {
		return result, partialSafetyMeta("retrieval_trace_source_error")
	}
	attemptFacts := s.runtimeAttemptTraceFacts(ctx, runID)
	meta := completeSafetyMeta()
	for _, traceRun := range rows {
		traceFacts := parseRuntimeTraceFacts(traceRun.Tags)
		traceFacts = enrichRuntimeTraceFacts(traceFacts, traceRun.TraceID, attemptFacts)
		// A barrier-incomplete Trace remains visible in the diagnostic Trace
		// tab, but its retrieval scores are not authoritative Evidence quality.
		if !runtimeTraceCanGrade(traceFacts.TraceQuality) {
			continue
		}
		nodes, err := traces.ListRetrievalMetadataByTraceID(ctx, traceRun.TraceID)
		if err != nil {
			mergeSafetyMeta(&meta, partialSafetyMeta("retrieval_node_source_error"))
			continue
		}
		for _, node := range nodes {
			var summaries []struct {
				EvidenceID  string  `json:"evidence_id"`
				Score       float64 `json:"score"`
				RerankScore float64 `json:"rerank_score"`
			}
			if strings.TrimSpace(node.RetrievedDocs) == "" || json.Unmarshal([]byte(node.RetrievedDocs), &summaries) != nil {
				continue
			}
			for _, summary := range summaries {
				if !validRuntimeEvidenceID(summary.EvidenceID) {
					continue
				}
				current := result[summary.EvidenceID]
				if finiteNonnegativeRuntimeFloat(summary.Score) {
					value := summary.Score
					if current.Vector == nil || value > *current.Vector {
						current.Vector = &value
					}
				}
				if finiteNonnegativeRuntimeFloat(summary.RerankScore) {
					value := summary.RerankScore
					if current.Rerank == nil || value > *current.Rerank {
						current.Rerank = &value
					}
				}
				result[summary.EvidenceID] = current
			}
		}
	}
	return result, meta
}

func (s *RuntimeService) runtimeAttemptTraceFacts(ctx context.Context, runID string) map[string]mysql.WorkflowAttempt {
	result := make(map[string]mysql.WorkflowAttempt)
	if s == nil || s.Store == nil || s.Store.DB() == nil {
		return result
	}
	var rows []mysql.WorkflowAttempt
	if err := s.Store.DB().WithContext(ctx).Where("run_id = ?", runID).Find(&rows).Error; err != nil {
		return result
	}
	for _, row := range rows {
		if row.TraceID != nil && strings.TrimSpace(*row.TraceID) != "" {
			result[*row.TraceID] = row
		}
	}
	return result
}

func enrichRuntimeTraceFacts(facts runtimeTraceFacts, traceID string, attempts map[string]mysql.WorkflowAttempt) runtimeTraceFacts {
	attempt, ok := attempts[strings.TrimSpace(traceID)]
	if !ok {
		return facts
	}
	if facts.RunID == "" {
		facts.RunID = attempt.RunID
	}
	if facts.Attempt == 0 {
		facts.Attempt = attempt.Attempt
	}
	if facts.TraceQuality == "" {
		facts.TraceQuality = value(attempt.TraceQuality)
	}
	if facts.RuntimeVersion == "" {
		facts.RuntimeVersion = value(attempt.RuntimeVersion)
	}
	return facts
}

func runtimeTraceCanGrade(raw string) bool {
	return !strings.EqualFold(strings.TrimSpace(raw), "incomplete")
}

func parseRuntimeTraceFacts(raw string) runtimeTraceFacts {
	var tags map[string]any
	if json.Unmarshal([]byte(raw), &tags) != nil || tags == nil {
		return runtimeTraceFacts{}
	}
	facts := runtimeTraceFacts{RunID: stringFromAny(tags["run_id"]), RuntimeVersion: stringFromAny(tags["runtime_version"]), TraceQuality: stringFromAny(tags["trace_quality"]), ServerUserID: stringFromAny(tags["server_user_id"]), Valid: true}
	facts.Attempt, _ = uintFromAny(tags["attempt"])
	facts.LeaseGeneration, _ = uint64FromAny(tags["lease_generation"])
	if facts.RunID == "" || facts.ServerUserID == "" {
		facts.Valid = false
	}
	return facts
}

func mapRuntimeTraceQuality(raw string) (v1.DataQuality, v1.ResourceMeta) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "complete":
		return v1.DataQualityComplete, v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	case "incomplete":
		return v1.DataQualityPartial, partialSafetyMeta(traceReasonIncomplete)
	case "reconstructed":
		return v1.DataQualityReconstructed, v1.ResourceMeta{Availability: v1.AvailabilityPartial, DataQuality: v1.DataQualityReconstructed}
	default:
		return v1.DataQualityUnknown, unavailableRuntimeMeta(traceReasonQualityUnknown)
	}
}

func unavailableRuntimeMeta(reason string) v1.ResourceMeta {
	return v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: reason}
}

func (f runtimeEvidenceEventFacts) MetaOr(reason string) v1.ResourceMeta {
	meta := f.Meta
	if reason != "" && meta.ReasonCode == "" {
		meta = partialSafetyMeta(reason)
	}
	return meta
}

func rawString(raw json.RawMessage) string {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func rawUint64(raw json.RawMessage) uint64 {
	if len(raw) == 0 {
		return 0
	}
	var number uint64
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var floating float64
	if json.Unmarshal(raw, &floating) == nil && finiteNonnegativeRuntimeFloat(floating) && floating == math.Trunc(floating) && floating < math.Exp2(64) {
		return uint64(floating)
	}
	return 0
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func uintFromAny(value any) (uint, bool) {
	number, ok := uint64FromAny(value)
	if !ok || uint64(uint(number)) != number {
		return 0, false
	}
	return uint(number), true
}

func uint64FromAny(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint64:
		return typed, true
	case uint:
		return uint64(typed), true
	case int:
		if typed >= 0 {
			return uint64(typed), true
		}
	case float64:
		if finiteNonnegativeRuntimeFloat(typed) && typed == math.Trunc(typed) && typed < math.Exp2(64) {
			return uint64(typed), true
		}
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil && parsed >= 0 {
			return uint64(parsed), true
		}
	}
	return 0, false
}

func finiteNonnegativeRuntimeFloat(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func boundRuntimeEvidenceQuote(value string) string {
	runes := []rune(value)
	if len(runes) <= maxRuntimeEvidenceQuoteRunes {
		return value
	}
	return string(runes[:maxRuntimeEvidenceQuoteRunes]) + "..."
}

func maxRuntimeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func nonnegativeRuntimeFloat(value float64) float64 {
	if !finiteNonnegativeRuntimeFloat(value) {
		return 0
	}
	return value
}

func boundedRuntimeInt64ToInt(value int64) int {
	if value <= 0 {
		return 0
	}
	if uint64(value) > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value)
}

func boundedRuntimeAttempt(value uint) int {
	if uint64(value) > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value)
}
