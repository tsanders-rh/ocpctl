-- +goose Up
-- +goose StatementBegin

-- Create profiles table to store cluster profiles in database
-- This replaces the YAML file-based profile loading with database-backed profiles
-- Profiles can be updated via API and changes are immediately visible without cache issues

CREATE TABLE IF NOT EXISTS profiles (
    name VARCHAR(255) PRIMARY KEY,
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    platform VARCHAR(50) NOT NULL,
    cluster_type VARCHAR(50) NOT NULL,
    track VARCHAR(50),
    enabled BOOLEAN DEFAULT true,
    profile_data JSONB NOT NULL, -- Full profile as JSON
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_profiles_platform ON profiles(platform) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_profiles_cluster_type ON profiles(cluster_type) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_profiles_track ON profiles(track) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_profiles_enabled ON profiles(enabled);

-- GIN index on JSONB for flexible queries
CREATE INDEX IF NOT EXISTS idx_profiles_data_gin ON profiles USING GIN (profile_data);

-- Trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_profiles_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS profiles_updated_at_trigger ON profiles;
CREATE TRIGGER profiles_updated_at_trigger
    BEFORE UPDATE ON profiles
    FOR EACH ROW
    EXECUTE FUNCTION update_profiles_updated_at();

-- Add comment
COMMENT ON TABLE profiles IS 'Cluster profiles stored in database for immediate updates without cache issues';
COMMENT ON COLUMN profiles.profile_data IS 'Full profile definition as JSONB for flexible querying';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS profiles_updated_at_trigger ON profiles;
DROP FUNCTION IF EXISTS update_profiles_updated_at();
DROP TABLE IF EXISTS profiles;

-- +goose StatementEnd
