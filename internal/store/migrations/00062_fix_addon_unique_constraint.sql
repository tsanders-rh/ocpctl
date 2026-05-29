-- +goose Up
-- Fix addon_id unique constraint to allow multiple versions per addon
-- Old: addon_id is UNIQUE (only one version per addon)
-- New: (addon_id, version) is UNIQUE (multiple versions per addon)

-- Drop the old UNIQUE constraint on addon_id
ALTER TABLE post_config_addons
  DROP CONSTRAINT IF EXISTS post_config_addons_addon_id_key;

-- Add composite UNIQUE constraint on (addon_id, version)
ALTER TABLE post_config_addons
  ADD CONSTRAINT post_config_addons_addon_id_version_key
  UNIQUE (addon_id, version);

COMMENT ON CONSTRAINT post_config_addons_addon_id_version_key
  ON post_config_addons
  IS 'Ensures unique combination of addon_id and version (allows multiple versions per addon)';

-- +goose Down
-- Restore old UNIQUE constraint on addon_id (will fail if multiple versions exist)
ALTER TABLE post_config_addons
  DROP CONSTRAINT IF EXISTS post_config_addons_addon_id_version_key;

ALTER TABLE post_config_addons
  ADD CONSTRAINT post_config_addons_addon_id_key
  UNIQUE (addon_id);
