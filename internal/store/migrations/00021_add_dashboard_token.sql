-- +goose Up
-- Add dashboard_token column to cluster_outputs table
ALTER TABLE cluster_outputs ADD COLUMN IF NOT EXISTS dashboard_token TEXT;

-- +goose Down
ALTER TABLE cluster_outputs DROP COLUMN IF EXISTS dashboard_token;
