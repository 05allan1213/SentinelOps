package evidence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validEvidence() EvidenceRef {
	ref := EvidenceRef{SourceType: "knowledge_chunk", SourceID: "chunk-1", BaseID: "base-1", DocumentID: "doc-1", ChunkID: "chunk-1", SourceVersion: "v2", ContentHash: strings.Repeat("a", 64), AccessScope: "user:u1", RetrievedAt: time.Unix(10, 0).UTC(), ContentPreview: "quoted text", VectorScore: .9}
	ref.EvidenceID = StableEvidenceID(ref.SourceType, ref.SourceID, ref.BaseID, ref.DocumentID, ref.ChunkID, ref.SourceVersion, ref.ContentHash)
	return ref
}

func TestStableEvidenceID(t *testing.T) {
	a, b := validEvidence(), validEvidence()
	if a.EvidenceID != b.EvidenceID {
		t.Fatal("same source identity must be stable")
	}
	b.SourceVersion = "v3"
	if a.EvidenceID == StableEvidenceID(b.SourceType, b.SourceID, b.BaseID, b.DocumentID, b.ChunkID, b.SourceVersion, b.ContentHash) {
		t.Fatal("version change must change identity")
	}
}

func TestEvidenceScope(t *testing.T) {
	s := Scope{UserID: "u1", Role: "viewer", KnowledgeBaseID: "base-1", AccessScope: "user:u1", IndexedVersion: 2, PolicyHash: "p1"}
	if !s.Allows("user:u1") || !s.Allows("public") || s.Allows("user:u2") {
		t.Fatal("scope predicate mismatch")
	}
	if s.Namespace() == (Scope{UserID: "u2", Role: s.Role, KnowledgeBaseID: s.KnowledgeBaseID, AccessScope: s.AccessScope, IndexedVersion: s.IndexedVersion, PolicyHash: s.PolicyHash}).Namespace() {
		t.Fatal("user must isolate namespace")
	}
}

func TestEvidenceRefContract(t *testing.T) {
	b, err := json.Marshal(validEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = UnmarshalEvidenceRef(b); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	delete(raw, "content_hash")
	b, _ = json.Marshal(raw)
	if _, err = UnmarshalEvidenceRef(b); err == nil {
		t.Fatal("missing content hash must be rejected")
	}
}
