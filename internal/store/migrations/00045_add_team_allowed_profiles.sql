-- +goose Up
-- Add allowed_profiles column to teams table to enable team admins to restrict which profiles their team members can use
-- NULL or empty array = no restrictions (all profiles allowed)
-- Non-empty array = only listed profiles allowed for team members

ALTER TABLE teams ADD COLUMN allowed_profiles TEXT[] DEFAULT NULL;

-- Add comment explaining the column
COMMENT ON COLUMN teams.allowed_profiles IS 'List of profile names allowed for this team. NULL = all profiles allowed. Empty array or specific list = only those profiles allowed.';

-- Add index for faster lookups when filtering profiles
CREATE INDEX idx_teams_allowed_profiles ON teams USING GIN (allowed_profiles);

-- +goose Down
DROP INDEX IF EXISTS idx_teams_allowed_profiles;
ALTER TABLE teams DROP COLUMN IF EXISTS allowed_profiles;
