-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE workflow_checkpoints
    ADD COLUMN eino_checkpoint_id VARCHAR(128) NULL AFTER snapshot_json,
    ADD COLUMN checkpoint_blob LONGBLOB NULL AFTER eino_checkpoint_id,
    ADD COLUMN payload_sha256 CHAR(64) NULL AFTER checkpoint_blob,
    ADD COLUMN runtime_version VARCHAR(128) NULL AFTER payload_sha256,
    ADD COLUMN runtime_compatibility_hash CHAR(64) NULL AFTER runtime_version,
    ADD COLUMN lease_generation BIGINT UNSIGNED NULL AFTER runtime_compatibility_hash,
    ADD COLUMN committed_at DATETIME(3) NULL AFTER lease_generation,
    ADD COLUMN expires_at DATETIME(3) NULL AFTER committed_at;

CREATE UNIQUE INDEX uidx_workflow_checkpoints_eino_id ON workflow_checkpoints(eino_checkpoint_id);
CREATE INDEX idx_workflow_checkpoints_run_expiry ON workflow_checkpoints(run_id, expires_at);

-- +goose Down
DROP INDEX idx_workflow_checkpoints_run_expiry ON workflow_checkpoints;
DROP INDEX uidx_workflow_checkpoints_eino_id ON workflow_checkpoints;
ALTER TABLE workflow_checkpoints
    DROP COLUMN expires_at,
    DROP COLUMN committed_at,
    DROP COLUMN lease_generation,
    DROP COLUMN runtime_compatibility_hash,
    DROP COLUMN runtime_version,
    DROP COLUMN payload_sha256,
    DROP COLUMN checkpoint_blob,
    DROP COLUMN eino_checkpoint_id;
