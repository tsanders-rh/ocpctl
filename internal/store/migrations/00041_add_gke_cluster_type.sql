-- Migration: Add 'gke' to cluster_type enum
-- Description: Support Google Kubernetes Engine (GKE) cluster type
-- Created: 2026-05-02

-- +goose Up
-- Add 'gke' to the cluster_type enum
ALTER TYPE cluster_type ADD VALUE IF NOT EXISTS 'gke';

-- Update comment for documentation
COMMENT ON COLUMN clusters.cluster_type IS 'Type of Kubernetes cluster: openshift (OCP/ROSA), eks (AWS EKS), iks (IBM Cloud IKS), or gke (Google Kubernetes Engine)';

-- +goose Down
-- Note: PostgreSQL does not support removing enum values
-- Rollback would require recreating the enum type, which is complex
-- and could cause downtime. If rollback is needed, manual intervention required.
