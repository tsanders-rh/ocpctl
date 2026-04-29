-- +goose Up
-- Add 'virtualization' to the allowed addon categories
ALTER TABLE post_config_addons
  DROP CONSTRAINT IF EXISTS post_config_addons_category_check;

ALTER TABLE post_config_addons
  ADD CONSTRAINT post_config_addons_category_check
  CHECK (category IN ('backup', 'migration', 'cicd', 'monitoring', 'security', 'storage', 'networking', 'virtualization'));

-- +goose Down
-- Remove 'virtualization' from allowed addon categories
ALTER TABLE post_config_addons
  DROP CONSTRAINT IF EXISTS post_config_addons_category_check;

ALTER TABLE post_config_addons
  ADD CONSTRAINT post_config_addons_category_check
  CHECK (category IN ('backup', 'migration', 'cicd', 'monitoring', 'security', 'storage', 'networking'));
