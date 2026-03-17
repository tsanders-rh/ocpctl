-- +goose Up
-- Migration: Add skip_post_deployment column to clusters table
-- Created: 2026-03-16

-- +goose StatementBegin

-- Add skip_post_deployment column to clusters table
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS skip_post_deployment BOOLEAN NOT NULL DEFAULT FALSE;

-- Add comment
COMMENT ON COLUMN clusters.skip_post_deployment IS 'When true, skip automatic post-deployment configuration even if profile has it enabled';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Remove skip_post_deployment column from clusters table
ALTER TABLE clusters DROP COLUMN IF EXISTS skip_post_deployment;

-- +goose StatementEnd
