package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TeamMembershipStore handles user team membership operations
type TeamMembershipStore struct {
	db *pgxpool.Pool
}

// NewTeamMembershipStore creates a new team membership store
func NewTeamMembershipStore(db *pgxpool.Pool) *TeamMembershipStore {
	return &TeamMembershipStore{db: db}
}

// GetUserTeams returns all teams a user belongs to
func (s *TeamMembershipStore) GetUserTeams(ctx context.Context, userID string) ([]string, error) {
	query := `
		SELECT team
		FROM user_team_memberships
		WHERE user_id = $1
		ORDER BY team
	`

	rows, err := s.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user teams: %w", err)
	}
	defer rows.Close()

	teams := []string{}
	for rows.Next() {
		var team string
		if err := rows.Scan(&team); err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}
		teams = append(teams, team)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating teams: %w", err)
	}

	return teams, nil
}

// GetTeamMembers returns all users who are members of a given team
func (s *TeamMembershipStore) GetTeamMembers(ctx context.Context, team string) ([]*types.UserTeamMembership, error) {
	query := `
		SELECT
			tm.id,
			tm.user_id,
			tm.team,
			tm.added_by,
			tm.added_at,
			tm.notes
		FROM user_team_memberships tm
		WHERE tm.team = $1
		ORDER BY tm.added_at DESC
	`

	rows, err := s.db.Query(ctx, query, team)
	if err != nil {
		return nil, fmt.Errorf("failed to query team members: %w", err)
	}
	defer rows.Close()

	members := []*types.UserTeamMembership{}
	for rows.Next() {
		member := &types.UserTeamMembership{}
		err := rows.Scan(
			&member.ID,
			&member.UserID,
			&member.Team,
			&member.AddedBy,
			&member.AddedAt,
			&member.Notes,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team member: %w", err)
		}
		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating team members: %w", err)
	}

	return members, nil
}

// AddUserToTeam adds a user to a team
func (s *TeamMembershipStore) AddUserToTeam(ctx context.Context, userID, team, addedBy string, notes *string) error {
	query := `
		INSERT INTO user_team_memberships (user_id, team, added_by, added_at, notes)
		VALUES ($1, $2, $3, NOW(), $4)
		ON CONFLICT (user_id, team) DO NOTHING
	`

	_, err := s.db.Exec(ctx, query, userID, team, addedBy, notes)
	if err != nil {
		return fmt.Errorf("failed to add user to team: %w", err)
	}

	return nil
}

// RemoveUserFromTeam removes a user from a team
func (s *TeamMembershipStore) RemoveUserFromTeam(ctx context.Context, userID, team string) error {
	query := `
		DELETE FROM user_team_memberships
		WHERE user_id = $1 AND team = $2
	`

	result, err := s.db.Exec(ctx, query, userID, team)
	if err != nil {
		return fmt.Errorf("failed to remove user from team: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// RemoveAllForUser removes all team memberships for a user
func (s *TeamMembershipStore) RemoveAllForUser(ctx context.Context, userID string) error {
	query := `
		DELETE FROM user_team_memberships
		WHERE user_id = $1
	`

	_, err := s.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to remove all team memberships for user: %w", err)
	}

	return nil
}

// UpdateUserTeams updates a user's team memberships to match the provided list
// This replaces all existing memberships with the new ones
func (s *TeamMembershipStore) UpdateUserTeams(ctx context.Context, userID, updatedBy string, teams []string) error {
	// Start a transaction
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Remove all existing memberships
	_, err = tx.Exec(ctx, `DELETE FROM user_team_memberships WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("failed to remove existing teams: %w", err)
	}

	// Add new memberships
	for _, team := range teams {
		_, err = tx.Exec(ctx, `
			INSERT INTO user_team_memberships (user_id, team, added_by, added_at)
			VALUES ($1, $2, $3, NOW())
		`, userID, team, updatedBy)
		if err != nil {
			return fmt.Errorf("failed to add team %s: %w", team, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
