-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX idx_users_role_scope ON users(role, deleted_at, id);
CREATE INDEX idx_settings_updated_at ON settings(updated_at);

-- +goose Down
DROP INDEX idx_settings_updated_at ON settings;
DROP INDEX idx_users_role_scope ON users;
