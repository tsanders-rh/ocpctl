-- Migration: Add new orphaned resource types for comprehensive leak detection
-- Purpose: Track EBS volumes, Elastic IPs, and CloudWatch log groups
-- Created: 2026-03-23

-- +goose Up

-- Drop the old CHECK constraint
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;

-- Add new CHECK constraint with additional resource types
ALTER TABLE orphaned_resources ADD CONSTRAINT orphaned_resources_resource_type_check
    CHECK (resource_type IN (
        'VPC',
        'LoadBalancer',
        'DNSRecord',
        'EC2Instance',
        'HostedZone',
        'IAMRole',
        'OIDCProvider',
        'EBSVolume',
        'ElasticIP',
        'CloudWatchLogGroup'
    ));

COMMENT ON CONSTRAINT orphaned_resources_resource_type_check ON orphaned_resources IS
    'Validates resource_type values including EBS volumes, Elastic IPs, and CloudWatch log groups for comprehensive leak detection';

-- +goose Down

-- Revert to original CHECK constraint
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;

ALTER TABLE orphaned_resources ADD CONSTRAINT orphaned_resources_resource_type_check
    CHECK (resource_type IN (
        'VPC',
        'LoadBalancer',
        'DNSRecord',
        'EC2Instance'
    ));
