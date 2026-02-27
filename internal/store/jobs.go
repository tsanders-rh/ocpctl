package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// JobStore handles job database operations
type JobStore struct {
	pool *pgxpool.Pool
}

// Create inserts a new job record
func (s *JobStore) Create(ctx context.Context, job *types.Job) error {
	query := `
		INSERT INTO jobs (
			id, cluster_id, job_type, status, attempt, max_attempts, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`

	_, err := s.pool.Exec(ctx, query,
		job.ID,
		job.ClusterID,
		job.JobType,
		job.Status,
		job.Attempt,
		job.MaxAttempts,
		job.Metadata,
	)

	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}

	return nil
}

// GetByID retrieves a job by ID
func (s *JobStore) GetByID(ctx context.Context, id string) (*types.Job, error) {
	query := `
		SELECT id, cluster_id, job_type, status, attempt, max_attempts,
			error_code, error_message, started_at, ended_at,
			created_at, updated_at, metadata
		FROM jobs
		WHERE id = $1
	`

	var job types.Job
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&job.ID,
		&job.ClusterID,
		&job.JobType,
		&job.Status,
		&job.Attempt,
		&job.MaxAttempts,
		&job.ErrorCode,
		&job.ErrorMessage,
		&job.StartedAt,
		&job.EndedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.Metadata,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query job: %w", err)
	}

	return &job, nil
}

// ListByClusterID retrieves all jobs for a cluster
func (s *JobStore) ListByClusterID(ctx context.Context, clusterID string) ([]*types.Job, error) {
	query := `
		SELECT id, cluster_id, job_type, status, attempt, max_attempts,
			error_code, error_message, started_at, ended_at,
			created_at, updated_at, metadata
		FROM jobs
		WHERE cluster_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query jobs by cluster: %w", err)
	}
	defer rows.Close()

	jobs := []*types.Job{}
	for rows.Next() {
		var job types.Job
		err := rows.Scan(
			&job.ID,
			&job.ClusterID,
			&job.JobType,
			&job.Status,
			&job.Attempt,
			&job.MaxAttempts,
			&job.ErrorCode,
			&job.ErrorMessage,
			&job.StartedAt,
			&job.EndedAt,
			&job.CreatedAt,
			&job.UpdatedAt,
			&job.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}

	return jobs, nil
}

// UpdateStatus updates job status within a transaction
func (s *JobStore) UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status types.JobStatus) error {
	query := `
		UPDATE jobs
		SET status = $1, updated_at = NOW()
		WHERE id = $2
	`

	result, err := tx.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("update job status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkStarted marks a job as running and sets started_at
func (s *JobStore) MarkStarted(ctx context.Context, tx pgx.Tx, id string) error {
	query := `
		UPDATE jobs
		SET status = $1, started_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`

	result, err := tx.Exec(ctx, query, types.JobStatusRunning, id)
	if err != nil {
		return fmt.Errorf("mark job started: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkSucceeded marks a job as succeeded and sets ended_at
func (s *JobStore) MarkSucceeded(ctx context.Context, id string) error {
	query := `
		UPDATE jobs
		SET status = $1, ended_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`

	result, err := s.pool.Exec(ctx, query, types.JobStatusSucceeded, id)
	if err != nil {
		return fmt.Errorf("mark job succeeded: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkFailed marks a job as failed with error details
func (s *JobStore) MarkFailed(ctx context.Context, id string, errorCode, errorMessage string) error {
	query := `
		UPDATE jobs
		SET status = $1, error_code = $2, error_message = $3,
			ended_at = NOW(), updated_at = NOW()
		WHERE id = $4
	`

	result, err := s.pool.Exec(ctx, query, types.JobStatusFailed, errorCode, errorMessage, id)
	if err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// IncrementAttempt increments the job attempt counter for retries
func (s *JobStore) IncrementAttempt(ctx context.Context, id string) error {
	query := `
		UPDATE jobs
		SET attempt = attempt + 1, status = $1, updated_at = NOW()
		WHERE id = $2
	`

	result, err := s.pool.Exec(ctx, query, types.JobStatusRetrying, id)
	if err != nil {
		return fmt.Errorf("increment job attempt: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetStuckJobs returns jobs in RUNNING status for longer than the threshold
func (s *JobStore) GetStuckJobs(ctx context.Context, threshold time.Duration) ([]*types.Job, error) {
	query := `
		SELECT id, cluster_id, job_type, status, attempt, max_attempts,
			error_code, error_message, started_at, ended_at,
			created_at, updated_at, metadata
		FROM jobs
		WHERE status = 'RUNNING'
			AND started_at < NOW() - $1::interval
		ORDER BY started_at ASC
	`

	rows, err := s.pool.Query(ctx, query, threshold)
	if err != nil {
		return nil, fmt.Errorf("query stuck jobs: %w", err)
	}
	defer rows.Close()

	jobs := []*types.Job{}
	for rows.Next() {
		var job types.Job
		err := rows.Scan(
			&job.ID,
			&job.ClusterID,
			&job.JobType,
			&job.Status,
			&job.Attempt,
			&job.MaxAttempts,
			&job.ErrorCode,
			&job.ErrorMessage,
			&job.StartedAt,
			&job.EndedAt,
			&job.CreatedAt,
			&job.UpdatedAt,
			&job.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("scan stuck job: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stuck jobs: %w", err)
	}

	return jobs, nil
}
