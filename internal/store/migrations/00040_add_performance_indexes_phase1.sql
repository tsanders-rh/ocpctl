-- Migration: Add performance indexes for Phase 1 scalability improvements
-- Creates composite indexes identified in CODE_REVIEW_2026-05-02.md as critical for query performance
-- These indexes prevent full table scans on large datasets (100k+ clusters)

-- +goose Up

-- Composite index for cluster queries filtered by status and ordered by creation time
-- Optimizes queries: SELECT * FROM clusters WHERE status = ? ORDER BY created_at DESC
-- Use case: Listing clusters by status (Ready, Provisioning, etc.), janitor cleanup operations
-- Performance gain: Eliminates full table scan, enables index-only scans for count queries
CREATE INDEX IF NOT EXISTS idx_clusters_status_created ON clusters(status, created_at DESC);

-- Composite index for owner-specific cluster queries with status filtering
-- Optimizes queries: SELECT * FROM clusters WHERE owner_id = ? AND status = ?
-- Use case: User dashboard showing their clusters, access control checks
-- Performance gain: Significant improvement for multi-tenant queries
CREATE INDEX IF NOT EXISTS idx_clusters_owner_id_status ON clusters(owner_id, status);

-- Composite index for job queries filtered by status and ordered by creation time
-- Optimizes queries: SELECT * FROM jobs WHERE status = ? ORDER BY created_at ASC
-- Use case: Job queue processing, worker job selection, job history queries
-- Performance gain: Enables efficient FIFO job processing from index
CREATE INDEX IF NOT EXISTS idx_jobs_status_created ON jobs(status, created_at ASC);

-- Composite index for orphaned resources queries filtered by status and detection time
-- Optimizes queries: SELECT * FROM orphaned_resources WHERE status = ? ORDER BY last_detected_at DESC
-- Use case: Admin dashboard orphan resource listing, automated cleanup operations
-- Performance gain: Fast retrieval of active orphans for cleanup
CREATE INDEX IF NOT EXISTS idx_orphaned_resources_status_detected
    ON orphaned_resources(status, last_detected_at DESC);

-- Add documentation comments
COMMENT ON INDEX idx_clusters_status_created IS 'Phase 1: Optimizes cluster status queries and janitor operations';
COMMENT ON INDEX idx_clusters_owner_id_status IS 'Phase 1: Optimizes multi-tenant cluster access queries';
COMMENT ON INDEX idx_jobs_status_created IS 'Phase 1: Optimizes job queue processing and worker selection';
COMMENT ON INDEX idx_orphaned_resources_status_detected IS 'Phase 1: Optimizes orphaned resource cleanup queries';

-- +goose Down

-- Drop the indexes created in the Up migration
DROP INDEX IF EXISTS idx_clusters_status_created;
DROP INDEX IF EXISTS idx_clusters_owner_id_status;
DROP INDEX IF EXISTS idx_jobs_status_created;
DROP INDEX IF EXISTS idx_orphaned_resources_status_detected;
