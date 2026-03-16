-- +goose Up
-- Migration: Add post-deployment configuration tracking
-- Created: 2026-03-16

-- +goose StatementBegin

-- Add post-deployment tracking to clusters table
ALTER TABLE clusters ADD COLUMN post_deploy_status VARCHAR(20);
ALTER TABLE clusters ADD COLUMN post_deploy_completed_at TIMESTAMP WITH TIME ZONE;

-- Create cluster_configurations table to track individual configuration tasks
CREATE TABLE cluster_configurations (
    id VARCHAR(64) PRIMARY KEY DEFAULT gen_random_uuid()::text,
    cluster_id VARCHAR(64) NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    config_type VARCHAR(50) NOT NULL,  -- 'operator', 'manifest', 'helm'
    config_name VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',  -- 'pending', 'installing', 'completed', 'failed'
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE,
    metadata JSONB,  -- Store additional config-specific data

    -- Indexes
    CONSTRAINT cluster_configurations_status_check CHECK (status IN ('pending', 'installing', 'completed', 'failed')),
    CONSTRAINT cluster_configurations_type_check CHECK (config_type IN ('operator', 'manifest', 'helm'))
);

-- Indexes for cluster_configurations
CREATE INDEX idx_cluster_configurations_cluster_id ON cluster_configurations(cluster_id);
CREATE INDEX idx_cluster_configurations_status ON cluster_configurations(status);
CREATE INDEX idx_cluster_configurations_created_at ON cluster_configurations(created_at DESC);

-- Comments
COMMENT ON TABLE cluster_configurations IS 'Tracks individual post-deployment configuration tasks for clusters';
COMMENT ON COLUMN cluster_configurations.config_type IS 'Type of configuration: operator, manifest, or helm';
COMMENT ON COLUMN cluster_configurations.config_name IS 'Name of the operator, manifest, or helm chart';
COMMENT ON COLUMN cluster_configurations.metadata IS 'JSON metadata about the configuration (e.g., operator channel, manifest path)';
COMMENT ON COLUMN clusters.post_deploy_status IS 'Overall post-deployment configuration status: pending, in_progress, completed, failed';
COMMENT ON COLUMN clusters.post_deploy_completed_at IS 'Timestamp when post-deployment configuration completed';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Drop cluster_configurations table
DROP TABLE IF EXISTS cluster_configurations;

-- Remove post-deployment columns from clusters table
ALTER TABLE clusters DROP COLUMN IF EXISTS post_deploy_status;
ALTER TABLE clusters DROP COLUMN IF EXISTS post_deploy_completed_at;

-- +goose StatementEnd
