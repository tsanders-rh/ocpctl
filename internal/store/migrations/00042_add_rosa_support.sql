-- Migration: Add ROSA support
-- Description: Support Red Hat OpenShift Service on AWS (ROSA) cluster type
-- Created: 2026-05-05

-- +goose Up
-- Add 'rosa' to the cluster_type enum
ALTER TYPE cluster_type ADD VALUE IF NOT EXISTS 'rosa';

-- Update comment for documentation - separate OpenShift IPI from ROSA
COMMENT ON COLUMN clusters.cluster_type IS 'Type of Kubernetes cluster: openshift (OpenShift IPI self-managed), rosa (Red Hat OpenShift Service on AWS managed), eks (AWS EKS), iks (IBM Cloud IKS), or gke (Google Kubernetes Engine)';

-- Add machine_pool_metadata column for ROSA-specific machine pool configuration
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS machine_pool_metadata JSONB;

-- Add comment for the new column
COMMENT ON COLUMN clusters.machine_pool_metadata IS 'ROSA-specific machine pool metadata including instance types, replica counts, autoscaling config, availability zones, etc.';

-- +goose Down
-- Note: PostgreSQL does not support removing enum values
-- Rollback would require recreating the enum type, which is complex
-- and could cause downtime. If rollback is needed, manual intervention required.

-- Drop machine_pool_metadata column
ALTER TABLE clusters DROP COLUMN IF EXISTS machine_pool_metadata;
