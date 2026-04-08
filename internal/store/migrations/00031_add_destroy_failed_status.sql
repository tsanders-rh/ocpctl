-- +goose Up

-- Add DESTROY_FAILED to the allowed cluster statuses
-- This allows the system to properly track clusters where destruction failed
-- (e.g., AWS API errors, network issues) separately from creation failures
ALTER TABLE clusters DROP CONSTRAINT clusters_status_check;
ALTER TABLE clusters ADD CONSTRAINT clusters_status_check
  CHECK (status IN (
    'PENDING',
    'CREATING',
    'READY',
    'HIBERNATING',
    'HIBERNATED',
    'RESUMING',
    'DESTROYING',
    'DESTROYED',
    'FAILED',
    'DESTROY_FAILED'
  ));

-- Add comment explaining the new status
COMMENT ON COLUMN clusters.status IS 'Current cluster status. FAILED = creation failed, DESTROY_FAILED = destruction failed';

-- +goose Down

-- Revert to original constraint without DESTROY_FAILED
ALTER TABLE clusters DROP CONSTRAINT clusters_status_check;
ALTER TABLE clusters ADD CONSTRAINT clusters_status_check
  CHECK (status IN (
    'PENDING',
    'CREATING',
    'READY',
    'HIBERNATING',
    'HIBERNATED',
    'RESUMING',
    'DESTROYING',
    'DESTROYED',
    'FAILED'
  ));
