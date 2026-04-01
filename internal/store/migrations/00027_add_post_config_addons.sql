-- +goose Up
-- Create post_config_addons table for add-on library
CREATE TABLE post_config_addons (
  id VARCHAR(64) PRIMARY KEY DEFAULT gen_random_uuid()::text,
  addon_id VARCHAR(100) UNIQUE NOT NULL,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  category VARCHAR(50) NOT NULL,
  config JSONB NOT NULL,
  supported_platforms TEXT[] NOT NULL DEFAULT '{}',
  enabled BOOLEAN DEFAULT TRUE,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

  -- Constraints
  CHECK (category IN ('backup', 'migration', 'cicd', 'monitoring', 'security', 'storage', 'networking'))
);

-- Indexes for efficient querying
CREATE INDEX idx_addons_category ON post_config_addons(category);
CREATE INDEX idx_addons_enabled ON post_config_addons(enabled) WHERE enabled = TRUE;
CREATE INDEX idx_addons_addon_id ON post_config_addons(addon_id);

-- Comments
COMMENT ON TABLE post_config_addons IS 'Pre-defined add-on configurations for post-deployment';
COMMENT ON COLUMN post_config_addons.addon_id IS 'Unique identifier for the add-on (e.g., "oadp", "mtc")';
COMMENT ON COLUMN post_config_addons.config IS 'CustomPostConfig JSONB containing operators, scripts, manifests, helmCharts';
COMMENT ON COLUMN post_config_addons.supported_platforms IS 'Array of supported platforms (openshift, eks, iks)';

-- +goose Down
DROP TABLE IF EXISTS post_config_addons;
