-- +goose Up
-- +goose StatementBegin

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Idempotency keys table (CRITICAL: prevents duplicate requests)
CREATE TABLE idempotency_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key VARCHAR(255) NOT NULL UNIQUE,
    request_hash VARCHAR(64) NOT NULL,
    response_status_code INTEGER,
    response_body JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE INDEX idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);
CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys(created_at);

-- RBAC mappings table (CRITICAL: IAM to team/role mappings)
CREATE TABLE rbac_mappings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    iam_principal_arn VARCHAR(512) NOT NULL,
    iam_principal_type VARCHAR(50) NOT NULL CHECK (iam_principal_type IN ('user', 'role', 'group')),
    team VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL CHECK (role IN ('requester', 'platform_admin', 'auditor')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(512),
    notes TEXT
);

CREATE INDEX idx_rbac_mappings_iam_principal ON rbac_mappings(iam_principal_arn, enabled);
CREATE INDEX idx_rbac_mappings_team ON rbac_mappings(team, enabled);

-- Clusters table (main inventory)
CREATE TABLE clusters (
    id VARCHAR(64) PRIMARY KEY, -- Format: clu_01J...
    name VARCHAR(255) NOT NULL,
    platform VARCHAR(50) NOT NULL CHECK (platform IN ('aws', 'ibmcloud')),
    version VARCHAR(50) NOT NULL,
    profile VARCHAR(100) NOT NULL,
    region VARCHAR(50) NOT NULL,
    base_domain VARCHAR(255) NOT NULL,
    owner VARCHAR(255) NOT NULL,
    team VARCHAR(255) NOT NULL,
    cost_center VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL CHECK (status IN ('PENDING', 'CREATING', 'READY', 'DESTROYING', 'DESTROYED', 'FAILED')),
    requested_by VARCHAR(512) NOT NULL, -- IAM principal ARN
    ttl_hours INTEGER NOT NULL,
    destroy_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    destroyed_at TIMESTAMP WITH TIME ZONE,
    request_tags JSONB,
    effective_tags JSONB,
    ssh_public_key TEXT,
    offhours_opt_in BOOLEAN NOT NULL DEFAULT FALSE
);

-- Unique constraint: only one active cluster with same name per platform/domain
CREATE UNIQUE INDEX idx_unique_active_cluster
ON clusters(name, platform, base_domain)
WHERE status NOT IN ('DESTROYED', 'FAILED');

CREATE INDEX idx_clusters_status ON clusters(status);
CREATE INDEX idx_clusters_team ON clusters(team);
CREATE INDEX idx_clusters_owner ON clusters(owner);
CREATE INDEX idx_clusters_destroy_at ON clusters(destroy_at) WHERE destroy_at IS NOT NULL;
CREATE INDEX idx_clusters_platform ON clusters(platform);
CREATE INDEX idx_clusters_created_at ON clusters(created_at);

-- Cluster outputs table (access credentials and endpoints)
CREATE TABLE cluster_outputs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cluster_id VARCHAR(64) NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    api_url VARCHAR(512),
    console_url VARCHAR(512),
    kubeconfig_s3_uri VARCHAR(1024),
    kubeadmin_secret_ref VARCHAR(512), -- AWS Secrets Manager ARN
    metadata_s3_uri VARCHAR(1024),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_cluster_outputs_cluster_id ON cluster_outputs(cluster_id);

-- Cluster artifacts table (S3 state snapshots and logs)
CREATE TABLE cluster_artifacts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cluster_id VARCHAR(64) NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    artifact_type VARCHAR(100) NOT NULL CHECK (artifact_type IN ('INSTALL_DIR_SNAPSHOT', 'LOG', 'AUTH_BUNDLE', 'METADATA', 'DESTROY_LOG')),
    s3_uri VARCHAR(1024) NOT NULL,
    checksum VARCHAR(128),
    size_bytes BIGINT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cluster_artifacts_cluster_id ON cluster_artifacts(cluster_id);
CREATE INDEX idx_cluster_artifacts_type ON cluster_artifacts(artifact_type);

-- Jobs table (async operation tracking)
CREATE TABLE jobs (
    id VARCHAR(64) PRIMARY KEY, -- Format: job_01J...
    cluster_id VARCHAR(64) NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    job_type VARCHAR(50) NOT NULL CHECK (job_type IN ('CREATE', 'DESTROY', 'SCALE_WORKERS', 'JANITOR_DESTROY', 'ORPHAN_SWEEP')),
    status VARCHAR(50) NOT NULL CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'RETRYING')),
    attempt INTEGER NOT NULL DEFAULT 1,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    error_code VARCHAR(100),
    error_message TEXT,
    started_at TIMESTAMP WITH TIME ZONE,
    ended_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    metadata JSONB
);

CREATE INDEX idx_jobs_cluster_id ON jobs(cluster_id);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_type ON jobs(job_type);
CREATE INDEX idx_jobs_created_at ON jobs(created_at);

-- Job lock to prevent concurrent operations on same cluster (CRITICAL)
CREATE TABLE job_locks (
    cluster_id VARCHAR(64) PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
    job_id VARCHAR(64) NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    locked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    locked_by VARCHAR(255) NOT NULL, -- worker instance ID
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE INDEX idx_job_locks_expires_at ON job_locks(expires_at);

-- Audit events table (immutable audit trail)
CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    actor VARCHAR(512) NOT NULL, -- IAM principal ARN
    action VARCHAR(100) NOT NULL,
    target_cluster_id VARCHAR(64) REFERENCES clusters(id) ON DELETE SET NULL,
    target_job_id VARCHAR(64),
    status VARCHAR(50) NOT NULL CHECK (status IN ('SUCCESS', 'FAILURE', 'DENIED')),
    metadata JSONB,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_events_actor ON audit_events(actor);
CREATE INDEX idx_audit_events_cluster_id ON audit_events(target_cluster_id);
CREATE INDEX idx_audit_events_created_at ON audit_events(created_at);
CREATE INDEX idx_audit_events_action ON audit_events(action);

-- Usage samples table (cost tracking)
CREATE TABLE usage_samples (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cluster_id VARCHAR(64) NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    sample_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    state VARCHAR(50) NOT NULL,
    control_plane_replicas INTEGER NOT NULL,
    worker_replicas INTEGER NOT NULL,
    estimated_hourly_cost DECIMAL(10, 4),
    metadata JSONB
);

CREATE INDEX idx_usage_samples_cluster_id ON usage_samples(cluster_id);
CREATE INDEX idx_usage_samples_time ON usage_samples(sample_time);
CREATE UNIQUE INDEX idx_usage_samples_cluster_time ON usage_samples(cluster_id, sample_time);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS usage_samples CASCADE;
DROP TABLE IF EXISTS audit_events CASCADE;
DROP TABLE IF EXISTS job_locks CASCADE;
DROP TABLE IF EXISTS jobs CASCADE;
DROP TABLE IF EXISTS cluster_artifacts CASCADE;
DROP TABLE IF EXISTS cluster_outputs CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS rbac_mappings CASCADE;
DROP TABLE IF EXISTS idempotency_keys CASCADE;

-- +goose StatementEnd
