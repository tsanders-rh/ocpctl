-- +goose Up
-- Add POOL_CLEAN to jobs_job_type_check constraint

-- Drop the existing constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

-- Recreate the constraint with POOL_CLEAN added
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
        'POOL_REPLENISH',
        'POOL_CLEAN'
    )
);

-- +goose Down
-- Remove POOL_CLEAN from jobs_job_type_check constraint

-- Drop the constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

-- Recreate the constraint without POOL_CLEAN
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
