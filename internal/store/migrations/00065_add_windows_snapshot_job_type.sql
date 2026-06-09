-- +goose Up
-- Add CREATE_WINDOWS_SNAPSHOT to jobs_job_type_check constraint

-- Drop the existing constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

-- Recreate the constraint with CREATE_WINDOWS_SNAPSHOT added
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
        'POOL_CLEAN',
        'CREATE_WINDOWS_SNAPSHOT'
    )
);

-- +goose Down
-- Remove CREATE_WINDOWS_SNAPSHOT from jobs_job_type_check constraint

-- Drop the constraint
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;

-- Recreate the constraint without CREATE_WINDOWS_SNAPSHOT
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
