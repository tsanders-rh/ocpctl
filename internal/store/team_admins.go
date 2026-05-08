package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TeamAdminStore handles team admin privilege operations
type TeamAdminStore struct {
	db *pgxpool.Pool
}

// NewTeamAdminStore creates a new team admin store
func NewTeamAdminStore(db *pgxpool.Pool) *TeamAdminStore {
	return &TeamAdminStore{db: db}
}

// GetManagedTeams returns list of teams a user can administer
func (s *TeamAdminStore) GetManagedTeams(ctx context.Context, userID string) ([]string, error) {
	query := `
		SELECT team
		FROM user_team_admin_mappings
		WHERE user_id = $1
		ORDER BY team
	`

	rows, err := s.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query managed teams: %w", err)
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

// GrantTeamAdmin adds team admin privilege for a user
func (s *TeamAdminStore) GrantTeamAdmin(ctx context.Context, userID, team, grantedBy string, notes *string) error {
	// First, verify user has TEAM_ADMIN role
	var userRole string
	err := s.db.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, userID).Scan(&userRole)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return fmt.Errorf("failed to check user role: %w", err)
	}

	if userRole != string(types.RoleTeamAdmin) && userRole != string(types.RoleAdmin) {
		return fmt.Errorf("user must have TEAM_ADMIN or ADMIN role to be granted team admin privileges")
	}

	// Insert team admin mapping
	query := `
		INSERT INTO user_team_admin_mappings (user_id, team, granted_by, granted_at, notes)
		VALUES ($1, $2, $3, NOW(), $4)
		ON CONFLICT (user_id, team) DO NOTHING
	`

	_, err = s.db.Exec(ctx, query, userID, team, grantedBy, notes)
	if err != nil {
		return fmt.Errorf("failed to grant team admin privilege: %w", err)
	}

	return nil
}

// RevokeTeamAdmin removes team admin privilege
func (s *TeamAdminStore) RevokeTeamAdmin(ctx context.Context, userID, team string) error {
	query := `
		DELETE FROM user_team_admin_mappings
		WHERE user_id = $1 AND team = $2
	`

	result, err := s.db.Exec(ctx, query, userID, team)
	if err != nil {
		return fmt.Errorf("failed to revoke team admin privilege: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// ListTeamAdmins returns all users who can administer a given team
func (s *TeamAdminStore) ListTeamAdmins(ctx context.Context, team string) ([]*types.TeamAdminResponse, error) {
	query := `
		SELECT
			tam.id,
			tam.user_id,
			tam.team,
			tam.granted_by,
			tam.granted_at,
			tam.notes,
			u.username,
			u.email
		FROM user_team_admin_mappings tam
		JOIN users u ON tam.user_id = u.id
		WHERE tam.team = $1
		ORDER BY tam.granted_at DESC
	`

	rows, err := s.db.Query(ctx, query, team)
	if err != nil {
		return nil, fmt.Errorf("failed to query team admins: %w", err)
	}
	defer rows.Close()

	admins := []*types.TeamAdminResponse{}
	for rows.Next() {
		admin := &types.TeamAdminResponse{}
		err := rows.Scan(
			&admin.ID,
			&admin.UserID,
			&admin.Team,
			&admin.GrantedBy,
			&admin.GrantedAt,
			&admin.Notes,
			&admin.Username,
			&admin.Email,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team admin: %w", err)
		}
		admins = append(admins, admin)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating team admins: %w", err)
	}

	return admins, nil
}

// IsTeamAdmin checks if user is admin for specific team
func (s *TeamAdminStore) IsTeamAdmin(ctx context.Context, userID, team string) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM user_team_admin_mappings
			WHERE user_id = $1 AND team = $2
		)
	`

	err := s.db.QueryRow(ctx, query, userID, team).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check team admin status: %w", err)
	}

	return exists, nil
}

// RevokeAllForUser removes all team admin privileges for a user
// Used when changing user role from TEAM_ADMIN to another role
func (s *TeamAdminStore) RevokeAllForUser(ctx context.Context, userID string) error {
	query := `
		DELETE FROM user_team_admin_mappings
		WHERE user_id = $1
	`

	_, err := s.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to revoke all team admin privileges: %w", err)
	}

	return nil
}
