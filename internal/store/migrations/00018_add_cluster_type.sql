-- +goose Up
-- Migration: Add cluster_type field to support OpenShift, EKS, and IKS clusters
-- Created: 2026-03-20

-- +goose StatementBegin

-- Add cluster_type enum type
CREATE TYPE cluster_type AS ENUM ('openshift', 'eks', 'iks');

-- Add cluster_type column to clusters table
ALTER TABLE clusters
ADD COLUMN cluster_type cluster_type NOT NULL DEFAULT 'openshift';

-- Create index for filtering by cluster type
CREATE INDEX idx_clusters_cluster_type ON clusters(cluster_type);

-- Update existing clusters to explicitly be openshift type
UPDATE clusters SET cluster_type = 'openshift' WHERE cluster_type IS NULL;

-- Add comment for documentation
COMMENT ON COLUMN clusters.cluster_type IS 'Type of Kubernetes cluster: openshift (OCP/ROSA), eks (AWS EKS), or iks (IBM Cloud IKS)';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Drop the index
DROP INDEX IF EXISTS idx_clusters_cluster_type;

-- Drop the column
ALTER TABLE clusters DROP COLUMN IF EXISTS cluster_type;

-- Drop the enum type
DROP TYPE IF EXISTS cluster_type;

-- +goose StatementEnd
