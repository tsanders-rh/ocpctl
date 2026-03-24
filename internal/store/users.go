package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// UserStore handles user database operations
type UserStore struct {
	pool *pgxpool.Pool
}

// Create creates a new user
func (s *UserStore) Create(ctx context.Context, user *types.User) error {
	query := `
		INSERT INTO users (id, email, username, password_hash, role, timezone, work_hours_enabled, work_hours_start, work_hours_end, work_days, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := s.pool.Exec(ctx, query,
		user.ID,
		user.Email,
		user.Username,
		user.PasswordHash,
		user.Role,
		user.Timezone,
		user.WorkHoursEnabled,
		user.WorkHoursStart,
		user.WorkHoursEnd,
		user.WorkDays,
		user.Active,
		user.CreatedAt,
		user.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	return nil
}

// GetByID retrieves a user by ID
func (s *UserStore) GetByID(ctx context.Context, id string) (*types.User, error) {
	query := `
		SELECT id, email, username, password_hash, role, timezone, work_hours_enabled, work_hours_start, work_hours_end, work_days, active, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user types.User
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.Username,
		&user.PasswordHash,
		&user.Role,
		&user.Timezone,
		&user.WorkHoursEnabled,
		&user.WorkHoursStart,
		&user.WorkHoursEnd,
		&user.WorkDays,
		&user.Active,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return &user, nil
}

// GetByEmail retrieves a user by email
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*types.User, error) {
	query := `
		SELECT id, email, username, password_hash, role, timezone, work_hours_enabled, work_hours_start, work_hours_end, work_days, active, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var user types.User
	err := s.pool.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Username,
		&user.PasswordHash,
		&user.Role,
		&user.Timezone,
		&user.WorkHoursEnabled,
		&user.WorkHoursStart,
		&user.WorkHoursEnd,
		&user.WorkDays,
		&user.Active,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	return &user, nil
}

// Update updates a user
func (s *UserStore) Update(ctx context.Context, user *types.User) error {
	user.UpdatedAt = time.Now()

	query := `
		UPDATE users
		SET email = $2, username = $3, password_hash = $4, role = $5, timezone = $6, work_hours_enabled = $7, work_hours_start = $8, work_hours_end = $9, work_days = $10, active = $11, updated_at = $12
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query,
		user.ID,
		user.Email,
		user.Username,
		user.PasswordHash,
		user.Role,
		user.Timezone,
		user.WorkHoursEnabled,
		user.WorkHoursStart,
		user.WorkHoursEnd,
		user.WorkDays,
		user.Active,
		user.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdatePartial updates specific fields of a user
func (s *UserStore) UpdatePartial(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	// Always update updated_at
	updates["updated_at"] = time.Now()

	// Build dynamic query
	query := "UPDATE users SET "
	args := []interface{}{}
	argCount := 1

	for field, value := range updates {
		if argCount > 1 {
			query += ", "
		}
		query += fmt.Sprintf("%s = $%d", field, argCount)
		args = append(args, value)
		argCount++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argCount)
	args = append(args, id)

	result, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update user partial: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Delete deletes a user
func (s *UserStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = $1`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// List retrieves all users
// DEPRECATED: Use ListPaginated for better performance with large user counts
func (s *UserStore) List(ctx context.Context) ([]*types.User, error) {
	query := `
		SELECT id, email, username, password_hash, role, timezone, work_hours_enabled, work_hours_start, work_hours_end, work_days, active, created_at, updated_at
		FROM users
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := []*types.User{}
	for rows.Next() {
		var user types.User
		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.Username,
			&user.PasswordHash,
			&user.Role,
			&user.Timezone,
			&user.WorkHoursEnabled,
			&user.WorkHoursStart,
			&user.WorkHoursEnd,
			&user.WorkDays,
			&user.Active,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, &user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

// ListPaginated retrieves users with pagination support
// Returns: (users, totalCount, error)
// - limit: maximum number of users to return (required, should be > 0)
// - offset: number of users to skip (optional, defaults to 0)
// - totalCount: total number of users in the database (for pagination UI)
func (s *UserStore) ListPaginated(ctx context.Context, limit, offset int) ([]*types.User, int, error) {
	// Validate pagination parameters
	if limit <= 0 {
		return nil, 0, fmt.Errorf("limit must be greater than 0, got: %d", limit)
	}
	if offset < 0 {
		return nil, 0, fmt.Errorf("offset must be non-negative, got: %d", offset)
	}

	// Get total count for pagination UI
	countQuery := `SELECT COUNT(*) FROM users`
	var totalCount int
	if err := s.pool.QueryRow(ctx, countQuery).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	// Fetch paginated results
	query := `
		SELECT id, email, username, password_hash, role, timezone, work_hours_enabled, work_hours_start, work_hours_end, work_days, active, created_at, updated_at
		FROM users
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users paginated: %w", err)
	}
	defer rows.Close()

	users := []*types.User{}
	for rows.Next() {
		var user types.User
		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.Username,
			&user.PasswordHash,
			&user.Role,
			&user.Timezone,
			&user.WorkHoursEnabled,
			&user.WorkHoursStart,
			&user.WorkHoursEnd,
			&user.WorkDays,
			&user.Active,
			&user.CreatedAt,
			&user.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, &user)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users: %w", err)
	}

	return users, totalCount, nil
}

// EmailExists checks if an email is already in use
func (s *UserStore) EmailExists(ctx context.Context, email string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`

	var exists bool
	err := s.pool.QueryRow(ctx, query, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check email exists: %w", err)
	}

	return exists, nil
}
