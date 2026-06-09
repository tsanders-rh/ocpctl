-- Windows regional snapshots tracking
-- This table tracks pre-created EBS snapshots per region for fast Windows VM provisioning

CREATE TABLE IF NOT EXISTS windows_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    region VARCHAR(50) NOT NULL,
    version VARCHAR(20) NOT NULL,
    ebs_snapshot_id VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'creating',
    ssm_parameter_path VARCHAR(255),
    s3_source_url TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    validated_at TIMESTAMP,
    error_message TEXT,
    job_id UUID REFERENCES jobs(id) ON DELETE SET NULL,

    -- Metadata
    snapshot_size_gb INTEGER,
    validation_vm_booted BOOLEAN DEFAULT false,

    CONSTRAINT windows_snapshots_region_version_unique UNIQUE(region, version),
    CONSTRAINT windows_snapshots_status_check CHECK (status IN ('creating', 'validating', 'ready', 'failed', 'deleting'))
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_windows_snapshots_region ON windows_snapshots(region);
CREATE INDEX IF NOT EXISTS idx_windows_snapshots_status ON windows_snapshots(status);
CREATE INDEX IF NOT EXISTS idx_windows_snapshots_version ON windows_snapshots(version);
CREATE INDEX IF NOT EXISTS idx_windows_snapshots_job_id ON windows_snapshots(job_id);

-- Audit logging
CREATE TABLE IF NOT EXISTS windows_snapshot_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id UUID NOT NULL REFERENCES windows_snapshots(id) ON DELETE CASCADE,
    action VARCHAR(50) NOT NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    details JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_windows_snapshot_audit_snapshot_id ON windows_snapshot_audit(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_windows_snapshot_audit_created_at ON windows_snapshot_audit(created_at);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_windows_snapshots_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-update updated_at
CREATE TRIGGER windows_snapshots_updated_at
    BEFORE UPDATE ON windows_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION update_windows_snapshots_updated_at();
