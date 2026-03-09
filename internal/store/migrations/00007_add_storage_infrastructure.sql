-- +goose Up
-- Migration: Add storage infrastructure for EFS and shared storage management
-- Created: 2026-03-08

-- +goose StatementBegin

-- Update jobs table CHECK constraint to include new storage job types
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check
    CHECK (job_type IN ('CREATE', 'DESTROY', 'SCALE_WORKERS', 'JANITOR_DESTROY', 'ORPHAN_SWEEP', 'CONFIGURE_EFS', 'PROVISION_SHARED_STORAGE', 'UNLINK_SHARED_STORAGE'));

-- Add storage_config JSONB column to clusters table
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS storage_config JSONB DEFAULT '{}'::jsonb;

-- Create GIN index for efficient JSONB queries
CREATE INDEX IF NOT EXISTS idx_clusters_storage_config ON clusters USING GIN (storage_config);

-- Add comment
COMMENT ON COLUMN clusters.storage_config IS 'Storage configuration including local EFS, shared EFS, S3 bucket, and storage group references';

-- Storage groups table (represents shared EFS + S3 bucket for migration)
CREATE TABLE IF NOT EXISTS storage_groups (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    efs_id VARCHAR(255),
    efs_security_group_id VARCHAR(255),
    s3_bucket VARCHAR(255),
    region VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL CHECK (status IN ('PROVISIONING', 'READY', 'FAILED', 'DELETING')),
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Index on status for querying active/failed groups
CREATE INDEX IF NOT EXISTS idx_storage_groups_status ON storage_groups(status);

-- Index on region for region-based queries
CREATE INDEX IF NOT EXISTS idx_storage_groups_region ON storage_groups(region);

-- Cluster to storage group links (many-to-many relationship)
CREATE TABLE IF NOT EXISTS cluster_storage_links (
    id VARCHAR(64) PRIMARY KEY,
    cluster_id VARCHAR(64) NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    storage_group_id VARCHAR(64) NOT NULL REFERENCES storage_groups(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL CHECK (role IN ('source', 'target', 'shared')),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(cluster_id, storage_group_id)
);

-- Indexes for efficient join queries
CREATE INDEX IF NOT EXISTS idx_cluster_storage_links_cluster ON cluster_storage_links(cluster_id);
CREATE INDEX IF NOT EXISTS idx_cluster_storage_links_group ON cluster_storage_links(storage_group_id);

-- Comments for documentation
COMMENT ON TABLE storage_groups IS 'Shared storage resources (EFS + S3) accessible from multiple clusters for migration testing';
COMMENT ON TABLE cluster_storage_links IS 'Links between clusters and storage groups (many-to-many relationship)';
COMMENT ON COLUMN cluster_storage_links.role IS 'Role of the cluster in migration: source, target, or shared';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Drop tables in reverse order (respecting foreign key dependencies)
DROP TABLE IF EXISTS cluster_storage_links;
DROP TABLE IF EXISTS storage_groups;

-- Drop column and index from clusters table
DROP INDEX IF EXISTS idx_clusters_storage_config;
ALTER TABLE clusters DROP COLUMN IF EXISTS storage_config;

-- Restore original jobs table CHECK constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check
    CHECK (job_type IN ('CREATE', 'DESTROY', 'SCALE_WORKERS', 'JANITOR_DESTROY', 'ORPHAN_SWEEP'));

-- +goose StatementEnd
