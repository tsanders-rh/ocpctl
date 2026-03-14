-- +goose Up
-- Migration: Add IAMRole and OIDCProvider types to orphaned_resources
-- Created: 2026-03-14

-- +goose StatementBegin

-- Drop the old constraint
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;

-- Add the new constraint with IAMRole and OIDCProvider
ALTER TABLE orphaned_resources ADD CONSTRAINT orphaned_resources_resource_type_check
    CHECK (resource_type IN ('VPC', 'LoadBalancer', 'DNSRecord', 'EC2Instance', 'HostedZone', 'IAMRole', 'OIDCProvider'));

-- Update the comment
COMMENT ON COLUMN orphaned_resources.resource_type IS 'Type of AWS resource: VPC, LoadBalancer, DNSRecord, EC2Instance, HostedZone, IAMRole, or OIDCProvider';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Revert to old constraint
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;
ALTER TABLE orphaned_resources ADD CONSTRAINT orphaned_resources_resource_type_check
    CHECK (resource_type IN ('VPC', 'LoadBalancer', 'DNSRecord', 'EC2Instance', 'HostedZone'));

-- Revert comment
COMMENT ON COLUMN orphaned_resources.resource_type IS 'Type of AWS resource: VPC, LoadBalancer, DNSRecord, EC2Instance, or HostedZone';

-- +goose StatementEnd
