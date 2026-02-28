-- +goose Up
-- Create initial admin user
-- Password: changeme (bcrypt hash)
INSERT INTO users (email, username, password_hash, role, active, created_at, updated_at)
VALUES (
    'admin@localhost',
    'admin',
    '$2a$10$FA2EDw.BHNmEP508TMIka.iXbt4l4D1B3AMcYBAzi9B1bO.gV6Yv2',
    'admin',
    true,
    NOW(),
    NOW()
) ON CONFLICT (email) DO NOTHING;

-- +goose Down
DELETE FROM users WHERE email = 'admin@localhost';
