-- +goose NO TRANSACTION
-- +goose Up

-- 当前 GORM Schema 的不可变基线。CREATE IF NOT EXISTS 使同一 migration 同时支持空库和既有库。
CREATE TABLE IF NOT EXISTS events (
    id VARCHAR(64) NOT NULL,
    title VARCHAR(256) NOT NULL,
    event_type VARCHAR(32) NULL,
    dedup_key VARCHAR(64) NULL,
    severity VARCHAR(32) NULL,
    source VARCHAR(128) NULL,
    status VARCHAR(32) NULL DEFAULT 'new',
    cve_id VARCHAR(64) NULL,
    risk_score DOUBLE NULL,
    metadata JSON NULL,
    raw_payload TEXT NULL,
    indexed_at DATETIME(3) NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_events_event_type (event_type),
    KEY idx_events_dedup_key (dedup_key),
    KEY idx_events_severity (severity),
    KEY idx_events_source (source),
    KEY idx_events_status (status),
    KEY idx_events_cve_id (cve_id),
    KEY idx_events_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS subscriptions (
    id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    url VARCHAR(512) NOT NULL,
    type VARCHAR(32) NULL,
    cron_expr VARCHAR(64) NULL,
    enabled TINYINT(1) NULL DEFAULT 1,
    last_fetch_at DATETIME(3) NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_subscriptions_type (type),
    KEY idx_subscriptions_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS reports (
    id VARCHAR(64) NOT NULL,
    title VARCHAR(256) NOT NULL,
    content LONGTEXT NULL,
    type VARCHAR(32) NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_reports_type (type),
    KEY idx_reports_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) NOT NULL,
    username VARCHAR(64) NOT NULL,
    password VARCHAR(256) NOT NULL,
    role VARCHAR(32) NULL DEFAULT 'user',
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_users_username (username),
    KEY idx_users_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS settings (
    `key` VARCHAR(128) NOT NULL,
    value TEXT NULL,
    updated_at DATETIME(3) NULL,
    PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS query_term_mappings (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    source_term VARCHAR(128) NOT NULL,
    target_term VARCHAR(256) NOT NULL,
    priority BIGINT NULL DEFAULT 0,
    enabled TINYINT(1) NULL DEFAULT 1,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_query_term_mappings_source_term (source_term),
    KEY idx_query_term_mappings_priority (priority),
    KEY idx_query_term_mappings_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS agent_trace_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    trace_id VARCHAR(36) NOT NULL,
    trace_name VARCHAR(200) NULL,
    entry_point VARCHAR(200) NULL,
    session_id VARCHAR(100) NULL,
    message_index BIGINT NULL DEFAULT 0,
    query_text TEXT NULL,
    status VARCHAR(20) NULL DEFAULT 'running',
    error_message VARCHAR(1000) NULL,
    error_code VARCHAR(50) NULL,
    start_time DATETIME(3) NULL,
    end_time DATETIME(3) NULL,
    duration_ms BIGINT NULL,
    total_input_tokens BIGINT NULL DEFAULT 0,
    cached_input_tokens BIGINT NULL DEFAULT 0,
    total_output_tokens BIGINT NULL DEFAULT 0,
    reasoning_tokens BIGINT NULL DEFAULT 0,
    estimated_cost_cny DECIMAL(10,6) NULL,
    tags TEXT NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_agent_trace_runs_trace_id (trace_id),
    KEY idx_agent_trace_runs_session_id (session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS agent_trace_nodes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    trace_id VARCHAR(36) NOT NULL,
    node_id VARCHAR(36) NOT NULL,
    parent_node_id VARCHAR(36) NULL,
    depth BIGINT NULL DEFAULT 0,
    node_type VARCHAR(50) NULL,
    node_name VARCHAR(200) NULL,
    status VARCHAR(20) NULL DEFAULT 'running',
    error_message VARCHAR(1000) NULL,
    error_code VARCHAR(50) NULL,
    error_type VARCHAR(100) NULL,
    start_time DATETIME(3) NULL,
    end_time DATETIME(3) NULL,
    duration_ms BIGINT NULL,
    model_name VARCHAR(100) NULL,
    input_tokens BIGINT NULL,
    cached_input_tokens BIGINT NULL,
    output_tokens BIGINT NULL,
    reasoning_tokens BIGINT NULL,
    cost_cny DECIMAL(10,6) NULL,
    prompt_text LONGTEXT NULL,
    completion_text TEXT NULL,
    query_text TEXT NULL,
    retrieved_docs TEXT NULL,
    final_top_k BIGINT NULL,
    cache_hit TINYINT(1) NULL,
    avg_vector_score DECIMAL(6,4) NULL DEFAULT 0,
    max_vector_score DECIMAL(6,4) NULL DEFAULT 0,
    doc_count BIGINT NULL DEFAULT 0,
    rerank_used TINYINT(1) NULL DEFAULT 0,
    avg_rerank_score DECIMAL(6,4) NULL DEFAULT 0,
    metadata TEXT NULL,
    created_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_agent_trace_nodes_node_id (node_id),
    KEY idx_agent_trace_nodes_trace_id (trace_id),
    KEY idx_agent_trace_nodes_parent_node_id (parent_node_id),
    KEY idx_agent_trace_nodes_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS knowledge_bases (
    id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NULL,
    doc_count BIGINT NULL DEFAULT 0,
    chunk_count BIGINT NULL DEFAULT 0,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_knowledge_bases_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS knowledge_documents (
    id VARCHAR(64) NOT NULL,
    base_id VARCHAR(64) NOT NULL,
    name VARCHAR(256) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    file_size BIGINT NOT NULL,
    file_type VARCHAR(32) NOT NULL,
    file_hash VARCHAR(64) NULL,
    chunk_strategy VARCHAR(32) NOT NULL,
    chunk_config JSON NULL,
    chunk_count BIGINT NULL DEFAULT 0,
    indexed_chunks BIGINT NULL DEFAULT 0,
    indexed_at DATETIME(3) NULL,
    index_duration_ms BIGINT NULL DEFAULT 0,
    index_status VARCHAR(32) NULL DEFAULT 'pending',
    index_error TEXT NULL,
    enabled TINYINT(1) NULL DEFAULT 1,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_knowledge_documents_base_id (base_id),
    KEY idx_knowledge_documents_file_type (file_type),
    KEY idx_knowledge_documents_file_hash (file_hash),
    KEY idx_knowledge_documents_index_status (index_status),
    KEY idx_knowledge_documents_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS knowledge_chunks (
    id VARCHAR(64) NOT NULL,
    doc_id VARCHAR(64) NOT NULL,
    chunk_index BIGINT NOT NULL,
    content_preview TEXT NULL,
    section_title VARCHAR(256) NULL,
    char_count BIGINT NOT NULL,
    enabled TINYINT(1) NULL DEFAULT 1,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_knowledge_chunks_doc_id (doc_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS message_feedbacks (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id VARCHAR(64) NULL,
    session_id VARCHAR(64) NULL,
    message_index BIGINT NOT NULL,
    vote BIGINT NOT NULL,
    reasons JSON NULL,
    created_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_message_feedbacks_user_id (user_id),
    KEY idx_message_feedbacks_session_id (session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS user_preferences (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id VARCHAR(64) NOT NULL,
    output_style VARCHAR(32) NULL DEFAULT 'detailed',
    analysis_depth VARCHAR(32) NULL DEFAULT 'standard',
    focus_areas JSON NULL,
    inferred_note TEXT NULL,
    updated_at DATETIME(3) NULL,
    created_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_user_preferences_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS ops_playbooks (
    id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(500) NULL,
    enabled TINYINT(1) NULL DEFAULT 1,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_ops_playbooks_enabled (enabled),
    KEY idx_ops_playbooks_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS ops_runs (
    id VARCHAR(64) NOT NULL,
    playbook_id VARCHAR(64) NOT NULL,
    event_id VARCHAR(64) NULL,
    status VARCHAR(32) NULL DEFAULT 'running',
    error_msg TEXT NULL,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    duration_ms BIGINT NULL DEFAULT 0,
    created_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_ops_runs_playbook_id (playbook_id),
    KEY idx_ops_runs_event_id (event_id),
    KEY idx_ops_runs_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS ops_run_steps (
    id VARCHAR(64) NOT NULL,
    run_id VARCHAR(64) NOT NULL,
    step_id VARCHAR(64) NOT NULL,
    step_order BIGINT NOT NULL,
    action_type VARCHAR(64) NULL,
    resolved_params JSON NULL,
    status VARCHAR(32) NULL DEFAULT 'running',
    output TEXT NULL,
    error_msg TEXT NULL,
    retry_count BIGINT NULL DEFAULT 0,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    duration_ms BIGINT NULL DEFAULT 0,
    created_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_ops_run_steps_run_id (run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS ops_protected_assets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    asset_type VARCHAR(32) NOT NULL,
    value VARCHAR(256) NOT NULL,
    reason VARCHAR(500) NULL,
    created_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_protected_asset_type_value (asset_type, value)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS workflow_runs (
    id VARCHAR(64) NOT NULL,
    workflow_key VARCHAR(128) NOT NULL,
    session_id VARCHAR(64) NULL,
    status VARCHAR(32) NULL DEFAULT 'running',
    input_payload TEXT NULL,
    output_payload TEXT NULL,
    error_message TEXT NULL,
    started_at DATETIME(3) NOT NULL,
    finished_at DATETIME(3) NULL,
    duration_ms BIGINT NULL DEFAULT 0,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_workflow_runs_workflow_key (workflow_key),
    KEY idx_workflow_runs_session_id (session_id),
    KEY idx_workflow_runs_status (status),
    KEY idx_workflow_runs_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE workflow_runs
    ADD COLUMN user_id VARCHAR(64) NULL AFTER workflow_key,
    ADD COLUMN parent_run_id VARCHAR(64) NULL AFTER session_id,
    ADD COLUMN active_session_key VARCHAR(64) NULL AFTER parent_run_id,
    ADD COLUMN available_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) AFTER status,
    ADD COLUMN priority INT NOT NULL DEFAULT 0 AFTER available_at,
    ADD COLUMN runtime_mode VARCHAR(32) NOT NULL DEFAULT 'legacy' AFTER priority,
    ADD COLUMN attempt INT UNSIGNED NOT NULL DEFAULT 0 AFTER runtime_mode,
    ADD COLUMN max_attempts INT UNSIGNED NOT NULL DEFAULT 3 AFTER attempt,
    ADD COLUMN lease_owner VARCHAR(128) NULL AFTER max_attempts,
    ADD COLUMN lease_until DATETIME(3) NULL AFTER lease_owner,
    ADD COLUMN lease_generation BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER lease_until,
    ADD COLUMN heartbeat_at DATETIME(3) NULL AFTER lease_generation,
    ADD COLUMN immutable_input_json JSON NULL AFTER heartbeat_at,
    ADD COLUMN query_text LONGTEXT NULL AFTER immutable_input_json,
    ADD COLUMN context_snapshot_json JSON NULL AFTER query_text,
    ADD COLUMN session_revision BIGINT UNSIGNED NULL AFTER context_snapshot_json,
    ADD COLUMN checkpoint_id VARCHAR(128) NULL AFTER session_revision,
    ADD COLUMN interrupt_address VARCHAR(512) NULL AFTER checkpoint_id,
    ADD COLUMN recovery_mode VARCHAR(32) NULL AFTER interrupt_address,
    ADD COLUMN runtime_version VARCHAR(128) NULL AFTER recovery_mode,
    ADD COLUMN runtime_compatibility_hash CHAR(64) NULL AFTER runtime_version,
    ADD COLUMN agent_revision VARCHAR(128) NULL AFTER runtime_compatibility_hash,
    ADD COLUMN model_snapshot JSON NULL AFTER agent_revision,
    ADD COLUMN tool_snapshot JSON NULL AFTER model_snapshot,
    ADD COLUMN mcp_catalog_hash CHAR(64) NULL AFTER tool_snapshot,
    ADD COLUMN skill_snapshot JSON NULL AFTER mcp_catalog_hash,
    ADD COLUMN prompt_hash CHAR(64) NULL AFTER skill_snapshot,
    ADD COLUMN policy_hash CHAR(64) NULL AFTER prompt_hash,
    ADD COLUMN config_hash CHAR(64) NULL AFTER policy_hash,
    ADD COLUMN feature_snapshot JSON NULL AFTER config_hash,
    ADD COLUMN budget_limits_json JSON NULL AFTER feature_snapshot,
    ADD COLUMN budget_usage_json JSON NULL AFTER budget_limits_json,
    ADD COLUMN budget_reservations_json JSON NULL AFTER budget_usage_json,
    ADD COLUMN usage_quality VARCHAR(32) NOT NULL DEFAULT 'unknown' AFTER budget_reservations_json,
    ADD COLUMN trace_quality VARCHAR(32) NOT NULL DEFAULT 'unknown' AFTER usage_quality,
    ADD COLUMN last_event_seq BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER trace_quality,
    ADD COLUMN cancel_requested_at DATETIME(3) NULL AFTER last_event_seq,
    ADD COLUMN park_reason VARCHAR(128) NULL AFTER cancel_requested_at;

CREATE UNIQUE INDEX uidx_workflow_runs_active_session ON workflow_runs(active_session_key);
CREATE INDEX idx_workflow_runs_claim ON workflow_runs(runtime_mode, status, available_at, priority, lease_until, id);
CREATE INDEX idx_workflow_runs_reap ON workflow_runs(status, lease_until, lease_generation);
CREATE INDEX idx_workflow_runs_status_updated ON workflow_runs(status, updated_at, id);
CREATE INDEX idx_workflow_runs_user_scope ON workflow_runs(user_id, status, created_at, id);

CREATE TABLE IF NOT EXISTS workflow_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    run_id VARCHAR(64) NOT NULL,
    seq BIGINT UNSIGNED NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    payload TEXT NULL,
    created_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_workflow_events_run_seq (run_id, seq),
    KEY idx_workflow_events_event_type (event_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE workflow_events
    MODIFY COLUMN seq BIGINT UNSIGNED NOT NULL,
    ADD COLUMN payload_version INT UNSIGNED NOT NULL DEFAULT 1 AFTER payload,
    ADD COLUMN trace_id VARCHAR(64) NULL AFTER payload_version;
CREATE INDEX idx_workflow_events_replay ON workflow_events(run_id, seq, event_type);

CREATE TABLE IF NOT EXISTS workflow_checkpoints (
    id VARCHAR(64) NOT NULL,
    run_id VARCHAR(64) NOT NULL,
    checkpoint_key VARCHAR(128) NOT NULL,
    snapshot_json JSON NOT NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_workflow_checkpoints_run_id (run_id),
    KEY idx_workflow_checkpoints_checkpoint_key (checkpoint_key),
    KEY idx_workflow_checkpoints_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS session_state_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    session_id VARCHAR(64) NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    state_json JSON NOT NULL,
    created_at DATETIME(3) NULL,
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_session_state_revisions_session_revision (session_id, revision),
    KEY idx_session_state_revisions_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE session_state_revisions MODIFY COLUMN revision BIGINT UNSIGNED NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS session_state_revisions;
DROP TABLE IF EXISTS workflow_checkpoints;
DROP TABLE IF EXISTS workflow_events;
DROP TABLE IF EXISTS workflow_runs;
DROP TABLE IF EXISTS ops_protected_assets;
DROP TABLE IF EXISTS ops_run_steps;
DROP TABLE IF EXISTS ops_runs;
DROP TABLE IF EXISTS ops_playbooks;
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS message_feedbacks;
DROP TABLE IF EXISTS knowledge_chunks;
DROP TABLE IF EXISTS knowledge_documents;
DROP TABLE IF EXISTS knowledge_bases;
DROP TABLE IF EXISTS agent_trace_nodes;
DROP TABLE IF EXISTS agent_trace_runs;
DROP TABLE IF EXISTS query_term_mappings;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS events;
