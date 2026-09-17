-- 00071_add_baremetal_platform_support.sql
-- Add bare-metal (agent-based) platform support to the clusters table constraint.

-- +goose Up
-- Drop the constraint that only allowed 'aws', 'ibmcloud', 'gcp', 'azure'
ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_platform_check;

-- Add new constraint that includes 'baremetal'
ALTER TABLE clusters ADD CONSTRAINT clusters_platform_check
  CHECK (platform IN ('aws', 'ibmcloud', 'gcp', 'azure', 'baremetal'));

-- +goose Down
-- Revert to the previous constraint (rollback only)
ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_platform_check;
ALTER TABLE clusters ADD CONSTRAINT clusters_platform_check
  CHECK (platform IN ('aws', 'ibmcloud', 'gcp', 'azure'));
