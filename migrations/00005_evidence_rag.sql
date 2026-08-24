-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE knowledge_bases
    ADD COLUMN content_hash CHAR(64) NULL AFTER description,
    ADD COLUMN source_version VARCHAR(128) NULL AFTER content_hash,
    ADD COLUMN access_scope VARCHAR(191) NOT NULL DEFAULT 'public' AFTER source_version,
    ADD COLUMN indexed_version BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER access_scope,
    ADD COLUMN updated_by VARCHAR(128) NULL AFTER indexed_version;

ALTER TABLE knowledge_documents
    ADD COLUMN content_hash CHAR(64) NULL AFTER file_hash,
    ADD COLUMN source_version VARCHAR(128) NULL AFTER content_hash,
    ADD COLUMN access_scope VARCHAR(191) NOT NULL DEFAULT 'public' AFTER source_version,
    ADD COLUMN indexed_version BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER access_scope,
    ADD COLUMN updated_by VARCHAR(128) NULL AFTER indexed_version;

ALTER TABLE knowledge_chunks
    ADD COLUMN content_hash CHAR(64) NULL AFTER content_preview,
    ADD COLUMN source_version VARCHAR(128) NULL AFTER content_hash,
    ADD COLUMN access_scope VARCHAR(191) NOT NULL DEFAULT 'public' AFTER source_version,
    ADD COLUMN indexed_version BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER access_scope,
    ADD COLUMN updated_by VARCHAR(128) NULL AFTER indexed_version;

CREATE INDEX idx_knowledge_bases_scope_version ON knowledge_bases(access_scope, indexed_version, updated_at);
CREATE INDEX idx_knowledge_documents_scope_version ON knowledge_documents(access_scope, indexed_version, updated_at);
CREATE INDEX idx_knowledge_chunks_scope_version ON knowledge_chunks(access_scope, indexed_version, doc_id);

-- +goose Down
DROP INDEX idx_knowledge_chunks_scope_version ON knowledge_chunks;
DROP INDEX idx_knowledge_documents_scope_version ON knowledge_documents;
DROP INDEX idx_knowledge_bases_scope_version ON knowledge_bases;
ALTER TABLE knowledge_chunks
    DROP COLUMN updated_by,
    DROP COLUMN indexed_version,
    DROP COLUMN access_scope,
    DROP COLUMN source_version,
    DROP COLUMN content_hash;
ALTER TABLE knowledge_documents
    DROP COLUMN updated_by,
    DROP COLUMN indexed_version,
    DROP COLUMN access_scope,
    DROP COLUMN source_version,
    DROP COLUMN content_hash;
ALTER TABLE knowledge_bases
    DROP COLUMN updated_by,
    DROP COLUMN indexed_version,
    DROP COLUMN access_scope,
    DROP COLUMN source_version,
    DROP COLUMN content_hash;
