-- Add GCP platform support
-- Migration: 00035
-- Description: Update platform constraint to allow 'gcp' as a valid platform

-- Drop the old constraint that only allowed 'aws' and 'ibmcloud'
ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_platform_check;

-- Add new constraint that includes 'gcp'
ALTER TABLE clusters ADD CONSTRAINT clusters_platform_check
  CHECK (platform IN ('aws', 'ibmcloud', 'gcp'));

-- Note: No cluster_type constraint exists, so 'gke' cluster type is already supported
