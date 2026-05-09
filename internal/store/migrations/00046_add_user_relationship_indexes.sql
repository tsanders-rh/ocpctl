-- +goose Up
-- Add missing indexes on user relationship foreign keys for performance
-- These indexes are critical for avoiding full table scans on user team queries

-- Index for finding all teams a user belongs to
CREATE INDEX IF NOT EXISTS idx_user_team_memberships_user_id
    ON user_team_memberships(user_id);

-- Index for finding all users in a team
CREATE INDEX IF NOT EXISTS idx_user_team_memberships_team
    ON user_team_memberships(team);

-- Index for finding all teams a user administers
CREATE INDEX IF NOT EXISTS idx_user_team_admin_mappings_user_id
    ON user_team_admin_mappings(user_id);

-- Index for finding all admins of a team
CREATE INDEX IF NOT EXISTS idx_user_team_admin_mappings_team
    ON user_team_admin_mappings(team);

-- +goose Down
DROP INDEX IF EXISTS idx_user_team_admin_mappings_team;
DROP INDEX IF EXISTS idx_user_team_admin_mappings_user_id;
DROP INDEX IF EXISTS idx_user_team_memberships_team;
DROP INDEX IF EXISTS idx_user_team_memberships_user_id;
