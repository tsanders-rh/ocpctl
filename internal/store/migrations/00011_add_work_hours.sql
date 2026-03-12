-- +goose Up
-- Migration: Add work hours support for automatic cluster hibernation

-- Add work hours configuration to users table (default work hours)
ALTER TABLE users ADD COLUMN work_hours_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN work_hours_start TIME NOT NULL DEFAULT '09:00:00';
ALTER TABLE users ADD COLUMN work_hours_end TIME NOT NULL DEFAULT '17:00:00';
ALTER TABLE users ADD COLUMN work_days SMALLINT NOT NULL DEFAULT 62; -- Binary: 0111110 = Mon-Fri (bits 1-5)

CREATE INDEX idx_users_work_hours_enabled ON users(work_hours_enabled) WHERE work_hours_enabled = TRUE;

COMMENT ON COLUMN users.work_days IS 'Bitmask for work days: bit 0=Sunday, 1=Monday, ..., 6=Saturday';

-- Add work hours override to clusters table
ALTER TABLE clusters ADD COLUMN work_hours_enabled BOOLEAN;
ALTER TABLE clusters ADD COLUMN work_hours_start TIME;
ALTER TABLE clusters ADD COLUMN work_hours_end TIME;
ALTER TABLE clusters ADD COLUMN work_days SMALLINT;
ALTER TABLE clusters ADD COLUMN last_work_hours_check TIMESTAMP WITH TIME ZONE;

CREATE INDEX idx_clusters_work_hours_enabled ON clusters(work_hours_enabled) WHERE work_hours_enabled = TRUE AND status = 'READY';

COMMENT ON COLUMN clusters.work_hours_enabled IS 'NULL = use user default, TRUE/FALSE = cluster-specific override';
COMMENT ON COLUMN clusters.last_work_hours_check IS 'Timestamp of last work hours enforcement check (prevents duplicate actions)';

-- Add new cluster states for hibernation
ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_status_check;
ALTER TABLE clusters ADD CONSTRAINT clusters_status_check
  CHECK (status IN ('PENDING', 'CREATING', 'READY', 'HIBERNATING', 'HIBERNATED', 'RESUMING', 'DESTROYING', 'DESTROYED', 'FAILED'));

-- Add new job types for hibernation
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check
  CHECK (job_type IN ('CREATE', 'DESTROY', 'SCALE_WORKERS', 'JANITOR_DESTROY', 'ORPHAN_SWEEP', 'CONFIGURE_EFS', 'PROVISION_SHARED_STORAGE', 'UNLINK_SHARED_STORAGE', 'HIBERNATE', 'RESUME'));

-- +goose Down
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_job_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_job_type_check
  CHECK (job_type IN ('CREATE', 'DESTROY', 'SCALE_WORKERS', 'JANITOR_DESTROY', 'ORPHAN_SWEEP', 'CONFIGURE_EFS', 'PROVISION_SHARED_STORAGE', 'UNLINK_SHARED_STORAGE'));

ALTER TABLE clusters DROP CONSTRAINT IF EXISTS clusters_status_check;
ALTER TABLE clusters ADD CONSTRAINT clusters_status_check
  CHECK (status IN ('PENDING', 'CREATING', 'READY', 'DESTROYING', 'DESTROYED', 'FAILED'));

ALTER TABLE clusters DROP COLUMN IF EXISTS last_work_hours_check;
ALTER TABLE clusters DROP COLUMN IF EXISTS work_days;
ALTER TABLE clusters DROP COLUMN IF EXISTS work_hours_end;
ALTER TABLE clusters DROP COLUMN IF EXISTS work_hours_start;
ALTER TABLE clusters DROP COLUMN IF EXISTS work_hours_enabled;

DROP INDEX IF EXISTS idx_clusters_work_hours_enabled;

ALTER TABLE users DROP COLUMN IF EXISTS work_days;
ALTER TABLE users DROP COLUMN IF EXISTS work_hours_end;
ALTER TABLE users DROP COLUMN IF EXISTS work_hours_start;
ALTER TABLE users DROP COLUMN IF EXISTS work_hours_enabled;

DROP INDEX IF EXISTS idx_users_work_hours_enabled;
