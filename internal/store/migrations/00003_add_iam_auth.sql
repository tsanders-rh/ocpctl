-- +goose Up
-- Migration: Add IAM Authentication Support
-- Description: Adds IAM principal mapping table for AWS IAM authentication
-- Date: 2026-02-28

-- IAM Principal Mappings Table
-- Maps AWS IAM principals (users, roles) to internal user records
CREATE TABLE iam_principal_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    iam_principal_arn VARCHAR(255) UNIQUE NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Index for fast lookups by IAM principal ARN
CREATE INDEX idx_iam_principals_arn ON iam_principal_mappings(iam_principal_arn);

-- Index for lookups by user_id
CREATE INDEX idx_iam_principals_user ON iam_principal_mappings(user_id);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_iam_mapping_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to automatically update updated_at
CREATE TRIGGER trg_iam_mapping_updated_at
    BEFORE UPDATE ON iam_principal_mappings
    FOR EACH ROW
    EXECUTE FUNCTION update_iam_mapping_updated_at();

-- Add comment for documentation
COMMENT ON TABLE iam_principal_mappings IS 'Maps AWS IAM principals to internal user records for IAM authentication';
COMMENT ON COLUMN iam_principal_mappings.iam_principal_arn IS 'AWS IAM principal ARN (arn:aws:iam::account-id:user/username or role/rolename)';
COMMENT ON COLUMN iam_principal_mappings.user_id IS 'Reference to internal users table';
COMMENT ON COLUMN iam_principal_mappings.enabled IS 'Whether this mapping is active (allows disabling without deletion)';

-- +goose Down
DROP TRIGGER IF EXISTS trg_iam_mapping_updated_at ON iam_principal_mappings;
DROP FUNCTION IF EXISTS update_iam_mapping_updated_at();
DROP TABLE IF EXISTS iam_principal_mappings;
