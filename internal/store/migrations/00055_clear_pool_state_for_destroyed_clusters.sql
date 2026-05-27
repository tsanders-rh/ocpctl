-- +goose Up
-- Migration: Clear pool_state for DESTROYED and FAILED clusters
-- Date: 2026-05-24
-- Description: This ensures pool statistics don't count destroyed/failed clusters

-- Clear pool_state for all DESTROYED and FAILED clusters
UPDATE clusters
SET pool_state = NULL
WHERE status IN ('DESTROYED', 'FAILED')
  AND pool_state IS NOT NULL;

-- +goose Down
-- Rollback: No action needed (pool_state was already inconsistent)
