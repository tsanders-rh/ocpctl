-- +goose Up
-- Migration: Fix post_deploy_status for existing clusters to allow hibernation

-- Update NULL post_deploy_status to 'skipped' for existing READY/HIBERNATED clusters
-- These clusters either had no post-deployment or it already completed
UPDATE clusters
SET post_deploy_status = 'skipped'
WHERE post_deploy_status IS NULL
  AND status IN ('READY', 'HIBERNATED');

-- Update NULL post_deploy_status to 'skipped' for FAILED/DESTROYED clusters
-- These never completed successfully so post-deployment is not applicable
UPDATE clusters
SET post_deploy_status = 'skipped'
WHERE post_deploy_status IS NULL
  AND status IN ('FAILED', 'DESTROYED');

-- Leave NULL for PENDING/CREATING/DESTROYING clusters as they may not have
-- had their post_deploy_status set yet during cluster creation

-- +goose Down
-- No rollback - this is a data fix migration
