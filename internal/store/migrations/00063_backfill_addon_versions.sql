-- +goose Up
-- Backfill version data for old system addons that were created before version support
-- This sets default version='stable' and version_number=1 for addons with blank version

UPDATE post_config_addons
SET
  version = 'stable',
  display_name = name || ' (Stable)',
  is_default = TRUE,
  version_number = 1
WHERE
  addon_source = 'system'
  AND (version IS NULL OR version = '');

COMMENT ON TABLE post_config_addons IS 'System addons backfilled with version=stable for migration';

-- +goose Down
-- No down migration - data changes are intentional and should not be reverted
