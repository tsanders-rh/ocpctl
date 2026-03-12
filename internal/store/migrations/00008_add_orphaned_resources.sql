-- +goose Up
-- Migration: Add orphaned_resources table for tracking AWS resources without matching clusters
-- Created: 2026-03-11

-- +goose StatementBegin

-- Table to store discovered orphaned AWS resources
CREATE TABLE IF NOT EXISTS orphaned_resources (
    id VARCHAR(64) PRIMARY KEY,
    resource_type VARCHAR(50) NOT NULL CHECK (resource_type IN ('VPC', 'LoadBalancer', 'DNSRecord', 'EC2Instance')),
    resource_id VARCHAR(255) NOT NULL,
    resource_name VARCHAR(255),
    region VARCHAR(50) NOT NULL,
    cluster_name VARCHAR(255),
    tags JSONB DEFAULT '{}'::jsonb,
    first_detected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_detected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    detection_count INT NOT NULL DEFAULT 1,
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'RESOLVED', 'IGNORED')),
    resolved_at TIMESTAMP WITH TIME ZONE,
    resolved_by VARCHAR(255),
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    UNIQUE(resource_type, resource_id, region)
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_orphaned_resources_status ON orphaned_resources(status);
CREATE INDEX IF NOT EXISTS idx_orphaned_resources_type ON orphaned_resources(resource_type);
CREATE INDEX IF NOT EXISTS idx_orphaned_resources_region ON orphaned_resources(region);
CREATE INDEX IF NOT EXISTS idx_orphaned_resources_first_detected ON orphaned_resources(first_detected_at);
CREATE INDEX IF NOT EXISTS idx_orphaned_resources_cluster_name ON orphaned_resources(cluster_name);

-- GIN index for JSONB tags
CREATE INDEX IF NOT EXISTS idx_orphaned_resources_tags ON orphaned_resources USING GIN (tags);

-- Comments for documentation
COMMENT ON TABLE orphaned_resources IS 'Tracks AWS resources that exist but have no matching cluster in the database';
COMMENT ON COLUMN orphaned_resources.resource_type IS 'Type of AWS resource: VPC, LoadBalancer, DNSRecord, or EC2Instance';
COMMENT ON COLUMN orphaned_resources.resource_id IS 'AWS resource identifier (vpc-xxx, i-xxx, arn, etc.)';
COMMENT ON COLUMN orphaned_resources.cluster_name IS 'Extracted cluster name from resource tags or name';
COMMENT ON COLUMN orphaned_resources.detection_count IS 'Number of times this orphan has been detected';
COMMENT ON COLUMN orphaned_resources.status IS 'Current status: ACTIVE (needs attention), RESOLVED (cleaned up), IGNORED (false positive)';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

DROP TABLE IF EXISTS orphaned_resources;

-- +goose StatementEnd
