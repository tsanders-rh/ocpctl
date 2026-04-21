-- Add preserve_on_failure column to clusters table
-- This allows users to keep cluster resources and work directories when cluster creation fails
-- Useful for debugging install failures

-- +goose Up
ALTER TABLE clusters
ADD COLUMN preserve_on_failure BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN clusters.preserve_on_failure IS 'If true, do not clean up cluster resources when creation fails (for debugging)';

-- +goose Down
ALTER TABLE clusters
DROP COLUMN preserve_on_failure;
