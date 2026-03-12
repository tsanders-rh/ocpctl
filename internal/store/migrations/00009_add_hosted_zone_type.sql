-- +goose Up
-- Migration: Add HostedZone type to orphaned_resources
-- Created: 2026-03-11

-- +goose StatementBegin

-- Drop the old constraint
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;

-- Add the new constraint with HostedZone
ALTER TABLE orphaned_resources ADD CONSTRAINT orphaned_resources_resource_type_check
    CHECK (resource_type IN ('VPC', 'LoadBalancer', 'DNSRecord', 'EC2Instance', 'HostedZone'));

-- Update the comment
COMMENT ON COLUMN orphaned_resources.resource_type IS 'Type of AWS resource: VPC, LoadBalancer, DNSRecord, EC2Instance, or HostedZone';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Revert to old constraint
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;
ALTER TABLE orphaned_resources ADD CONSTRAINT orphaned_resources_resource_type_check
    CHECK (resource_type IN ('VPC', 'LoadBalancer', 'DNSRecord', 'EC2Instance'));

-- Revert comment
COMMENT ON COLUMN orphaned_resources.resource_type IS 'Type of AWS resource: VPC, LoadBalancer, DNSRecord, or EC2Instance';

-- +goose StatementEnd
