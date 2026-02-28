package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// RBACStore handles RBAC mapping operations
type RBACStore struct {
	pool *pgxpool.Pool
}

// GetByPrincipal retrieves RBAC mappings for an IAM principal ARN
func (s *RBACStore) GetByPrincipal(ctx context.Context, principalARN string) ([]*types.RBACMapping, error) {
	query := `
		SELECT id, iam_principal_arn, iam_principal_type, team, role,
			enabled, created_at, updated_at, created_by, notes
		FROM rbac_mappings
		WHERE iam_principal_arn = $1 AND enabled = true
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, principalARN)
	if err != nil {
		return nil, fmt.Errorf("query RBAC mappings: %w", err)
	}
	defer rows.Close()

	mappings := []*types.RBACMapping{}
	for rows.Next() {
		var mapping types.RBACMapping
		err := rows.Scan(
			&mapping.ID,
			&mapping.IAMPrincipalARN,
			&mapping.IAMPrincipalType,
			&mapping.Team,
			&mapping.Role,
			&mapping.Enabled,
			&mapping.CreatedAt,
			&mapping.UpdatedAt,
			&mapping.CreatedBy,
			&mapping.Notes,
		)
		if err != nil {
			return nil, fmt.Errorf("scan RBAC mapping: %w", err)
		}
		mappings = append(mappings, &mapping)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate RBAC mappings: %w", err)
	}

	return mappings, nil
}

// Create inserts a new RBAC mapping
func (s *RBACStore) Create(ctx context.Context, mapping *types.RBACMapping) error {
	query := `
		INSERT INTO rbac_mappings (
			id, iam_principal_arn, iam_principal_type, team, role,
			enabled, created_by, notes
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err := s.pool.Exec(ctx, query,
		mapping.ID,
		mapping.IAMPrincipalARN,
		mapping.IAMPrincipalType,
		mapping.Team,
		mapping.Role,
		mapping.Enabled,
		mapping.CreatedBy,
		mapping.Notes,
	)

	if err != nil {
		return fmt.Errorf("insert RBAC mapping: %w", err)
	}

	return nil
}

// ListByTeam retrieves all RBAC mappings for a team
func (s *RBACStore) ListByTeam(ctx context.Context, team string) ([]*types.RBACMapping, error) {
	query := `
		SELECT id, iam_principal_arn, iam_principal_type, team, role,
			enabled, created_at, updated_at, created_by, notes
		FROM rbac_mappings
		WHERE team = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, team)
	if err != nil {
		return nil, fmt.Errorf("query RBAC mappings by team: %w", err)
	}
	defer rows.Close()

	mappings := []*types.RBACMapping{}
	for rows.Next() {
		var mapping types.RBACMapping
		err := rows.Scan(
			&mapping.ID,
			&mapping.IAMPrincipalARN,
			&mapping.IAMPrincipalType,
			&mapping.Team,
			&mapping.Role,
			&mapping.Enabled,
			&mapping.CreatedAt,
			&mapping.UpdatedAt,
			&mapping.CreatedBy,
			&mapping.Notes,
		)
		if err != nil {
			return nil, fmt.Errorf("scan RBAC mapping: %w", err)
		}
		mappings = append(mappings, &mapping)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate RBAC mappings: %w", err)
	}

	return mappings, nil
}
