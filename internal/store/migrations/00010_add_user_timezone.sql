-- +goose Up
-- Migration: Add timezone field to users table

ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone VARCHAR(100) NOT NULL DEFAULT 'UTC';

CREATE INDEX idx_users_timezone ON users(timezone);

-- +goose Down
DROP INDEX IF EXISTS idx_users_timezone;
ALTER TABLE users DROP COLUMN IF EXISTS timezone;
