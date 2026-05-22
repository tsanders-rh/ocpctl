-- Create cluster_pools table for managing pre-provisioned cluster pools
-- Migration: 00050

-- +goose Up
CREATE TABLE IF NOT EXISTS cluster_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    display_name VARCHAR(255) NOT NULL,
    description TEXT,
    profile VARCHAR(100) NOT NULL,

    -- Pool sizing
    target_size INTEGER NOT NULL DEFAULT 3,
    min_size INTEGER NOT NULL DEFAULT 1,
    max_size INTEGER NOT NULL DEFAULT 10,

    -- Lease configuration
    max_lease_duration_hours INTEGER NOT NULL DEFAULT 2,
    auto_release_enabled BOOLEAN NOT NULL DEFAULT true,

    -- Cluster lifecycle
    max_cluster_age_days INTEGER NOT NULL DEFAULT 7,
    auto_refresh_enabled BOOLEAN NOT NULL DEFAULT true,

    -- Scheduling (work hours mode)
    scheduled_mode BOOLEAN NOT NULL DEFAULT false,
    schedule_timezone VARCHAR(50) DEFAULT 'America/New_York',
    schedule_start_hour INTEGER DEFAULT 8,  -- 8am
    schedule_end_hour INTEGER DEFAULT 18,   -- 6pm
    schedule_days_of_week INTEGER[] DEFAULT ARRAY[1,2,3,4,5], -- Mon-Fri

    -- Configuration overrides (JSON)
    cluster_config JSONB DEFAULT '{}',

    -- Metadata
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(255),

    -- Constraints
    CONSTRAINT valid_pool_size CHECK (min_size <= target_size AND target_size <= max_size),
    CONSTRAINT valid_schedule_hours CHECK (schedule_start_hour >= 0 AND schedule_start_hour <= 23
                                            AND schedule_end_hour >= 0 AND schedule_end_hour <= 23),
    CONSTRAINT valid_max_age CHECK (max_cluster_age_days > 0),
    CONSTRAINT valid_max_lease CHECK (max_lease_duration_hours > 0)
);

-- Create indexes for common queries
CREATE INDEX idx_cluster_pools_profile ON cluster_pools(profile);
CREATE INDEX idx_cluster_pools_enabled ON cluster_pools(enabled);
CREATE INDEX idx_cluster_pools_scheduled_mode ON cluster_pools(scheduled_mode);

-- Comments for documentation
COMMENT ON TABLE cluster_pools IS 'Pre-provisioned cluster pools for fast CI/CD integration';
COMMENT ON COLUMN cluster_pools.target_size IS 'Desired number of READY clusters in pool';
COMMENT ON COLUMN cluster_pools.min_size IS 'Minimum pool size (triggers replenishment)';
COMMENT ON COLUMN cluster_pools.max_size IS 'Maximum pool size (prevents over-provisioning)';
COMMENT ON COLUMN cluster_pools.max_lease_duration_hours IS 'Auto-release leased clusters after this duration';
COMMENT ON COLUMN cluster_pools.max_cluster_age_days IS 'Auto-refresh clusters older than this';
COMMENT ON COLUMN cluster_pools.scheduled_mode IS 'Enable work hours only mode (destroys clusters outside hours)';
COMMENT ON COLUMN cluster_pools.cluster_config IS 'JSON overrides for cluster creation (region, ttl, tags, etc.)';

-- +goose Down
DROP TABLE IF EXISTS cluster_pools;
