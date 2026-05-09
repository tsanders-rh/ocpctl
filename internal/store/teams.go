package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TeamStore handles team operations
type TeamStore struct {
	db *pgxpool.Pool
}

// NewTeamStore creates a new team store
func NewTeamStore(db *pgxpool.Pool) *TeamStore {
	return &TeamStore{db: db}
}

// List returns all teams
func (s *TeamStore) List(ctx context.Context) ([]*types.Team, error) {
	query := `
		SELECT id, name, description, created_at, updated_at, created_by
		FROM teams
		ORDER BY name
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query teams: %w", err)
	}
	defer rows.Close()

	teams := []*types.Team{}
	for rows.Next() {
		team := &types.Team{}
		err := rows.Scan(
			&team.ID,
			&team.Name,
			&team.Description,
			&team.CreatedAt,
			&team.UpdatedAt,
			&team.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}
		teams = append(teams, team)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating teams: %w", err)
	}

	return teams, nil
}

// Create creates a new team
func (s *TeamStore) Create(ctx context.Context, team *types.Team) error {
	query := `
		INSERT INTO teams (name, description, created_at, updated_at, created_by)
		VALUES ($1, $2, NOW(), NOW(), $3)
		RETURNING id, created_at, updated_at
	`

	err := s.db.QueryRow(
		ctx,
		query,
		team.Name,
		team.Description,
		team.CreatedBy,
	).Scan(&team.ID, &team.CreatedAt, &team.UpdatedAt)

	if err != nil {
		// Check for unique constraint violation (PostgreSQL error code 23505)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("team with name '%s' already exists", team.Name)
		}
		return fmt.Errorf("failed to create team: %w", err)
	}

	return nil
}

// Get retrieves a team by name
func (s *TeamStore) Get(ctx context.Context, name string) (*types.Team, error) {
	team := &types.Team{}

	query := `
		SELECT id, name, description, created_at, updated_at, created_by
		FROM teams
		WHERE name = $1
	`

	err := s.db.QueryRow(ctx, query, name).Scan(
		&team.ID,
		&team.Name,
		&team.Description,
		&team.CreatedAt,
		&team.UpdatedAt,
		&team.CreatedBy,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get team: %w", err)
	}

	return team, nil
}

// Update updates team metadata
func (s *TeamStore) Update(ctx context.Context, name string, updates map[string]interface{}) error {
	// Build UPDATE query dynamically
	if len(updates) == 0 {
		return fmt.Errorf("no fields to update")
	}

	// Only allow updating description and allowed_profiles
	allowedFields := map[string]bool{
		"description":      true,
		"allowed_profiles": true,
	}

	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	for field, value := range updates {
		if !allowedFields[field] {
			return fmt.Errorf("field '%s' cannot be updated", field)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", field, argIdx))
		args = append(args, value)
		argIdx++
	}

	// Add updated_at
	setClauses = append(setClauses, fmt.Sprintf("updated_at = NOW()"))

	// Add WHERE clause
	args = append(args, name)

	query := fmt.Sprintf(
		"UPDATE teams SET %s WHERE name = $%d",
		joinStrings(setClauses, ", "),
		argIdx,
	)

	result, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update team: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Delete removes a team (only if no clusters reference it)
func (s *TeamStore) Delete(ctx context.Context, name string) error {
	// First check if any clusters reference this team
	var count int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM clusters WHERE team = $1`, name).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check cluster references: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("cannot delete team '%s': %d cluster(s) still reference it", name, count)
	}

	// Delete team admin mappings first
	_, err = s.db.Exec(ctx, `DELETE FROM user_team_admin_mappings WHERE team = $1`, name)
	if err != nil {
		return fmt.Errorf("failed to delete team admin mappings: %w", err)
	}

	// Delete team
	result, err := s.db.Exec(ctx, `DELETE FROM teams WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("failed to delete team: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetTeamsWithClusterCounts returns teams with count of clusters in each team
func (s *TeamStore) GetTeamsWithClusterCounts(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT team, COUNT(*) as cluster_count
		FROM clusters
		WHERE team IS NOT NULL AND team != ''
		GROUP BY team
		ORDER BY team
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query team cluster counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var team string
		var count int
		if err := rows.Scan(&team, &count); err != nil {
			return nil, fmt.Errorf("failed to scan team cluster count: %w", err)
		}
		counts[team] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating team cluster counts: %w", err)
	}

	return counts, nil
}

// joinStrings is a helper to join string slices
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
