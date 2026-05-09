-- +goose Up
-- +goose StatementBegin
-- Create user_team_memberships table for general team membership
-- (separate from user_team_admin_mappings which is for team admin privileges)
CREATE TABLE IF NOT EXISTS user_team_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team VARCHAR(255) NOT NULL REFERENCES teams(name) ON DELETE CASCADE,
    added_by UUID REFERENCES users(id) ON DELETE SET NULL,
    added_at TIMESTAMP NOT NULL DEFAULT NOW(),
    notes TEXT,
    UNIQUE(user_id, team)
);

CREATE INDEX idx_user_team_memberships_user_id ON user_team_memberships(user_id);
CREATE INDEX idx_user_team_memberships_team ON user_team_memberships(team);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_team_memberships;
-- +goose StatementEnd
