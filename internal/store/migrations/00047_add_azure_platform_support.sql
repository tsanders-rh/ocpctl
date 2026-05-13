-- 00047_add_azure_platform_support.sql
-- Add Azure platform support to clusters table constraint

-- +goose Up
-- Drop the old constraint that only allowed 'aws', 'ibmcloud', 'gcp'
ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_platform_check;

-- Add new constraint that includes 'azure'
ALTER TABLE clusters ADD CONSTRAINT clusters_platform_check
  CHECK (platform IN ('aws', 'ibmcloud', 'gcp', 'azure'));

-- Note: cluster_type enum values ('aro' and 'aks') will be added in migration 00048

-- +goose Down
-- Revert to previous constraint (only for rollback scenarios)
ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_platform_check;
ALTER TABLE clusters ADD CONSTRAINT clusters_platform_check
  CHECK (platform IN ('aws', 'ibmcloud', 'gcp'));
