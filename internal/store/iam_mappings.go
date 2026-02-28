package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// IAMMappingStore handles IAM principal mapping database operations
type IAMMappingStore struct {
	pool  *pgxpool.Pool
	cache map[string]*types.IAMPrincipalMapping // Cache by ARN for performance
	mu    sync.RWMutex
}

// GetByPrincipalARN retrieves an IAM mapping by principal ARN
func (s *IAMMappingStore) GetByPrincipalARN(ctx context.Context, arn string) (*types.IAMPrincipalMapping, error) {
	// Check cache first
	s.mu.RLock()
	if mapping, ok := s.cache[arn]; ok && mapping.Enabled {
		s.mu.RUnlock()
		return mapping, nil
	}
	s.mu.RUnlock()

	// Query database
	query := `
		SELECT id, iam_principal_arn, user_id, enabled, created_at, updated_at
		FROM iam_principal_mappings
		WHERE iam_principal_arn = $1 AND enabled = true
	`

	var mapping types.IAMPrincipalMapping
	err := s.pool.QueryRow(ctx, query, arn).Scan(
		&mapping.ID,
		&mapping.IAMPrincipalARN,
		&mapping.UserID,
		&mapping.Enabled,
		&mapping.CreatedAt,
		&mapping.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("IAM principal mapping not found for ARN: %s", arn)
		}
		return nil, fmt.Errorf("failed to get IAM mapping: %w", err)
	}

	// Update cache
	s.mu.Lock()
	s.cache[arn] = &mapping
	s.mu.Unlock()

	return &mapping, nil
}

// Create creates a new IAM principal mapping
func (s *IAMMappingStore) Create(ctx context.Context, req *types.CreateIAMMapping) (*types.IAMPrincipalMapping, error) {
	query := `
		INSERT INTO iam_principal_mappings (iam_principal_arn, user_id, enabled)
		VALUES ($1, $2, $3)
		RETURNING id, iam_principal_arn, user_id, enabled, created_at, updated_at
	`

	enabled := req.Enabled
	if !enabled {
		enabled = true // Default to enabled
	}

	var mapping types.IAMPrincipalMapping
	err := s.pool.QueryRow(ctx, query, req.IAMPrincipalARN, req.UserID, enabled).Scan(
		&mapping.ID,
		&mapping.IAMPrincipalARN,
		&mapping.UserID,
		&mapping.Enabled,
		&mapping.CreatedAt,
		&mapping.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create IAM mapping: %w", err)
	}

	// Update cache
	s.mu.Lock()
	s.cache[mapping.IAMPrincipalARN] = &mapping
	s.mu.Unlock()

	return &mapping, nil
}

// Update updates an existing IAM principal mapping
func (s *IAMMappingStore) Update(ctx context.Context, arn string, req *types.UpdateIAMMapping) (*types.IAMPrincipalMapping, error) {
	query := `
		UPDATE iam_principal_mappings
		SET enabled = COALESCE($2, enabled)
		WHERE iam_principal_arn = $1
		RETURNING id, iam_principal_arn, user_id, enabled, created_at, updated_at
	`

	var mapping types.IAMPrincipalMapping
	err := s.pool.QueryRow(ctx, query, arn, req.Enabled).Scan(
		&mapping.ID,
		&mapping.IAMPrincipalARN,
		&mapping.UserID,
		&mapping.Enabled,
		&mapping.CreatedAt,
		&mapping.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("IAM principal mapping not found for ARN: %s", arn)
		}
		return nil, fmt.Errorf("failed to update IAM mapping: %w", err)
	}

	// Update cache
	s.mu.Lock()
	if mapping.Enabled {
		s.cache[arn] = &mapping
	} else {
		delete(s.cache, arn) // Remove from cache if disabled
	}
	s.mu.Unlock()

	return &mapping, nil
}

// Delete deletes an IAM principal mapping
func (s *IAMMappingStore) Delete(ctx context.Context, arn string) error {
	query := `DELETE FROM iam_principal_mappings WHERE iam_principal_arn = $1`

	result, err := s.pool.Exec(ctx, query, arn)
	if err != nil {
		return fmt.Errorf("failed to delete IAM mapping: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("IAM principal mapping not found for ARN: %s", arn)
	}

	// Remove from cache
	s.mu.Lock()
	delete(s.cache, arn)
	s.mu.Unlock()

	return nil
}

// List returns all IAM principal mappings (for admin)
func (s *IAMMappingStore) List(ctx context.Context) ([]*types.IAMPrincipalMapping, error) {
	query := `
		SELECT id, iam_principal_arn, user_id, enabled, created_at, updated_at
		FROM iam_principal_mappings
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list IAM mappings: %w", err)
	}
	defer rows.Close()

	var mappings []*types.IAMPrincipalMapping
	for rows.Next() {
		var mapping types.IAMPrincipalMapping
		if err := rows.Scan(
			&mapping.ID,
			&mapping.IAMPrincipalARN,
			&mapping.UserID,
			&mapping.Enabled,
			&mapping.CreatedAt,
			&mapping.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan IAM mapping: %w", err)
		}
		mappings = append(mappings, &mapping)
	}

	return mappings, rows.Err()
}

// RefreshCache reloads all enabled mappings into the cache
func (s *IAMMappingStore) RefreshCache(ctx context.Context) error {
	query := `
		SELECT id, iam_principal_arn, user_id, enabled, created_at, updated_at
		FROM iam_principal_mappings
		WHERE enabled = true
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to refresh cache: %w", err)
	}
	defer rows.Close()

	newCache := make(map[string]*types.IAMPrincipalMapping)
	for rows.Next() {
		var mapping types.IAMPrincipalMapping
		if err := rows.Scan(
			&mapping.ID,
			&mapping.IAMPrincipalARN,
			&mapping.UserID,
			&mapping.Enabled,
			&mapping.CreatedAt,
			&mapping.UpdatedAt,
		); err != nil {
			return fmt.Errorf("failed to scan IAM mapping: %w", err)
		}
		newCache[mapping.IAMPrincipalARN] = &mapping
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	// Atomically replace cache
	s.mu.Lock()
	s.cache = newCache
	s.mu.Unlock()

	return nil
}
