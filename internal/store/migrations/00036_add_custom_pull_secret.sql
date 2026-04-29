-- Add custom_pull_secret column to clusters table
-- This allows users to provide additional registry credentials that are merged with the standard pull secret
-- Common use case: accessing pre-release builds from quay.io/openshift-cnv or other private registries

-- +goose Up
ALTER TABLE clusters
ADD COLUMN custom_pull_secret TEXT;

COMMENT ON COLUMN clusters.custom_pull_secret IS 'Optional custom pull secret JSON (encrypted) to merge with standard pull secret. Format: {"auths": {"registry": {"auth": "..."}}}';

-- +goose Down
ALTER TABLE clusters
DROP COLUMN custom_pull_secret;
