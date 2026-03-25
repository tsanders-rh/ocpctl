-- +goose Up
-- +goose StatementBegin
-- Create table for tracking deployment time metrics by profile
-- This table stores pre-calculated statistics from the last 30 successful deployments
-- Updated periodically by the janitor service (every 5 minutes)
CREATE TABLE profile_deployment_metrics (
    profile VARCHAR(255) PRIMARY KEY,
    avg_duration_seconds INTEGER NOT NULL,
    min_duration_seconds INTEGER NOT NULL,
    max_duration_seconds INTEGER NOT NULL,
    p50_duration_seconds INTEGER,  -- Median deployment time (50th percentile)
    p95_duration_seconds INTEGER,  -- 95th percentile deployment time
    sample_count INTEGER NOT NULL DEFAULT 0,  -- Number of samples in calculation (max 30)
    success_count INTEGER NOT NULL DEFAULT 0,  -- Total successful deployments tracked
    last_deployment_at TIMESTAMP WITH TIME ZONE,  -- Most recent deployment timestamp
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Index for monitoring data freshness
CREATE INDEX idx_profile_deployment_metrics_updated_at ON profile_deployment_metrics(updated_at);

-- Add comments for documentation
COMMENT ON TABLE profile_deployment_metrics IS 'Pre-calculated deployment time statistics by profile, updated periodically by janitor';
COMMENT ON COLUMN profile_deployment_metrics.profile IS 'Profile name (references clusters.profile)';
COMMENT ON COLUMN profile_deployment_metrics.avg_duration_seconds IS 'Average deployment time from last 30 successful deployments';
COMMENT ON COLUMN profile_deployment_metrics.min_duration_seconds IS 'Minimum deployment time from last 30 successful deployments';
COMMENT ON COLUMN profile_deployment_metrics.max_duration_seconds IS 'Maximum deployment time from last 30 successful deployments';
COMMENT ON COLUMN profile_deployment_metrics.p50_duration_seconds IS 'Median deployment time (50th percentile)';
COMMENT ON COLUMN profile_deployment_metrics.p95_duration_seconds IS '95th percentile deployment time';
COMMENT ON COLUMN profile_deployment_metrics.sample_count IS 'Number of samples used in calculation (max 30 for rolling window)';
COMMENT ON COLUMN profile_deployment_metrics.success_count IS 'Total count of successful deployments ever tracked for this profile';
COMMENT ON COLUMN profile_deployment_metrics.last_deployment_at IS 'Timestamp of most recent successful deployment';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS profile_deployment_metrics;
-- +goose StatementEnd
