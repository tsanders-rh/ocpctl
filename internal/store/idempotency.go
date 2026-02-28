package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// IdempotencyStore handles idempotency key operations
type IdempotencyStore struct {
	pool *pgxpool.Pool
}

// Store saves an idempotency key with response
func (s *IdempotencyStore) Store(ctx context.Context, key types.IdempotencyKey) error {
	query := `
		INSERT INTO idempotency_keys (
			id, key, request_hash, response_status_code, response_body, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
		ON CONFLICT (key) DO UPDATE
		SET response_status_code = EXCLUDED.response_status_code,
			response_body = EXCLUDED.response_body
	`

	_, err := s.pool.Exec(ctx, query,
		key.ID,
		key.Key,
		key.RequestHash,
		key.ResponseStatusCode,
		key.ResponseBody,
		key.ExpiresAt,
	)

	if err != nil {
		return fmt.Errorf("store idempotency key: %w", err)
	}

	return nil
}

// Get retrieves an idempotency key if it exists and hasn't expired
func (s *IdempotencyStore) Get(ctx context.Context, key string) (*types.IdempotencyKey, error) {
	query := `
		SELECT id, key, request_hash, response_status_code, response_body,
			created_at, expires_at
		FROM idempotency_keys
		WHERE key = $1 AND expires_at > NOW()
	`

	var ikey types.IdempotencyKey
	err := s.pool.QueryRow(ctx, query, key).Scan(
		&ikey.ID,
		&ikey.Key,
		&ikey.RequestHash,
		&ikey.ResponseStatusCode,
		&ikey.ResponseBody,
		&ikey.CreatedAt,
		&ikey.ExpiresAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency key: %w", err)
	}

	return &ikey, nil
}

// CleanupExpired removes expired idempotency keys
func (s *IdempotencyStore) CleanupExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM idempotency_keys WHERE expires_at < NOW()`

	result, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired idempotency keys: %w", err)
	}

	return result.RowsAffected(), nil
}
