-- +goose NO TRANSACTION
-- +goose Up
-- 这两个索引由旧启动路径手工创建；空库需要补建，既有库必须原样保留。
SET @phase03_index_exists = (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events' AND INDEX_NAME = 'idx_events_created_at'
);
SET @phase03_index_sql = IF(
    @phase03_index_exists = 0,
    'CREATE INDEX idx_events_created_at ON events(created_at DESC)',
    'SELECT 1'
);
PREPARE phase03_index_statement FROM @phase03_index_sql;
EXECUTE phase03_index_statement;
DEALLOCATE PREPARE phase03_index_statement;

SET @phase03_index_exists = (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'agent_trace_runs' AND INDEX_NAME = 'idx_trace_runs_created'
);
SET @phase03_index_sql = IF(
    @phase03_index_exists = 0,
    'CREATE INDEX idx_trace_runs_created ON agent_trace_runs(created_at DESC)',
    'SELECT 1'
);
PREPARE phase03_index_statement FROM @phase03_index_sql;
EXECUTE phase03_index_statement;
DEALLOCATE PREPARE phase03_index_statement;
CREATE INDEX idx_trace_runs_retention ON agent_trace_runs(created_at, status);
CREATE INDEX idx_trace_nodes_retention ON agent_trace_nodes(created_at, trace_id);
CREATE INDEX idx_workflow_runs_retention ON workflow_runs(finished_at, deleted_at, status);
CREATE INDEX idx_workflow_events_retention ON workflow_events(created_at, run_id);
CREATE INDEX idx_workflow_checkpoints_retention ON workflow_checkpoints(expires_at, committed_at);
CREATE INDEX idx_agent_approvals_retention ON agent_approvals(decided_at, expires_at, status);
CREATE INDEX idx_agent_effects_retention ON agent_effects(finished_at, resolved_at, status);
CREATE INDEX idx_session_revisions_retention ON session_state_revisions(created_at, session_id);

-- +goose Down
DROP INDEX idx_session_revisions_retention ON session_state_revisions;
DROP INDEX idx_agent_effects_retention ON agent_effects;
DROP INDEX idx_agent_approvals_retention ON agent_approvals;
DROP INDEX idx_workflow_checkpoints_retention ON workflow_checkpoints;
DROP INDEX idx_workflow_events_retention ON workflow_events;
DROP INDEX idx_workflow_runs_retention ON workflow_runs;
DROP INDEX idx_trace_nodes_retention ON agent_trace_nodes;
DROP INDEX idx_trace_runs_retention ON agent_trace_runs;
DROP INDEX idx_trace_runs_created ON agent_trace_runs;
DROP INDEX idx_events_created_at ON events;
