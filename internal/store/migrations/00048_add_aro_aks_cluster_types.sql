-- 00048_add_aro_aks_cluster_types.sql
-- Add ARO and AKS cluster types for Azure platform support

-- +goose Up
-- Add ARO (Azure Red Hat OpenShift) cluster type
ALTER TYPE cluster_type ADD VALUE IF NOT EXISTS 'aro';

-- Add AKS (Azure Kubernetes Service) cluster type
ALTER TYPE cluster_type ADD VALUE IF NOT EXISTS 'aks';

-- +goose Down
-- Note: PostgreSQL does not support removing enum values directly.
-- To rollback, you would need to:
-- 1. Create a new enum type without 'aro' and 'aks'
-- 2. Alter the column to use the new type
-- 3. Drop the old type
-- This is complex and risky, so we leave the enum values in place for rollback safety.
