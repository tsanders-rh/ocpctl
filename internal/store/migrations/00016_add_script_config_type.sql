-- +goose Up
-- Migration: Add 'script' to cluster_configurations config_type constraint
-- Created: 2026-03-18
-- This allows post-deployment scripts to be tracked as configuration tasks

-- +goose StatementBegin

-- Drop the existing constraint
ALTER TABLE cluster_configurations DROP CONSTRAINT IF EXISTS cluster_configurations_type_check;

-- Recreate the constraint with 'script' included
ALTER TABLE cluster_configurations ADD CONSTRAINT cluster_configurations_type_check
    CHECK (config_type IN ('operator', 'script', 'manifest', 'helm'));

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Revert to original constraint without 'script'
ALTER TABLE cluster_configurations DROP CONSTRAINT IF EXISTS cluster_configurations_type_check;

ALTER TABLE cluster_configurations ADD CONSTRAINT cluster_configurations_type_check
    CHECK (config_type IN ('operator', 'manifest', 'helm'));

-- +goose StatementEnd

