-- +goose Up
-- Migration: Add POOL_REPLENISH job type
-- Date: 2026-05-22
-- Description: Add POOL_REPLENISH to the allowed job types for cluster pool management

-- Drop the existing constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

-- Recreate the constraint with POOL_REPLENISH added
ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check CHECK (
    job_type IN (
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
        'POST_CONFIGURE',
        'POOL_REPLENISH'
    )
);

-- +goose Down
-- Rollback: Remove POOL_REPLENISH from job types
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check CHECK (
    job_type IN (
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
    )
);
