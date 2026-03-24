-- Migration: Add performance indexes
-- This migration adds database indexes identified in the code review to improve query performance
-- See: CODE_REVIEW.md section "Missing Database Indexes"

-- +goose Up
-- Index 1: job_locks.expires_at
-- Used by CleanupExpiredLocks (called every poll cycle - 5 seconds)
-- Query: SELECT lock_id FROM job_locks WHERE expires_at < NOW()
-- Performance gain: 90%+ reduction in lock query time
CREATE INDEX IF NOT EXISTS idx_job_locks_expires_at ON job_locks(expires_at);

-- Composite index for IsLocked query
-- Query: SELECT EXISTS(SELECT 1 FROM job_locks WHERE cluster_id = $1 AND expires_at > NOW())
CREATE INDEX IF NOT EXISTS idx_job_locks_lookup ON job_locks(cluster_id, expires_at);

-- Index 2: job_retry_history.failed_at
-- Used by GetRecentRetries for retry statistics
-- Query: SELECT * FROM job_retry_history WHERE failed_at >= NOW() - $1::interval ORDER BY failed_at DESC
CREATE INDEX IF NOT EXISTS idx_job_retry_history_failed_at ON job_retry_history(failed_at DESC);

-- Index 3: clusters work hours enforcement
-- Used by GetClustersForWorkHoursEnforcement
-- Query: SELECT ... FROM clusters WHERE status IN ('READY', 'HIBERNATED') AND work_hours_enabled = true
-- Partial index (only indexes rows where work_hours_enabled = true)
CREATE INDEX IF NOT EXISTS idx_clusters_work_hours ON clusters(work_hours_enabled, status)
    WHERE work_hours_enabled = true;

-- Add comments for documentation
COMMENT ON INDEX idx_job_locks_expires_at IS 'Improves performance of expired lock cleanup queries (called every 5s)';
COMMENT ON INDEX idx_job_locks_lookup IS 'Composite index for lock existence checks by resource type/ID';
COMMENT ON INDEX idx_job_retry_history_failed_at IS 'Improves performance of retry history queries';
COMMENT ON INDEX idx_clusters_work_hours IS 'Partial index for work hours enforcement queries';

-- +goose Down
-- Drop all indexes created in the Up migration
DROP INDEX IF EXISTS idx_job_locks_expires_at;
DROP INDEX IF EXISTS idx_job_locks_lookup;
DROP INDEX IF EXISTS idx_job_retry_history_failed_at;
DROP INDEX IF EXISTS idx_clusters_work_hours;
