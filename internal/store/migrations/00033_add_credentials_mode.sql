-- +goose Up
-- Add credentials_mode column to clusters table
ALTER TABLE clusters ADD COLUMN credentials_mode TEXT;

COMMENT ON COLUMN clusters.credentials_mode IS 'Cloud credentials mode: Manual, Passthrough, or Mint (NULL = installer default)';

-- +goose Down
ALTER TABLE clusters DROP COLUMN credentials_mode;
