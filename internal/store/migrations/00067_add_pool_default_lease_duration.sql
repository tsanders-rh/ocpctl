-- +goose Up
ALTER TABLE cluster_pools
ADD COLUMN default_lease_duration_hours INTEGER DEFAULT 24 NOT NULL;

COMMENT ON COLUMN cluster_pools.default_lease_duration_hours IS 'Default lease duration in hours for this pool';

-- Update existing pools to 24 hours
UPDATE cluster_pools SET default_lease_duration_hours = 24;

-- +goose Down
ALTER TABLE cluster_pools
DROP COLUMN default_lease_duration_hours;
