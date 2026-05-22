-- Add cluster pool tracking fields to clusters table
-- Migration: 00051

-- Add pool-related columns
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS pool_id UUID REFERENCES cluster_pools(id) ON DELETE SET NULL;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS pool_state VARCHAR(20);
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS leased_by VARCHAR(255);
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS leased_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS lease_metadata JSONB DEFAULT '{}';
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS pool_generation INTEGER DEFAULT 1;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS last_cleaned_at TIMESTAMP WITH TIME ZONE;

-- Create indexes for pool queries
CREATE INDEX IF NOT EXISTS idx_clusters_pool_id ON clusters(pool_id);
CREATE INDEX IF NOT EXISTS idx_clusters_pool_state ON clusters(pool_state);
CREATE INDEX IF NOT EXISTS idx_clusters_lease_expires_at ON clusters(lease_expires_at) WHERE lease_expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_clusters_pool_id_pool_state ON clusters(pool_id, pool_state);

-- Add check constraint for valid pool states
ALTER TABLE clusters ADD CONSTRAINT valid_pool_state
    CHECK (pool_state IS NULL OR pool_state IN ('READY', 'LEASED', 'PROVISIONING', 'CLEANING', 'EXPIRED'));

-- Comments for documentation
COMMENT ON COLUMN clusters.pool_id IS 'Foreign key to cluster_pools if cluster belongs to a pool';
COMMENT ON COLUMN clusters.pool_state IS 'Pool-specific state: READY, LEASED, PROVISIONING, CLEANING, EXPIRED';
COMMENT ON COLUMN clusters.leased_by IS 'Identity of lease holder (user, service account, job ID)';
COMMENT ON COLUMN clusters.leased_at IS 'Timestamp when cluster was leased';
COMMENT ON COLUMN clusters.lease_expires_at IS 'Automatic release timestamp';
COMMENT ON COLUMN clusters.lease_metadata IS 'JSON metadata about the lease (job_id, build_url, etc.)';
COMMENT ON COLUMN clusters.pool_generation IS 'Incremented each time cluster is cleaned and returned to pool';
COMMENT ON COLUMN clusters.last_cleaned_at IS 'Last successful POOL_CLEAN job completion timestamp';
