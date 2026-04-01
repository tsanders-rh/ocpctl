-- +goose Up
-- Add custom_post_config column to clusters table
ALTER TABLE clusters
ADD COLUMN custom_post_config JSONB;

-- Add tracking columns to cluster_configurations table for user-defined configs
ALTER TABLE cluster_configurations
ADD COLUMN user_defined BOOLEAN DEFAULT FALSE,
ADD COLUMN created_by_user_id UUID REFERENCES users(id),
ADD COLUMN source VARCHAR(20) CHECK (source IN ('profile', 'addon', 'custom'));

-- Create index on user_defined configs for faster querying
CREATE INDEX idx_cluster_configs_user_defined
ON cluster_configurations(user_defined)
WHERE user_defined = TRUE;

-- Create index on source for analytics
CREATE INDEX idx_cluster_configs_source
ON cluster_configurations(source);

-- Add comment to explain the new columns
COMMENT ON COLUMN clusters.custom_post_config IS 'User-defined post-deployment configuration specified at cluster creation time (JSONB)';
COMMENT ON COLUMN cluster_configurations.user_defined IS 'Flag indicating if this config was user-defined (true) or profile-defined (false)';
COMMENT ON COLUMN cluster_configurations.created_by_user_id IS 'User ID who created this custom configuration';
COMMENT ON COLUMN cluster_configurations.source IS 'Source of configuration: profile, addon, or custom';

-- +goose Down
-- Remove the new columns
DROP INDEX IF EXISTS idx_cluster_configs_source;
DROP INDEX IF EXISTS idx_cluster_configs_user_defined;

ALTER TABLE cluster_configurations
DROP COLUMN IF EXISTS source,
DROP COLUMN IF EXISTS created_by_user_id,
DROP COLUMN IF EXISTS user_defined;

ALTER TABLE clusters
DROP COLUMN IF EXISTS custom_post_config;
