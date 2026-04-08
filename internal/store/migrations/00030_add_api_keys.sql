-- +goose Up

-- Create api_keys table for user API key management
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    key_prefix VARCHAR(16) NOT NULL,
    key_hash VARCHAR(64) NOT NULL,
    scope VARCHAR(20) NOT NULL DEFAULT 'full_access',
    last_used_at TIMESTAMP,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMP
);

-- Create indexes for common queries
CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_active ON api_keys(user_id, revoked_at) WHERE revoked_at IS NULL;

-- Add comments
COMMENT ON TABLE api_keys IS 'API keys for programmatic access';
COMMENT ON COLUMN api_keys.name IS 'User-friendly name for the API key (e.g., "CI/CD Pipeline", "Dev Environment")';
COMMENT ON COLUMN api_keys.key_prefix IS 'First 8 chars of the key for display purposes (e.g., "ocpctl_a")';
COMMENT ON COLUMN api_keys.key_hash IS 'SHA-256 hash of the full API key';
COMMENT ON COLUMN api_keys.scope IS 'Permission scope: read_only or full_access';
COMMENT ON COLUMN api_keys.last_used_at IS 'Timestamp of last successful authentication with this key';
COMMENT ON COLUMN api_keys.expires_at IS 'Optional expiration timestamp, NULL for non-expiring keys';
COMMENT ON COLUMN api_keys.revoked_at IS 'Timestamp when key was revoked, NULL for active keys';

-- +goose Down
DROP TABLE IF EXISTS api_keys;
