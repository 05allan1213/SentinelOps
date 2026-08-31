-- +goose NO TRANSACTION
-- +goose Up

-- Runtime query projections deliberately do not declare foreign keys.  The
-- workflow tables are expand-compatible and may be rebuilt independently.
CREATE TABLE workflow_attempts (
    id VARCHAR(128) NOT NULL,
    run_id VARCHAR(64) NOT NULL,
    attempt INT UNSIGNED NOT NULL,
    mode VARCHAR(32) NULL,
    status VARCHAR(32) NULL,
    current_phase VARCHAR(32) NULL,
    worker_id VARCHAR(128) NULL,
    lease_generation BIGINT UNSIGNED NULL,
    runtime_version VARCHAR(128) NULL,
    run_compatibility_hash CHAR(64) NULL,
    checkpoint_compatibility_hash CHAR(64) NULL,
    executing_worker_fingerprint CHAR(64) NULL,
    trace_id VARCHAR(64) NULL,
    operation_id VARCHAR(128) NULL,
    retry_count INT UNSIGNED NULL,
    failover_count INT UNSIGNED NULL,
    failure_code VARCHAR(64) NULL,
    failure_message_redacted TEXT NULL,
    usage_quality VARCHAR(32) NULL,
    trace_quality VARCHAR(32) NULL,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uidx_workflow_attempts_run_attempt (run_id, attempt),
    KEY idx_workflow_attempts_run_status (run_id, status),
    KEY idx_workflow_attempts_worker_generation (worker_id, lease_generation)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE workflow_events
    ADD COLUMN operation_id VARCHAR(128) NULL AFTER trace_id,
    ADD COLUMN command_action VARCHAR(32) NULL AFTER operation_id,
    ADD COLUMN idempotency_key_digest CHAR(64) NULL AFTER command_action,
    ADD COLUMN request_fingerprint CHAR(64) NULL AFTER idempotency_key_digest,
    ADD COLUMN actor_id VARCHAR(128) NULL AFTER request_fingerprint,
    ADD COLUMN reason_redacted TEXT NULL AFTER actor_id,
    ADD COLUMN correlation_seq BIGINT UNSIGNED NULL AFTER reason_redacted;

-- Command writers reserve the digest only on operation.accepted. MySQL permits
-- multiple NULL values, so started/terminal lifecycle events remain correlated
-- by operation_id without colliding with the accepted command row.
CREATE UNIQUE INDEX uidx_workflow_events_run_idempotency
    ON workflow_events(run_id, idempotency_key_digest);
CREATE INDEX idx_workflow_events_operation_seq
    ON workflow_events(operation_id, seq);
CREATE INDEX idx_workflow_events_active_operation
    ON workflow_events(run_id, command_action, created_at);

CREATE TABLE runtime_worker_snapshots (
    worker_id VARCHAR(128) NOT NULL,
    heartbeat_at DATETIME(3) NULL,
    runtime_version VARCHAR(128) NULL,
    runtime_compatibility_hash CHAR(64) NULL,
    configured_catalog_hash CHAR(64) NULL,
    observed_mcp_json JSON NULL,
    observed_skill_json JSON NULL,
    active_run_id VARCHAR(64) NULL,
    active_generation BIGINT UNSIGNED NULL,
    status VARCHAR(32) NULL,
    last_error_redacted TEXT NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    PRIMARY KEY (worker_id),
    KEY idx_runtime_worker_snapshots_active_run (active_run_id, active_generation),
    KEY idx_runtime_worker_snapshots_status_heartbeat (status, heartbeat_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE IF EXISTS runtime_worker_snapshots;
DROP INDEX idx_workflow_events_active_operation ON workflow_events;
DROP INDEX idx_workflow_events_operation_seq ON workflow_events;
DROP INDEX uidx_workflow_events_run_idempotency ON workflow_events;
ALTER TABLE workflow_events
    DROP COLUMN correlation_seq,
    DROP COLUMN reason_redacted,
    DROP COLUMN actor_id,
    DROP COLUMN request_fingerprint,
    DROP COLUMN idempotency_key_digest,
    DROP COLUMN command_action,
    DROP COLUMN operation_id;
DROP TABLE IF EXISTS workflow_attempts;
