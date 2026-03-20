-- Add cluster_type field to support OpenShift, EKS, and IKS clusters
-- Migration: 00018_add_cluster_type.sql

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
