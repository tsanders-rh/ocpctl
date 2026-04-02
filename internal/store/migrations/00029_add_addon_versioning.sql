-- +goose Up

-- Add version column and update unique constraint
ALTER TABLE post_config_addons ADD COLUMN version VARCHAR(50);
ALTER TABLE post_config_addons ADD COLUMN display_name VARCHAR(255);
ALTER TABLE post_config_addons ADD COLUMN is_default BOOLEAN DEFAULT FALSE;

-- Drop old unique constraint on addon_id
ALTER TABLE post_config_addons DROP CONSTRAINT IF EXISTS post_config_addons_addon_id_key;

-- Add new unique constraint on addon_id + version
ALTER TABLE post_config_addons ADD CONSTRAINT addon_version_unique UNIQUE(addon_id, version);

-- Update existing records with version
UPDATE post_config_addons SET version = 'stable-1.4', display_name = 'OADP 1.4 (Stable)', is_default = TRUE WHERE addon_id = 'oadp';
UPDATE post_config_addons SET version = 'release-v1.8', display_name = 'MTC 1.8 (Stable)', is_default = TRUE WHERE addon_id = 'mtc';
UPDATE post_config_addons SET version = 'stable-v7.0', display_name = 'MTA 7.0 (Stable)', is_default = TRUE WHERE addon_id = 'mta';

-- Create index for common queries
CREATE INDEX idx_addons_id_enabled ON post_config_addons(addon_id, enabled);
CREATE INDEX idx_addons_default ON post_config_addons(addon_id, is_default) WHERE is_default = TRUE;

COMMENT ON COLUMN post_config_addons.version IS 'Operator channel or version identifier (e.g., stable-1.4)';
COMMENT ON COLUMN post_config_addons.is_default IS 'Marks the default/recommended version for this add-on';

-- +goose Down
ALTER TABLE post_config_addons DROP CONSTRAINT IF EXISTS addon_version_unique;
ALTER TABLE post_config_addons DROP COLUMN IF EXISTS version;
ALTER TABLE post_config_addons DROP COLUMN IF EXISTS display_name;
ALTER TABLE post_config_addons DROP COLUMN IF EXISTS is_default;
ALTER TABLE post_config_addons ADD CONSTRAINT post_config_addons_addon_id_key UNIQUE(addon_id);
DROP INDEX IF EXISTS idx_addons_id_enabled;
DROP INDEX IF EXISTS idx_addons_default;
