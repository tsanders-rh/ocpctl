-- +goose Up
-- Create post-config templates table for reusable configurations
CREATE TABLE postconfig_templates (
  id VARCHAR(64) PRIMARY KEY DEFAULT gen_random_uuid()::text,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  config JSONB NOT NULL,
  owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  is_public BOOLEAN DEFAULT FALSE,
  tags TEXT[] DEFAULT '{}',
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for efficient querying
CREATE INDEX idx_postconfig_templates_owner ON postconfig_templates(owner_id);
CREATE INDEX idx_postconfig_templates_public ON postconfig_templates(is_public) WHERE is_public = TRUE;
CREATE INDEX idx_postconfig_templates_tags ON postconfig_templates USING GIN(tags);

-- Comments
COMMENT ON TABLE postconfig_templates IS 'Reusable post-configuration templates created by users';
COMMENT ON COLUMN postconfig_templates.is_public IS 'If true, template is visible to all users. If false, only visible to owner.';
COMMENT ON COLUMN postconfig_templates.tags IS 'Tags for categorizing and searching templates (e.g., backup, monitoring, security)';

-- +goose Down
DROP TABLE IF EXISTS postconfig_templates;
