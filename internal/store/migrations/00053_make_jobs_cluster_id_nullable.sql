-- +goose Up
-- Migration: Drop foreign key constraint on jobs.cluster_id
-- Date: 2026-05-22
-- Description: Remove foreign key constraint to allow pool-level jobs that don't reference a cluster

-- Drop the foreign key constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_cluster_id_fkey;

-- +goose Down
-- Rollback: Recreate the foreign key constraint
ALTER TABLE jobs ADD CONSTRAINT jobs_cluster_id_fkey
    FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE CASCADE;
