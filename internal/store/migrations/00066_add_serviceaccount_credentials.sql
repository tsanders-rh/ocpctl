-- +goose Up
ALTER TABLE cluster_outputs
ADD COLUMN sa_name TEXT,
ADD COLUMN sa_namespace TEXT DEFAULT 'default',
ADD COLUMN sa_token TEXT,
ADD COLUMN sa_token_expires_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN oc_login_command TEXT;

COMMENT ON COLUMN cluster_outputs.sa_name IS 'ServiceAccount name for pool cluster leasing';
COMMENT ON COLUMN cluster_outputs.sa_namespace IS 'ServiceAccount namespace (default: default)';
COMMENT ON COLUMN cluster_outputs.sa_token IS 'Time-bound ServiceAccount token (expires with lease)';
COMMENT ON COLUMN cluster_outputs.sa_token_expires_at IS 'Token expiration timestamp (matches lease_expires_at)';
COMMENT ON COLUMN cluster_outputs.oc_login_command IS 'Ready-to-use oc login command with token';

-- +goose Down
ALTER TABLE cluster_outputs
DROP COLUMN sa_name,
DROP COLUMN sa_namespace,
DROP COLUMN sa_token,
DROP COLUMN sa_token_expires_at,
DROP COLUMN oc_login_command;
