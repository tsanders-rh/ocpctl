-- +goose Up
-- Migration: Add POST_CONFIGURE to allowed job types
-- Created: 2026-03-17
-- Fixes: Missing job type in database constraint that prevented post-deployment jobs

-- +goose StatementBegin

-- Drop the existing constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

-- Recreate with POST_CONFIGURE included
ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check
    CHECK (job_type IN (
        'CREATE',
        'DESTROY',
        'SCALE_WORKERS',
        'JANITOR_DESTROY',
        'ORPHAN_SWEEP',
        'CONFIGURE_EFS',
        'PROVISION_SHARED_STORAGE',
        'UNLINK_SHARED_STORAGE',
        'HIBERNATE',
        'RESUME',
        'POST_CONFIGURE'
    ));

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

-- Revert to original constraint without POST_CONFIGURE
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check
    CHECK (job_type IN (
        'CREATE',
        'DESTROY',
        'SCALE_WORKERS',
        'JANITOR_DESTROY',
        'ORPHAN_SWEEP',
        'CONFIGURE_EFS',
        'PROVISION_SHARED_STORAGE',
        'UNLINK_SHARED_STORAGE',
        'HIBERNATE',
        'RESUME'
    ));

-- +goose StatementEnd
