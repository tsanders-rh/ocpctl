package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// JobLockStore handles job lock database operations
type JobLockStore struct {
	pool *pgxpool.Pool
}

// Acquire attempts to acquire a lock for a cluster within a transaction
// Returns true if lock was acquired, false if already held by another worker
func (s *JobLockStore) Acquire(ctx context.Context, tx pgx.Tx, clusterID, jobID, workerID string, ttl time.Duration) (bool, error) {
	query := `
		INSERT INTO job_locks (cluster_id, job_id, locked_by, expires_at)
		VALUES ($1, $2, $3, NOW() + $4::interval)
		ON CONFLICT (cluster_id) DO NOTHING
		RETURNING cluster_id
	`

	var returnedClusterID string
	err := tx.QueryRow(ctx, query, clusterID, jobID, workerID, ttl).Scan(&returnedClusterID)

	if err == pgx.ErrNoRows {
		// Lock already held by another worker
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("acquire lock: %w", err)
	}

	return true, nil
}

// Release removes a lock for a cluster
func (s *JobLockStore) Release(ctx context.Context, clusterID, jobID string) error {
	query := `
		DELETE FROM job_locks
		WHERE cluster_id = $1 AND job_id = $2
	`

	_, err := s.pool.Exec(ctx, query, clusterID, jobID)
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}

	return nil
}

// UpdateExpiry extends the lock expiration time (heartbeat)
func (s *JobLockStore) UpdateExpiry(ctx context.Context, clusterID, jobID string, ttl time.Duration) error {
	query := `
		UPDATE job_locks
		SET expires_at = NOW() + $1::interval
		WHERE cluster_id = $2 AND job_id = $3
	`

	result, err := s.pool.Exec(ctx, query, ttl, clusterID, jobID)
	if err != nil {
		return fmt.Errorf("update lock expiry: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetLock retrieves lock information for a cluster
func (s *JobLockStore) GetLock(ctx context.Context, clusterID string) (*types.JobLock, error) {
	query := `
		SELECT cluster_id, job_id, locked_at, locked_by, expires_at
		FROM job_locks
		WHERE cluster_id = $1
	`

	var lock types.JobLock
	err := s.pool.QueryRow(ctx, query, clusterID).Scan(
		&lock.ClusterID,
		&lock.JobID,
		&lock.LockedAt,
		&lock.LockedBy,
		&lock.ExpiresAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get lock: %w", err)
	}

	return &lock, nil
}

// GetExpiredLocks returns locks that have expired
func (s *JobLockStore) GetExpiredLocks(ctx context.Context) ([]*types.JobLock, error) {
	query := `
		SELECT cluster_id, job_id, locked_at, locked_by, expires_at
		FROM job_locks
		WHERE expires_at < NOW()
		ORDER BY expires_at ASC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query expired locks: %w", err)
	}
	defer rows.Close()

	locks := []*types.JobLock{}
	for rows.Next() {
		var lock types.JobLock
		err := rows.Scan(
			&lock.ClusterID,
			&lock.JobID,
			&lock.LockedAt,
			&lock.LockedBy,
			&lock.ExpiresAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan expired lock: %w", err)
		}
		locks = append(locks, &lock)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired locks: %w", err)
	}

	return locks, nil
}

// CleanupExpiredLocks removes expired locks and returns them for logging
func (s *JobLockStore) CleanupExpiredLocks(ctx context.Context) ([]*types.JobLock, error) {
	query := `
		DELETE FROM job_locks
		WHERE expires_at < NOW()
		RETURNING cluster_id, job_id, locked_at, locked_by, expires_at
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("cleanup expired locks: %w", err)
	}
	defer rows.Close()

	locks := []*types.JobLock{}
	for rows.Next() {
		var lock types.JobLock
		err := rows.Scan(
			&lock.ClusterID,
			&lock.JobID,
			&lock.LockedAt,
			&lock.LockedBy,
			&lock.ExpiresAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan cleaned lock: %w", err)
		}
		locks = append(locks, &lock)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cleaned locks: %w", err)
	}

	return locks, nil
}

// IsLocked checks if a cluster is currently locked
func (s *JobLockStore) IsLocked(ctx context.Context, clusterID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM job_locks
			WHERE cluster_id = $1 AND expires_at > NOW()
		)
	`

	var locked bool
	err := s.pool.QueryRow(ctx, query, clusterID).Scan(&locked)
	if err != nil {
		return false, fmt.Errorf("check if locked: %w", err)
	}

	return locked, nil
}
