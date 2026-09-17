-- +goose Up
-- Add 'ai' to the allowed addon categories (for RHOAI and future AI/ML addons)
ALTER TABLE post_config_addons
  DROP CONSTRAINT IF EXISTS post_config_addons_category_check;

ALTER TABLE post_config_addons
  ADD CONSTRAINT post_config_addons_category_check
  CHECK (category IN ('backup', 'migration', 'cicd', 'monitoring', 'security', 'storage', 'networking', 'virtualization', 'ai'));

-- +goose Down
-- Remove 'ai' from allowed addon categories
ALTER TABLE post_config_addons
  DROP CONSTRAINT IF EXISTS post_config_addons_category_check;

ALTER TABLE post_config_addons
  ADD CONSTRAINT post_config_addons_category_check
  CHECK (category IN ('backup', 'migration', 'cicd', 'monitoring', 'security', 'storage', 'networking', 'virtualization'));
