-- +goose NO TRANSACTION
-- +goose Up

-- Docker/学习环境固定提供两个可登录用户，密码均为 123456 的 bcrypt 哈希。
DELETE FROM users;
INSERT INTO users (id, username, password, role, created_at, updated_at)
VALUES
    ('default-admin-20260828', 'admin', '$2b$12$0YqpfIaesYyR/uSjwSgqWOU2Eav0ZY.fBoaCknFyLr5uZJbV0dYoi', 'admin', NOW(3), NOW(3)),
    ('default-user1-20260828', 'user1', '$2b$12$0YqpfIaesYyR/uSjwSgqWOU2Eav0ZY.fBoaCknFyLr5uZJbV0dYoi', 'user', NOW(3), NOW(3));

-- +goose Down
DELETE FROM users WHERE username IN ('admin', 'user1');
