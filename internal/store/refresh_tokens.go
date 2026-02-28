package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// RefreshTokenStore handles refresh token database operations
type RefreshTokenStore struct {
	pool *pgxpool.Pool
}

// Create creates a new refresh token
func (s *RefreshTokenStore) Create(ctx context.Context, token *types.RefreshToken) error {
	query := `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := s.pool.Exec(ctx, query,
		token.ID,
		token.UserID,
		token.TokenHash,
		token.ExpiresAt,
		token.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}

	return nil
}

// GetByTokenHash retrieves a refresh token by its hash
func (s *RefreshTokenStore) GetByTokenHash(ctx context.Context, tokenHash string) (*types.RefreshToken, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, created_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL
	`

	var token types.RefreshToken
	err := s.pool.QueryRow(ctx, query, tokenHash).Scan(
		&token.ID,
		&token.UserID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.CreatedAt,
		&token.RevokedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh token: %w", err)
	}

	// Check if expired
	if token.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("refresh token expired")
	}

	return &token, nil
}

// Revoke revokes a refresh token
func (s *RefreshTokenStore) Revoke(ctx context.Context, tokenHash string) error {
	query := `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`

	result, err := s.pool.Exec(ctx, query, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// RevokeAllForUser revokes all refresh tokens for a user
func (s *RefreshTokenStore) RevokeAllForUser(ctx context.Context, userID string) error {
	query := `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`

	_, err := s.pool.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("revoke all tokens for user: %w", err)
	}

	return nil
}

// CleanupExpired removes expired refresh tokens
func (s *RefreshTokenStore) CleanupExpired(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM refresh_tokens
		WHERE expires_at < NOW()
	`

	result, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired refresh tokens: %w", err)
	}

	return result.RowsAffected(), nil
}

// ListByUserID retrieves all active refresh tokens for a user
func (s *RefreshTokenStore) ListByUserID(ctx context.Context, userID string) ([]*types.RefreshToken, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, created_at, revoked_at
		FROM refresh_tokens
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list refresh tokens: %w", err)
	}
	defer rows.Close()

	tokens := []*types.RefreshToken{}
	for rows.Next() {
		var token types.RefreshToken
		err := rows.Scan(
			&token.ID,
			&token.UserID,
			&token.TokenHash,
			&token.ExpiresAt,
			&token.CreatedAt,
			&token.RevokedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan refresh token: %w", err)
		}
		tokens = append(tokens, &token)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate refresh tokens: %w", err)
	}

	return tokens, nil
}
