package mysql

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

// RuntimeCheckpointFact is the metadata-only projection used by Runtime
// approval queries.  The opaque checkpoint bytes are never scanned into this
// type; BlobSHA256 is calculated by MySQL so the service can compare it with
// the persisted payload_sha256 without turning the query into a content read.
type RuntimeCheckpointFact struct {
	ID                       string     `gorm:"column:id"`
	RunID                    string     `gorm:"column:run_id"`
	CheckpointKey            string     `gorm:"column:checkpoint_key"`
	EinoCheckpointID         *string    `gorm:"column:eino_checkpoint_id"`
	PayloadSHA256            *string    `gorm:"column:payload_sha256"`
	BlobSHA256               *string    `gorm:"column:blob_sha256"`
	BlobLength               *int64     `gorm:"column:blob_length"`
	RuntimeVersion           *string    `gorm:"column:runtime_version"`
	RuntimeCompatibilityHash *string    `gorm:"column:runtime_compatibility_hash"`
	LeaseGeneration          *uint64    `gorm:"column:lease_generation"`
	CommittedAt              *time.Time `gorm:"column:committed_at"`
	ExpiresAt                *time.Time `gorm:"column:expires_at"`
}

// runtimeCheckpointFactProjection deliberately selects metadata and digest
// expressions only.  In particular, checkpoint_blob itself is not returned to
// the Go process or exposed through a Runtime DTO.
const runtimeCheckpointFactProjection = "workflow_checkpoints.id, workflow_checkpoints.run_id, " +
	"workflow_checkpoints.checkpoint_key, workflow_checkpoints.eino_checkpoint_id, " +
	"workflow_checkpoints.payload_sha256, " +
	"CASE WHEN workflow_checkpoints.checkpoint_blob IS NULL THEN NULL ELSE SHA2(workflow_checkpoints.checkpoint_blob, 256) END AS blob_sha256, " +
	"OCTET_LENGTH(workflow_checkpoints.checkpoint_blob) AS blob_length, " +
	"workflow_checkpoints.runtime_version, workflow_checkpoints.runtime_compatibility_hash, " +
	"workflow_checkpoints.lease_generation, workflow_checkpoints.committed_at, workflow_checkpoints.expires_at"

// GetRuntimeCheckpointFact loads the single Eino checkpoint identity bound to
// an owned durable Run.  The run ID and checkpoint ID are selectors only; the
// current server Identity is applied by runtimeScopedQuery before the row is
// visible.
func (s *GORMStore) GetRuntimeCheckpointFact(ctx context.Context, runID, checkpointID string) (*RuntimeCheckpointFact, error) {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(checkpointID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	q, err := s.runtimeScopedQuery(ctx, runID, &WorkflowCheckpoint{}, "workflow_checkpoints")
	if err != nil {
		return nil, err
	}
	var row RuntimeCheckpointFact
	if err := q.Where("workflow_checkpoints.run_id = ? AND workflow_checkpoints.eino_checkpoint_id = ?", runID, checkpointID).
		Select(runtimeCheckpointFactProjection).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// RuntimeCheckpointFactProjection returns the SQL shape for structural tests
// and keeps accidental wildcard/blob regressions visible to reviewers.
func RuntimeCheckpointFactProjection() string { return runtimeCheckpointFactProjection }
