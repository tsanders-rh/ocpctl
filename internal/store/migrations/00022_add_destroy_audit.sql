-- Migration: Add destroy_audit table for tracking cluster destruction attempts
-- Purpose: Forensic logging and reconciliation for destroy operations
-- Related to: DESTROY_VERIFYING and DESTROY_FAILED states

-- +goose Up
CREATE TABLE IF NOT EXISTS destroy_audit (
    id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    worker_id TEXT NOT NULL,
    destroy_started_at TIMESTAMPTZ NOT NULL,
    last_verified_at TIMESTAMPTZ,
    verification_passed BOOLEAN,
    last_resource_present TEXT,
    terminal_reason TEXT,
    resources_snapshot JSONB,
    verification_snapshot JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

-- Index for querying by cluster
CREATE INDEX idx_destroy_audit_cluster_id ON destroy_audit(cluster_id);

-- Index for querying by job
CREATE INDEX idx_destroy_audit_job_id ON destroy_audit(job_id);

-- Index for finding incomplete destroy attempts (for reconciliation)
CREATE INDEX idx_destroy_audit_incomplete ON destroy_audit(cluster_id, completed_at) WHERE completed_at IS NULL;

-- Index for finding failed verifications (for drift detection)
CREATE INDEX idx_destroy_audit_failed_verification ON destroy_audit(verification_passed, last_verified_at) WHERE verification_passed = false;

COMMENT ON TABLE destroy_audit IS 'Forensic log of cluster destruction attempts for verification and reconciliation';
COMMENT ON COLUMN destroy_audit.worker_id IS 'Hostname or ID of worker that executed destroy';
COMMENT ON COLUMN destroy_audit.verification_passed IS 'NULL=not verified, true=verified clean, false=resources remain';
COMMENT ON COLUMN destroy_audit.last_resource_present IS 'Last remaining resource detected during verification';
COMMENT ON COLUMN destroy_audit.terminal_reason IS 'Final outcome: success, timeout, error, etc.';
COMMENT ON COLUMN destroy_audit.resources_snapshot IS 'AWS resource state before destroy (for reconciliation)';
COMMENT ON COLUMN destroy_audit.verification_snapshot IS 'AWS resource state after destroy (for verification)';

-- +goose Down
DROP TABLE IF EXISTS destroy_audit;
