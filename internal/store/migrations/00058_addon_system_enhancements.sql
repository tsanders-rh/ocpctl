-- +goose Up
-- Migration 00058: Addon System Enhancements
-- Add support for user-created addons, versioning, and immutability

-- Add addon source tracking and versioning columns
ALTER TABLE post_config_addons
  ADD COLUMN addon_source VARCHAR(20) DEFAULT 'system' CHECK (addon_source IN ('system', 'user')),
  ADD COLUMN created_by_user_id VARCHAR(255),
  ADD COLUMN is_published BOOLEAN DEFAULT FALSE,
  ADD COLUMN published_at TIMESTAMP WITH TIME ZONE,
  ADD COLUMN parent_version_id VARCHAR(64),  -- For version lineage (draft cloned from this)
  ADD COLUMN version_number INTEGER DEFAULT 1,  -- Incremental version counter
  ADD COLUMN is_immutable BOOLEAN DEFAULT FALSE;  -- Published versions can't be edited

-- Create index for user addon queries (partial index for efficiency)
CREATE INDEX idx_post_config_addons_user
  ON post_config_addons(created_by_user_id)
  WHERE addon_source = 'user';

-- Create index for version lineage queries
CREATE INDEX idx_post_config_addons_parent
  ON post_config_addons(parent_version_id)
  WHERE parent_version_id IS NOT NULL;

-- Add check constraint: system addons must be immutable
ALTER TABLE post_config_addons
  ADD CONSTRAINT system_addons_immutable CHECK (
    (addon_source = 'system' AND is_immutable = true) OR addon_source = 'user'
  );

-- Add check constraint: published user addons must be immutable
ALTER TABLE post_config_addons
  ADD CONSTRAINT published_addons_immutable CHECK (
    (is_published = false) OR (is_published = true AND is_immutable = true)
  );

-- Backfill existing addons: mark all as system and immutable
UPDATE post_config_addons
SET addon_source = 'system',
    is_immutable = true
WHERE addon_source IS NULL;

-- +goose Down
ALTER TABLE post_config_addons DROP CONSTRAINT IF EXISTS published_addons_immutable;
ALTER TABLE post_config_addons DROP CONSTRAINT IF EXISTS system_addons_immutable;
DROP INDEX IF EXISTS idx_post_config_addons_parent;
DROP INDEX IF EXISTS idx_post_config_addons_user;
ALTER TABLE post_config_addons
  DROP COLUMN IF EXISTS is_immutable,
  DROP COLUMN IF EXISTS version_number,
  DROP COLUMN IF EXISTS parent_version_id,
  DROP COLUMN IF EXISTS published_at,
  DROP COLUMN IF EXISTS is_published,
  DROP COLUMN IF EXISTS created_by_user_id,
  DROP COLUMN IF EXISTS addon_source;
