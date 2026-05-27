-- +goose Up
-- Clear lease information for clusters that have been released back to pool
-- These clusters are in READY or CLEANING state but still have leased_by set
UPDATE clusters
SET leased_by = NULL,
    leased_at = NULL,
    lease_expires_at = NULL,
    lease_metadata = NULL
WHERE pool_state IN ('READY', 'CLEANING')
  AND leased_by IS NOT NULL
  AND pool_id IS NOT NULL;

-- +goose Down
-- No-op: We cannot restore the old lease information
