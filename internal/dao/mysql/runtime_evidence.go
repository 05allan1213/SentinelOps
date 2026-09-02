package mysql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"

	"gorm.io/gorm"
)

// RuntimeEvidenceSource is the metadata needed to reconstruct an EvidenceRef
// from the persisted knowledge source.  ContentPreview is populated only when
// a caller has already passed the explicit content-expansion gate.
//
// The evidence event stores a stable Evidence ID rather than a copy of this
// structure.  Keeping the source lookup here makes that limitation explicit:
// a missing or changed source can be reported as partial by the service,
// rather than silently manufacturing a quote from an event ID.
type RuntimeEvidenceSource struct {
	ChunkID             string     `gorm:"column:chunk_id"`
	DocID               string     `gorm:"column:doc_id"`
	BaseID              string     `gorm:"column:base_id"`
	ChunkContentHash    string     `gorm:"column:chunk_content_hash"`
	DocContentHash      string     `gorm:"column:doc_content_hash"`
	ChunkSourceVersion  string     `gorm:"column:chunk_source_version"`
	DocSourceVersion    string     `gorm:"column:doc_source_version"`
	ChunkAccessScope    string     `gorm:"column:chunk_access_scope"`
	DocAccessScope      string     `gorm:"column:doc_access_scope"`
	ChunkIndexedVersion uint64     `gorm:"column:chunk_indexed_version"`
	DocIndexedVersion   uint64     `gorm:"column:doc_indexed_version"`
	ChunkEnabled        bool       `gorm:"column:chunk_enabled"`
	DocEnabled          bool       `gorm:"column:doc_enabled"`
	IndexStatus         string     `gorm:"column:index_status"`
	IndexedAt           *time.Time `gorm:"column:indexed_at"`
	DocDeletedAt        *time.Time `gorm:"column:doc_deleted_at"`
	ContentAvailable    bool       `gorm:"column:content_available"`
	ContentPreview      string     `gorm:"column:content_preview"`
}

// AccessScope returns the source ACL only when both chunk and document agree.
// An empty result is intentionally treated as an invalid/mismatched source by
// the Runtime service.
func (s RuntimeEvidenceSource) AccessScope() string {
	chunk, doc := strings.TrimSpace(s.ChunkAccessScope), strings.TrimSpace(s.DocAccessScope)
	if chunk != "" && doc != "" && chunk != doc {
		return ""
	}
	if chunk != "" {
		return chunk
	}
	return doc
}

// runtimeEvidenceSourceSelect is deliberately an explicit projection.  In
// particular, ListRuntimeEvidenceSources never selects ContentPreview, so a
// metadata-only Evidence request cannot accidentally carry source text into a
// service response or a query cache.
const runtimeEvidenceSourceSelect = "" +
	"kc.id AS chunk_id, kc.doc_id AS doc_id, kd.base_id AS base_id, " +
	"kc.content_hash AS chunk_content_hash, kd.content_hash AS doc_content_hash, " +
	"kc.source_version AS chunk_source_version, kd.source_version AS doc_source_version, " +
	"kc.access_scope AS chunk_access_scope, kd.access_scope AS doc_access_scope, " +
	"kc.indexed_version AS chunk_indexed_version, kd.indexed_version AS doc_indexed_version, " +
	"kc.enabled AS chunk_enabled, kd.enabled AS doc_enabled, kd.index_status AS index_status, " +
	"kd.indexed_at AS indexed_at, kd.deleted_at AS doc_deleted_at"

func runtimeEvidenceSourceQuery(ctx context.Context, store *GORMStore, includeContent bool) (*gorm.DB, error) {
	if ctx == nil || store == nil || store.db == nil {
		return nil, fmt.Errorf("database context/store is required")
	}
	// IdentityFromContext is intentionally required even though the service
	// performs the Run authorization first.  It prevents this low-level source
	// query from becoming an unauthenticated content oracle for future callers.
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	selectList := runtimeEvidenceSourceSelect
	if includeContent {
		selectList += ", CASE WHEN kc.content_preview IS NOT NULL AND kc.content_preview <> '' THEN 1 ELSE 0 END AS content_available, kc.content_preview AS content_preview"
	} else {
		selectList += ", CASE WHEN kc.content_preview IS NOT NULL AND kc.content_preview <> '' THEN 1 ELSE 0 END AS content_available, '' AS content_preview"
	}
	query := store.db.WithContext(ctx).Unscoped().Table("knowledge_chunks AS kc").
		Select(selectList).
		Joins("JOIN knowledge_documents AS kd ON kd.id = kc.doc_id")
	if !identity.Scope.All {
		// Keep private source metadata out of the service query entirely.  The
		// service still verifies that chunk/document fields agree before it
		// reconstructs an EvidenceRef, so an inconsistent pair is partial rather
		// than an ACL bypass.
		allowed := []string{"public", "user:" + identity.Scope.UserID, "role:" + string(identity.Role)}
		query = query.Where("kc.access_scope IN ? AND kd.access_scope IN ?", allowed, allowed)
	}
	return query, nil
}

// ListRuntimeEvidenceSources returns source metadata for all known chunks.  A
// stable Evidence ID is a one-way hash, so the service must compare requested
// IDs against reconstructed source identities; querying by the event ID alone
// cannot recover the source fields.  This method intentionally includes
// disabled, incomplete and soft-deleted documents so the service can return an
// explicit partial/unavailable state instead of reporting a false empty list.
func (s *GORMStore) ListRuntimeEvidenceSources(ctx context.Context) ([]RuntimeEvidenceSource, error) {
	query, err := runtimeEvidenceSourceQuery(ctx, s, false)
	if err != nil {
		return nil, err
	}
	var rows []RuntimeEvidenceSource
	if err := query.Order("kc.id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list runtime evidence sources: %w", err)
	}
	return rows, nil
}

// GetRuntimeEvidenceSource loads one source's current content after the
// service has matched its stable Evidence ID and checked the current Scope.
func (s *GORMStore) GetRuntimeEvidenceSource(ctx context.Context, chunkID string) (*RuntimeEvidenceSource, error) {
	if strings.TrimSpace(chunkID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	query, err := runtimeEvidenceSourceQuery(ctx, s, true)
	if err != nil {
		return nil, err
	}
	var row RuntimeEvidenceSource
	// Take avoids GORM deriving an ORDER BY from the first projected struct
	// field (chunk_id), which is an alias rather than a physical column.
	if err := query.Where("kc.id = ?", chunkID).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
