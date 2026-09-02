package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"
)

func runtimeEvidenceFixture() (string, mysql.RuntimeEvidenceSource) {
	hash := strings.Repeat("a", 64)
	source := mysql.RuntimeEvidenceSource{
		ChunkID:             "chunk-b105",
		DocID:               "doc-b105",
		BaseID:              "base-b105",
		ChunkContentHash:    hash,
		DocContentHash:      hash,
		ChunkSourceVersion:  "source-v1",
		DocSourceVersion:    "source-v1",
		ChunkAccessScope:    "user:alice",
		DocAccessScope:      "user:alice",
		ChunkIndexedVersion: 7,
		DocIndexedVersion:   7,
		ChunkEnabled:        true,
		DocEnabled:          true,
		IndexStatus:         "completed",
		ContentAvailable:    true,
		ContentPreview:      "untrusted source text",
	}
	return evidence.StableEvidenceID("knowledge_chunk", source.ChunkID, source.BaseID, source.DocID, source.ChunkID, source.ChunkSourceVersion, hash), source
}

func runtimeScopedIdentity(userID string, role policy.Role, all bool) policy.Identity {
	return policy.Identity{UserID: userID, Role: role, Scope: policy.Scope{UserID: userID, All: all}}
}

func TestEvidenceDTODefaultOmitsQuote(t *testing.T) {
	evidenceID, source := runtimeEvidenceFixture()
	identity, ok, reason := reconstructRuntimeEvidenceIdentity(evidenceID, source)
	if !ok {
		t.Fatalf("reconstruct evidence identity: ok=%t reason=%q", ok, reason)
	}
	item := mapRuntimeEvidenceMetadata(identity, time.Unix(100, 0).UTC(), source, runtimeEvidenceScore{})
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"quote":`) || strings.Contains(string(encoded), `"content_preview":`) {
		t.Fatalf("metadata DTO exposed source content: %s", encoded)
	}
	if item.QuoteAvailable != source.ContentAvailable {
		t.Fatalf("quote availability=%t, want %t", item.QuoteAvailable, source.ContentAvailable)
	}
}

func TestEvidenceExpansionRevalidatesRunScope(t *testing.T) {
	evidenceID, source := runtimeEvidenceFixture()
	ref, ok, reason := reconstructRuntimeEvidenceIdentity(evidenceID, source)
	if !ok {
		t.Fatalf("reconstruct evidence identity: ok=%t reason=%q", ok, reason)
	}
	matching := runtimeCitationFact{RunID: "run-b105", EvidenceID: evidenceID, SourceVersion: ref.SourceVersion, ContentHash: ref.ContentHash, AccessScope: ref.AccessScope, Indexed: ref.Indexed, Complete: true}
	if !citationMatchesRuntimeEvidence(matching, "run-b105", ref) {
		t.Fatal("matching citation was rejected")
	}
	for name, mutate := range map[string]func(*runtimeCitationFact){
		"scope":   func(c *runtimeCitationFact) { c.AccessScope = "user:bob" },
		"version": func(c *runtimeCitationFact) { c.SourceVersion = "source-v2" },
		"hash":    func(c *runtimeCitationFact) { c.ContentHash = strings.Repeat("b", 64) },
		"run":     func(c *runtimeCitationFact) { c.RunID = "other-run" },
	} {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			candidate := matching
			mutate(&candidate)
			if citationMatchesRuntimeEvidence(candidate, "run-b105", ref) {
				t.Fatalf("tampered %s citation accepted", name)
			}
		})
	}
}

func TestEvidenceHashVersionTamperRejected(t *testing.T) {
	evidenceID, source := runtimeEvidenceFixture()
	if _, ok, reason := reconstructRuntimeEvidenceIdentity(evidenceID, source); !ok {
		t.Fatalf("valid source rejected: %q", reason)
	}
	tamperedHash := source
	tamperedHash.ChunkContentHash = strings.Repeat("b", 64)
	if _, ok, reason := reconstructRuntimeEvidenceIdentity(evidenceID, tamperedHash); ok || reason == "" {
		t.Fatalf("content hash tamper accepted: ok=%t reason=%q", ok, reason)
	}
	tamperedVersion := source
	tamperedVersion.DocSourceVersion = "source-v2"
	if _, ok, reason := reconstructRuntimeEvidenceIdentity(evidenceID, tamperedVersion); ok || reason == "" {
		t.Fatalf("source version tamper accepted: ok=%t reason=%q", ok, reason)
	}
	tamperedIndexedVersion := source
	tamperedIndexedVersion.ChunkIndexedVersion = 8
	if _, ok, reason := reconstructRuntimeEvidenceIdentity(evidenceID, tamperedIndexedVersion); ok || reason == "" {
		t.Fatalf("indexed version tamper accepted: ok=%t reason=%q", ok, reason)
	}
}

func TestTraceAggregateMapsAttemptAndQuality(t *testing.T) {
	facts := parseRuntimeTraceFacts(`{"run_id":"run-b105","server_user_id":"alice","attempt":2,"lease_generation":9,"trace_quality":"complete"}`)
	if !facts.Valid || facts.RunID != "run-b105" || facts.Attempt != 2 || facts.LeaseGeneration != 9 || facts.TraceQuality != "complete" {
		t.Fatalf("trace facts=%+v", facts)
	}
	quality, meta := mapRuntimeTraceQuality(facts.TraceQuality)
	if quality != v1.DataQualityComplete || meta.Availability != v1.AvailabilityAvailable {
		t.Fatalf("trace quality=%q meta=%+v", quality, meta)
	}
	if quality, meta = mapRuntimeTraceQuality("incomplete"); quality != v1.DataQualityPartial || meta.ReasonCode != traceReasonIncomplete {
		t.Fatalf("incomplete trace quality=%q meta=%+v", quality, meta)
	}
	qualityFromAttempt := enrichRuntimeTraceFacts(
		parseRuntimeTraceFacts(`{"run_id":"run-b105","server_user_id":"alice","attempt":2}`),
		"trace-b105",
		map[string]mysql.WorkflowAttempt{"trace-b105": {RunID: "run-b105", Attempt: 2, TraceQuality: strp("complete"), RuntimeVersion: strp("runtime-v1")}},
	)
	if qualityFromAttempt.TraceQuality != "complete" || qualityFromAttempt.RuntimeVersion != "runtime-v1" {
		t.Fatalf("attempt fallback facts=%+v", qualityFromAttempt)
	}
}

func TestIncompleteTraceExcludedFromEvidenceGrade(t *testing.T) {
	if runtimeTraceCanGrade("incomplete") {
		t.Fatal("incomplete Trace was eligible for Evidence grade")
	}
	if !runtimeTraceCanGrade("complete") || !runtimeTraceCanGrade("unknown") {
		t.Fatal("non-incomplete Trace was unexpectedly excluded")
	}
}

func TestTraceCrossScopeForbidden(t *testing.T) {
	alice := policy.WithIdentity(context.Background(), runtimeScopedIdentity("alice", policy.RoleViewer, false))
	if !runtimeAccessAllows(mustRuntimeIdentity(t, alice), "user:alice") {
		t.Fatal("owner Scope was rejected")
	}
	if runtimeAccessAllows(mustRuntimeIdentity(t, alice), "user:bob") {
		t.Fatal("cross-owner Trace Scope was accepted")
	}
	admin := policy.WithIdentity(context.Background(), runtimeScopedIdentity("admin", policy.RoleAdmin, true))
	if !runtimeAccessAllows(mustRuntimeIdentity(t, admin), "user:bob") {
		t.Fatal("admin global Scope was rejected")
	}
}

func mustRuntimeIdentity(t *testing.T, ctx context.Context) policy.Identity {
	t.Helper()
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
