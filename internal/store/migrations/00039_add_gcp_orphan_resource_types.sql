-- Migration: Add GCP orphaned resource types
-- Purpose: Track GCP resources (service accounts, networks, disks, etc.) for comprehensive leak detection
-- Created: 2026-04-29

-- +goose Up

-- Drop the old CHECK constraint
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;

-- Add new CHECK constraint with GCP resource types
ALTER TABLE orphaned_resources ADD CONSTRAINT orphaned_resources_resource_type_check
    CHECK (resource_type IN (
        -- AWS Resources
        'VPC',
        'LoadBalancer',
        'DNSRecord',
        'EC2Instance',
        'HostedZone',
        'IAMRole',
        'OIDCProvider',
        'EBSVolume',
        'ElasticIP',
        'CloudWatchLogGroup',
        -- GCP Resources
        'GCPServiceAccount',
        'GCPNetwork',
        'GCPSubnetwork',
        'GCPDisk',
        'GCPInstance',
        'GCPBucket',
        'GCPIPAddress'
    ));

COMMENT ON CONSTRAINT orphaned_resources_resource_type_check ON orphaned_resources IS
    'Validates resource_type values including AWS and GCP resources for comprehensive leak detection';

-- +goose Down

-- Revert to previous CHECK constraint (AWS only)
ALTER TABLE orphaned_resources DROP CONSTRAINT IF EXISTS orphaned_resources_resource_type_check;

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
