-- +goose Up
-- Migration: Make base_domain nullable for EKS/IKS clusters
-- Created: 2026-03-20

-- +goose StatementBegin

-- Make base_domain nullable since it's not required for EKS/IKS clusters
ALTER TABLE clusters ALTER COLUMN base_domain DROP NOT NULL;

-- Add a check constraint to ensure OpenShift clusters still have base_domain
-- Note: We can't easily enforce this at DB level since we'd need to check cluster_type
-- This will be validated at the application layer instead

-- Update the unique index to handle NULL base_domain values
-- Drop the old index that includes base_domain
DROP INDEX IF EXISTS idx_unique_active_cluster;

-- Create a new unique index that excludes base_domain
-- For OpenShift: name + platform + base_domain must be unique
-- For EKS/IKS: name + platform must be unique
CREATE UNIQUE INDEX idx_unique_active_cluster_with_domain
ON clusters(name, platform, base_domain)
WHERE status NOT IN ('DESTROYED', 'FAILED') AND base_domain IS NOT NULL;

CREATE UNIQUE INDEX idx_unique_active_cluster_without_domain
ON clusters(name, platform)
WHERE status NOT IN ('DESTROYED', 'FAILED') AND base_domain IS NULL;

COMMENT ON COLUMN clusters.base_domain IS 'DNS base domain for the cluster (required for OpenShift, not used for EKS/IKS)';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Drop the new indexes
DROP INDEX IF EXISTS idx_unique_active_cluster_without_domain;
DROP INDEX IF EXISTS idx_unique_active_cluster_with_domain;

-- Restore the original unique index
CREATE UNIQUE INDEX idx_unique_active_cluster
ON clusters(name, platform, base_domain)
WHERE status NOT IN ('DESTROYED', 'FAILED');

-- Make base_domain NOT NULL again (this will fail if there are NULL values)
ALTER TABLE clusters ALTER COLUMN base_domain SET NOT NULL;

-- +goose StatementEnd
