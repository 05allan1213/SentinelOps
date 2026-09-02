package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"

	"gorm.io/gorm"
)

func TestRuntimeEvidenceSourceProjectionIsMetadataOnlyAndScoped(t *testing.T) {
	store, db := runtimeQueryStore(t, "runtime_evidence_projection")
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedRuntimeEvidenceSources(t, db, now)

	alice := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "alice", Role: policy.RoleViewer, Scope: policy.Scope{UserID: "alice"},
	})
	rows, err := store.ListRuntimeEvidenceSources(alice)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]RuntimeEvidenceSource, len(rows))
	for _, row := range rows {
		seen[row.ChunkID] = row
		if row.ContentPreview != "" {
			t.Fatalf("metadata projection loaded content for %q", row.ChunkID)
		}
	}
	if len(seen) != 2 || seen["chunk-public"].ChunkID == "" || seen["chunk-alice"].ChunkID == "" {
		t.Fatalf("alice source scope = %#v, want public and own source", seen)
	}
	if _, exists := seen["chunk-bob"]; exists {
		t.Fatalf("cross-user source leaked into metadata projection: %#v", seen)
	}

	if _, err := store.GetRuntimeEvidenceSource(alice, "chunk-bob"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user source err=%v, want record not found", err)
	}
	current, err := store.GetRuntimeEvidenceSource(alice, "chunk-alice")
	if err != nil {
		t.Fatal(err)
	}
	if current.ContentPreview != "alice source content" {
		t.Fatalf("explicit source content=%q", current.ContentPreview)
	}

	admin := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "admin", Role: policy.RoleAdmin, Scope: policy.Scope{All: true},
	})
	adminRows, err := store.ListRuntimeEvidenceSources(admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(adminRows) != 3 {
		t.Fatalf("admin source count=%d, want 3", len(adminRows))
	}
}

func seedRuntimeEvidenceSources(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()
	for _, source := range []struct {
		docID, chunkID, scope, content string
	}{
		{docID: "doc-public", chunkID: "chunk-public", scope: "public", content: "public source content"},
		{docID: "doc-alice", chunkID: "chunk-alice", scope: "user:alice", content: "alice source content"},
		{docID: "doc-bob", chunkID: "chunk-bob", scope: "user:bob", content: "bob source content"},
	} {
		document := KnowledgeDocument{
			ID: source.docID, BaseID: "base-" + source.docID, Name: source.docID + ".md",
			FilePath: "/tmp/" + source.docID + ".md", FileSize: int64(len(source.content)), FileType: "md",
			ChunkStrategy: "sliding_window", ChunkConfig: "{}", ChunkCount: 1, IndexedChunks: 1, IndexStatus: "completed",
			Enabled: true, ContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SourceVersion: "source-v1", AccessScope: source.scope, IndexedVersion: 7,
			IndexedAt: &now, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&document).Error; err != nil {
			t.Fatalf("create document %s: %v", source.docID, err)
		}
		chunk := KnowledgeChunk{
			ID: source.chunkID, DocID: source.docID, ChunkIndex: 0, ContentPreview: source.content,
			CharCount: len([]rune(source.content)), Enabled: true,
			ContentHash:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			SourceVersion: "source-v1", AccessScope: source.scope, IndexedVersion: 7,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&chunk).Error; err != nil {
			t.Fatalf("create chunk %s: %v", source.chunkID, err)
		}
	}
}
