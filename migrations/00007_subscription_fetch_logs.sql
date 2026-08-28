-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE IF NOT EXISTS subscription_fetch_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    subscription_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    fetched_count INT NOT NULL DEFAULT 0,
    new_count INT NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    error_msg TEXT,
    created_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_subscription_fetch_logs_subscription (subscription_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE IF EXISTS subscription_fetch_logs;
