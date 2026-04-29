-- +goose Up
ALTER TABLE post_config_addons
ADD COLUMN metadata JSONB DEFAULT NULL;

-- Create index on metadata for efficient filtering
CREATE INDEX idx_post_config_addons_metadata ON post_config_addons USING gin(metadata);

-- +goose Down
DROP INDEX IF EXISTS idx_post_config_addons_metadata;
ALTER TABLE post_config_addons DROP COLUMN metadata;
