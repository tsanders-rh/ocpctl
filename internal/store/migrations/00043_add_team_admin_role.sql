-- +goose Up
-- Migration: Add Team Admin RBAC role for team-scoped cluster management
-- Issue: #32
-- Description: Introduces TEAM_ADMIN role allowing users to manage all clusters
--              within assigned teams without platform-wide admin privileges.

-- 1. Update users table role constraint to include TEAM_ADMIN
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('ADMIN', 'USER', 'VIEWER', 'TEAM_ADMIN'));

-- 2. Create teams table for team registry and referential integrity
CREATE TABLE IF NOT EXISTS teams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX idx_teams_name ON teams(name);

COMMENT ON TABLE teams IS 'Team registry for organizational structure and access control';
COMMENT ON COLUMN teams.name IS 'Team identifier (matches cluster.team field)';
COMMENT ON COLUMN teams.description IS 'Team description or purpose';
COMMENT ON COLUMN teams.created_by IS 'Platform admin who created this team';

-- 3. Create user_team_admin_mappings table for team admin privileges
CREATE TABLE IF NOT EXISTS user_team_admin_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team VARCHAR(255) NOT NULL,
    granted_by UUID REFERENCES users(id),
    granted_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    notes TEXT,
    UNIQUE(user_id, team)
);

CREATE INDEX idx_user_team_admin_user ON user_team_admin_mappings(user_id);
CREATE INDEX idx_user_team_admin_team ON user_team_admin_mappings(team);

COMMENT ON TABLE user_team_admin_mappings IS 'Grants team admin privileges to users for specific teams';
COMMENT ON COLUMN user_team_admin_mappings.user_id IS 'User being granted team admin privileges (must have role=TEAM_ADMIN)';
COMMENT ON COLUMN user_team_admin_mappings.team IS 'Team name that user can administer';
COMMENT ON COLUMN user_team_admin_mappings.granted_by IS 'Platform admin who granted this privilege';
COMMENT ON COLUMN user_team_admin_mappings.granted_at IS 'When the privilege was granted';
COMMENT ON COLUMN user_team_admin_mappings.notes IS 'Optional notes about why privilege was granted';

-- 4. Insert commonly used teams from existing clusters
INSERT INTO teams (name, description, created_at)
SELECT DISTINCT
    team,
    'Auto-created from existing clusters',
    NOW()
FROM clusters
WHERE team IS NOT NULL
  AND team != ''
  AND NOT EXISTS (SELECT 1 FROM teams WHERE teams.name = clusters.team)
ORDER BY team;

-- +goose Down
-- Rollback: Remove Team Admin RBAC functionality

-- 1. Drop user_team_admin_mappings table
DROP TABLE IF EXISTS user_team_admin_mappings;

-- 2. Drop teams table
DROP TABLE IF EXISTS teams;

-- 3. Revert users role constraint to original values
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('ADMIN', 'USER', 'VIEWER'));

-- 4. Update any existing TEAM_ADMIN users to USER role
UPDATE users SET role = 'USER' WHERE role = 'TEAM_ADMIN';
