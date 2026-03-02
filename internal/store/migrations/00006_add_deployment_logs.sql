-- +goose Up
-- +goose StatementBegin

-- Migration: Add deployment_logs table for real-time log streaming
-- This table stores incremental deployment logs from cluster create/destroy operations
-- Enables web UI to display logs without SSH access

CREATE TABLE deployment_logs (
    id BIGSERIAL PRIMARY KEY,
    cluster_id VARCHAR(64) NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    job_id VARCHAR(64) NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    log_level VARCHAR(20),
    message TEXT NOT NULL,
    source VARCHAR(50) NOT NULL DEFAULT 'installer',

    CONSTRAINT unique_cluster_job_sequence UNIQUE(cluster_id, job_id, sequence)
);

-- Index for primary query pattern: fetch logs by cluster and job, ordered by sequence
CREATE INDEX idx_deployment_logs_cluster_job
    ON deployment_logs(cluster_id, job_id);

-- Index for cursor-based pagination: fetch logs after specific sequence
CREATE INDEX idx_deployment_logs_sequence
    ON deployment_logs(cluster_id, job_id, sequence);

-- Index for timestamp-based queries (optional, for time-range filtering)
CREATE INDEX idx_deployment_logs_timestamp
    ON deployment_logs(timestamp);

-- Partial index for error/warning filtering (space-efficient, only indexes problematic logs)
CREATE INDEX idx_deployment_logs_level
    ON deployment_logs(log_level)
    WHERE log_level IN ('error', 'warn');

-- Add comment for documentation
COMMENT ON TABLE deployment_logs IS 'Stores deployment logs from cluster creation and destruction operations for real-time viewing in web UI';
COMMENT ON COLUMN deployment_logs.sequence IS 'Monotonic sequence number for ordering and cursor-based pagination';
COMMENT ON COLUMN deployment_logs.log_level IS 'Extracted log level: info, warn, error, debug';
COMMENT ON COLUMN deployment_logs.source IS 'Log source: installer (openshift-install), worker (ocpctl worker), terraform (future)';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS deployment_logs;

-- +goose StatementEnd
