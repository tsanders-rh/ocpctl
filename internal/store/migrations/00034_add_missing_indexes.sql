-- Migration: Add missing database indexes
-- Adds indexes identified as missing for improved query performance

-- +goose Up
-- Index for jobs.started_at
-- Used by GetStuckJobs query which filters RUNNING jobs by started_at threshold
-- Query: SELECT * FROM jobs WHERE status = 'RUNNING' AND started_at < NOW() - interval ORDER BY started_at ASC
-- Performance gain: Significant improvement for stuck job detection queries
CREATE INDEX IF NOT EXISTS idx_jobs_started_at ON jobs(started_at) WHERE started_at IS NOT NULL;

-- Verify idx_clusters_owner_id exists (should have been created in migration 00002)
-- Adding here for completeness in case it was accidentally dropped
CREATE INDEX IF NOT EXISTS idx_clusters_owner_id ON clusters(owner_id);

-- Composite index for stuck jobs query optimization
-- Optimizes the common query pattern: WHERE status = 'RUNNING' AND started_at < threshold
CREATE INDEX IF NOT EXISTS idx_jobs_status_started_at ON jobs(status, started_at)
    WHERE status = 'RUNNING' AND started_at IS NOT NULL;

-- Add comments for documentation
COMMENT ON INDEX idx_jobs_started_at IS 'Improves performance of job queries filtered by start time';
COMMENT ON INDEX idx_jobs_status_started_at IS 'Composite index optimized for stuck job detection queries';

-- +goose Down
-- Drop the indexes created in the Up migration
DROP INDEX IF EXISTS idx_jobs_started_at;
DROP INDEX IF EXISTS idx_jobs_status_started_at;
-- Note: We don't drop idx_clusters_owner_id as it may have been created by migration 00002
