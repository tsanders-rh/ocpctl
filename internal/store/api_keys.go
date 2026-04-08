package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// APIKeyStore handles API key database operations
type APIKeyStore struct {
	pool *pgxpool.Pool
}

// Create creates a new API key
func (s *APIKeyStore) Create(ctx context.Context, apiKey *types.APIKey) error {
	query := `
		INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, scope, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := s.pool.Exec(ctx, query,
		apiKey.ID,
		apiKey.UserID,
		apiKey.Name,
		apiKey.KeyPrefix,
		apiKey.KeyHash,
		apiKey.Scope,
		apiKey.ExpiresAt,
		apiKey.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}

	return nil
}

// GetByID retrieves an API key by ID
func (s *APIKeyStore) GetByID(ctx context.Context, id string) (*types.APIKey, error) {
	query := `
		SELECT id, user_id, name, key_prefix, key_hash, scope, last_used_at, expires_at, created_at, revoked_at
		FROM api_keys
		WHERE id = $1
	`

	var apiKey types.APIKey
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&apiKey.ID,
		&apiKey.UserID,
		&apiKey.Name,
		&apiKey.KeyPrefix,
		&apiKey.KeyHash,
		&apiKey.Scope,
		&apiKey.LastUsedAt,
		&apiKey.ExpiresAt,
		&apiKey.CreatedAt,
		&apiKey.RevokedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}

	return &apiKey, nil
}

// GetByKeyHash retrieves an active API key by its hash
func (s *APIKeyStore) GetByKeyHash(ctx context.Context, keyHash string) (*types.APIKey, error) {
	query := `
		SELECT id, user_id, name, key_prefix, key_hash, scope, last_used_at, expires_at, created_at, revoked_at
		FROM api_keys
		WHERE key_hash = $1 AND revoked_at IS NULL
	`

	var apiKey types.APIKey
	err := s.pool.QueryRow(ctx, query, keyHash).Scan(
		&apiKey.ID,
		&apiKey.UserID,
		&apiKey.Name,
		&apiKey.KeyPrefix,
		&apiKey.KeyHash,
		&apiKey.Scope,
		&apiKey.LastUsedAt,
		&apiKey.ExpiresAt,
		&apiKey.CreatedAt,
		&apiKey.RevokedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}

	// Check if expired
	if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("api key expired")
	}

	return &apiKey, nil
}

// ListByUserID retrieves all API keys for a user
func (s *APIKeyStore) ListByUserID(ctx context.Context, userID string) ([]*types.APIKey, error) {
	query := `
		SELECT id, user_id, name, key_prefix, key_hash, scope, last_used_at, expires_at, created_at, revoked_at
		FROM api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	apiKeys := []*types.APIKey{}
	for rows.Next() {
		var apiKey types.APIKey
		err := rows.Scan(
			&apiKey.ID,
			&apiKey.UserID,
			&apiKey.Name,
			&apiKey.KeyPrefix,
			&apiKey.KeyHash,
			&apiKey.Scope,
			&apiKey.LastUsedAt,
			&apiKey.ExpiresAt,
			&apiKey.CreatedAt,
			&apiKey.RevokedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		apiKeys = append(apiKeys, &apiKey)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}

	return apiKeys, nil
}

// UpdateLastUsed updates the last_used_at timestamp for an API key
func (s *APIKeyStore) UpdateLastUsed(ctx context.Context, id string) error {
	query := `
		UPDATE api_keys
		SET last_used_at = NOW()
		WHERE id = $1
	`

	_, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("update last used: %w", err)
	}

	return nil
}

// UpdateName updates the name of an API key
func (s *APIKeyStore) UpdateName(ctx context.Context, id string, name string) error {
	query := `
		UPDATE api_keys
		SET name = $1
		WHERE id = $2 AND revoked_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, name, id)
	if err != nil {
		return fmt.Errorf("update api key name: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Revoke revokes an API key
func (s *APIKeyStore) Revoke(ctx context.Context, id string) error {
	query := `
		UPDATE api_keys
		SET revoked_at = NOW()
		WHERE id = $1 AND revoked_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// RevokeAllForUser revokes all API keys for a user
func (s *APIKeyStore) RevokeAllForUser(ctx context.Context, userID string) error {
	query := `
		UPDATE api_keys
		SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`

	_, err := s.pool.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("revoke all api keys for user: %w", err)
	}

	return nil
}

// Delete permanently deletes an API key
func (s *APIKeyStore) Delete(ctx context.Context, id string) error {
	query := `
		DELETE FROM api_keys
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// CleanupExpired removes expired API keys
func (s *APIKeyStore) CleanupExpired(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM api_keys
		WHERE expires_at IS NOT NULL AND expires_at < NOW()
	`

	result, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired api keys: %w", err)
	}

	return result.RowsAffected(), nil
}
