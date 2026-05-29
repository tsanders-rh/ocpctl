-- +goose Up
-- Add version, display_name, and is_default columns that were missing from original schema
ALTER TABLE post_config_addons
  ADD COLUMN IF NOT EXISTS version VARCHAR(100) DEFAULT '',
  ADD COLUMN IF NOT EXISTS display_name VARCHAR(255) DEFAULT '',
  ADD COLUMN IF NOT EXISTS is_default BOOLEAN DEFAULT FALSE;

-- Create indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_post_config_addons_version
  ON post_config_addons(addon_id, version);

CREATE INDEX IF NOT EXISTS idx_post_config_addons_default
  ON post_config_addons(is_default)
  WHERE is_default = TRUE;

COMMENT ON COLUMN post_config_addons.version IS 'Version/channel identifier (e.g., stable, nightly, 4.22)';
COMMENT ON COLUMN post_config_addons.display_name IS 'Human-readable version name (e.g., OADP 1.5 Stable)';
COMMENT ON COLUMN post_config_addons.is_default IS 'Whether this is the default version for this addon';

-- +goose Down
DROP INDEX IF EXISTS idx_post_config_addons_default;
DROP INDEX IF EXISTS idx_post_config_addons_version;

ALTER TABLE post_config_addons
  DROP COLUMN IF EXISTS is_default,
  DROP COLUMN IF EXISTS display_name,
  DROP COLUMN IF EXISTS version;
