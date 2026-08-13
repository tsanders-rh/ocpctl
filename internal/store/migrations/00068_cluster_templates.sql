-- +goose Up
-- Per-user reusable cluster-creation parameter presets
CREATE TABLE cluster_templates (
  id VARCHAR(64) PRIMARY KEY DEFAULT gen_random_uuid()::text,
  name VARCHAR(255) NOT NULL,
  owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  config JSONB NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
  UNIQUE (owner_id, name)
);

CREATE INDEX idx_cluster_templates_owner ON cluster_templates(owner_id);

COMMENT ON TABLE cluster_templates IS 'Per-user reusable cluster-creation parameter presets. Config is a partial create-cluster payload; the cluster name is never stored.';

-- +goose Down
DROP TABLE IF EXISTS cluster_templates;
