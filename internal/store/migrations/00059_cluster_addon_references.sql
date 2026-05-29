-- Migration 00059: Cluster Addon References
-- Add selected addon IDs to clusters table for tracking which addons are enabled per cluster

-- Add selected addon IDs column (PostgreSQL array)
ALTER TABLE clusters
  ADD COLUMN selected_addon_ids TEXT[];

-- Create GIN index for efficient array queries (e.g., "which clusters use this addon?")
CREATE INDEX idx_clusters_selected_addons
  ON clusters
  USING GIN(selected_addon_ids);

-- Backfill existing clusters with empty array (NULL -> empty array)
UPDATE clusters
SET selected_addon_ids = '{}'
WHERE selected_addon_ids IS NULL;

-- Add NOT NULL constraint after backfill
ALTER TABLE clusters
  ALTER COLUMN selected_addon_ids SET DEFAULT '{}';

ALTER TABLE clusters
  ALTER COLUMN selected_addon_ids SET NOT NULL;

-- Rollback SQL (for reference, not executed):
-- ALTER TABLE clusters ALTER COLUMN selected_addon_ids DROP NOT NULL;
-- ALTER TABLE clusters ALTER COLUMN selected_addon_ids DROP DEFAULT;
-- DROP INDEX IF EXISTS idx_clusters_selected_addons;
-- ALTER TABLE clusters DROP COLUMN IF EXISTS selected_addon_ids;
