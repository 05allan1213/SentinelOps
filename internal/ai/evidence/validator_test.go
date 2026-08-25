package evidence

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testCitationEvidence() EvidenceRef {
	ref := EvidenceRef{
		SourceType: "knowledge_chunk", SourceID: "chunk-31", BaseID: "base-31",
		DocumentID: "doc-31", ChunkID: "chunk-31", SourceVersion: "v31",
		ContentHash: strings.Repeat("b", 64), AccessScope: "user:u31",
		RetrievedAt: time.Unix(100, 0).UTC(), ContentPreview: "trusted quote",
		IndexedVersion: 7,
	}
	ref.EvidenceID = StableEvidenceID(ref.SourceType, ref.SourceID, ref.BaseID, ref.DocumentID, ref.ChunkID, ref.SourceVersion, ref.ContentHash)
	return ref
}

func TestCitationCurrentRunAndIntegrity(t *testing.T) {
	ref := testCitationEvidence()
	set := RunEvidence{RunID: "run-31", Scope: Scope{UserID: "u31", Role: "viewer", AccessScope: "user:u31", IndexedVersion: 7}, Refs: map[string]EvidenceRef{ref.EvidenceID: ref}}
	citation := Citation{RunID: "run-31", EvidenceID: ref.EvidenceID, SourceVersion: ref.SourceVersion, ContentHash: ref.ContentHash, AccessScope: ref.AccessScope, IndexedVersion: ref.IndexedVersion}
	if err := ValidateCitation(citation, set, time.Unix(101, 0).UTC()); err != nil {
		t.Fatalf("valid citation rejected: %v", err)
	}

	for name, mutate := range map[string]func(*Citation){
		"not-current-run": func(c *Citation) { c.RunID = "run-other" },
		"unknown-id":      func(c *Citation) { c.EvidenceID = "evidence-v1:" + strings.Repeat("f", 64) },
		"version-tamper":  func(c *Citation) { c.SourceVersion = "v32" },
		"hash-tamper":     func(c *Citation) { c.ContentHash = strings.Repeat("c", 64) },
		"scope-tamper":    func(c *Citation) { c.AccessScope = "public" },
		"index-tamper":    func(c *Citation) { c.IndexedVersion = 8 },
	} {
		candidate := citation
		mutate(&candidate)
		if err := ValidateCitation(candidate, set, time.Unix(101, 0).UTC()); !errors.Is(err, ErrInvalidCitation) {
			t.Errorf("%s: err=%v, want ErrInvalidCitation", name, err)
		}
	}

	expired := set
	if err := ValidateCitation(citation, expired, time.Unix(100, 0).UTC().Add(DefaultEvidenceMaxAge+time.Second)); !errors.Is(err, ErrExpiredEvidence) {
		t.Fatalf("expired evidence err=%v, want ErrExpiredEvidence", err)
	}
}

func TestCitationNoEvidenceIsInference(t *testing.T) {
	result, err := ValidateAnswer("根据现有信息建议继续观察。", RunEvidence{RunID: "run-31"}, time.Unix(101, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.Grounding != GroundingInference || !strings.Contains(result.Reason, "信息不足") {
		t.Fatalf("result=%#v", result)
	}
	if finalized, _, err := FinalizeAnswer("建议继续观察。", RunEvidence{RunID: "run-31"}, time.Unix(101, 0).UTC()); err != nil || !strings.Contains(finalized, "推断/信息不足") {
		t.Fatalf("finalized=%q err=%v", finalized, err)
	}
}

func TestCitationMarkerRejectsInvalidCurrentRunReference(t *testing.T) {
	ref := testCitationEvidence()
	set := RunEvidence{RunID: "run-31", Scope: Scope{UserID: "u31", AccessScope: "user:u31", IndexedVersion: 7}, Refs: map[string]EvidenceRef{ref.EvidenceID: ref}}
	answer := "结论 [evidence:" + ref.EvidenceID + "]"
	result, err := ValidateAnswer(answer, set, time.Unix(101, 0).UTC())
	if err != nil || result.Grounding != GroundingGrounded || len(result.Citations) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := ValidateAnswer("结论 [evidence:evidence-v1:"+strings.Repeat("f", 64)+"]", set, time.Unix(101, 0).UTC()); !errors.Is(err, ErrInvalidCitation) {
		t.Fatalf("invalid marker err=%v", err)
	}
}
