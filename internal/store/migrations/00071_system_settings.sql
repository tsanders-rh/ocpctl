-- +goose Up
-- Migration: Generic system_settings key/value store
-- Backs runtime-tunable admin controls that must be shared between the API
-- (which serves the admin console) and the worker/janitor (a SEPARATE process on
-- a SEPARATE host). The database is the only channel between them, so settings a
-- human toggles in the console -- e.g. orphaned-resource auto-remediation mode --
-- land here and the janitor reads them per-cycle without a restart. The janitor
-- also writes a per-cycle telemetry row back through the same table.
-- Created: 2026-09-04

-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS system_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_by TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE system_settings IS 'Generic key/value store for runtime-tunable admin settings shared between the API and the worker/janitor via the database (issue #101 phase 4)';
COMMENT ON COLUMN system_settings.key IS 'Setting key, e.g. orphan_auto_remediation (config) or orphan_auto_remediation_status (janitor-written telemetry)';
COMMENT ON COLUMN system_settings.value IS 'Arbitrary JSON payload for the setting; shape is owned by the reading/writing code, not the schema';
COMMENT ON COLUMN system_settings.updated_by IS 'Actor that last wrote this row (admin email for console writes, "janitor" for telemetry)';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

DROP TABLE IF EXISTS system_settings;

-- +goose StatementEnd
