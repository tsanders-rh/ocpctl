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

// Create inserts a new job record into the database.
// The job is initialized with PENDING status and attempt counter at 1.
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

// GetByID retrieves a job by its unique identifier.
// Returns ErrNotFound if no job exists with the given ID.
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

// ListByClusterID retrieves all jobs for a specific cluster.
// Jobs are ordered by creation time in descending order (newest first).
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

// GetByClusterIDAndType retrieves all jobs for a cluster with a specific job type
func (s *JobStore) GetByClusterIDAndType(ctx context.Context, clusterID string, jobType types.JobType) ([]*types.Job, error) {
	query := `
		SELECT id, cluster_id, job_type, status, attempt, max_attempts,
			error_code, error_message, started_at, ended_at,
			created_at, updated_at, metadata
		FROM jobs
		WHERE cluster_id = $1 AND job_type = $2
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, clusterID, jobType)
	if err != nil {
		return nil, fmt.Errorf("query jobs by cluster and type: %w", err)
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

// List retrieves jobs with pagination and returns the total count.
// Jobs are ordered by creation time in descending order (newest first).
// Returns a slice of jobs, the total count (before pagination), and an error if the query fails.
func (s *JobStore) List(ctx context.Context, offset, limit int) ([]*types.Job, int, error) {
	// Get total count
	var total int
	countQuery := `SELECT COUNT(*) FROM jobs`
	err := s.pool.QueryRow(ctx, countQuery).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}

	// Get jobs with pagination
	query := `
		SELECT id, cluster_id, job_type, status, attempt, max_attempts,
			error_code, error_message, started_at, ended_at,
			created_at, updated_at, metadata
		FROM jobs
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query jobs: %w", err)
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
			return nil, 0, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate jobs: %w", err)
	}

	return jobs, total, nil
}

// UpdateStatus updates a job's status and automatically updates the updated_at timestamp.
// Returns ErrNotFound if the job does not exist.
func (s *JobStore) UpdateStatus(ctx context.Context, id string, status types.JobStatus) error {
	query := `
		UPDATE jobs
		SET status = $1, updated_at = NOW()
		WHERE id = $2
	`

	result, err := s.pool.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("update job status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkStarted marks a job as RUNNING and sets the started_at timestamp to the current time.
// Returns ErrNotFound if the job does not exist.
func (s *JobStore) MarkStarted(ctx context.Context, id string) error {
	query := `
		UPDATE jobs
		SET status = $1, started_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`

	result, err := s.pool.Exec(ctx, query, types.JobStatusRunning, id)
	if err != nil {
		return fmt.Errorf("mark job started: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkSucceeded marks a job as SUCCEEDED, sets the ended_at timestamp, and updates the job metadata.
// The metadata typically contains result information from the job execution.
// Returns ErrNotFound if the job does not exist.
func (s *JobStore) MarkSucceeded(ctx context.Context, id string, metadata types.JobMetadata) error {
	query := `
		UPDATE jobs
		SET status = $1, metadata = $2, ended_at = NOW(), updated_at = NOW()
		WHERE id = $3
	`

	result, err := s.pool.Exec(ctx, query, types.JobStatusSucceeded, metadata, id)
	if err != nil {
		return fmt.Errorf("mark job succeeded: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkFailed marks a job as FAILED with error details and sets the ended_at timestamp.
// The errorCode and errorMessage provide diagnostic information about the failure.
// Returns ErrNotFound if the job does not exist.
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

// IncrementAttempt increments the job attempt counter and sets status to RETRYING.
// This is used when a job fails and will be retried based on its max_attempts setting.
// Returns ErrNotFound if the job does not exist.
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

// MarkFailedForRetry marks a job as failed but resets it to RETRYING status for automatic retry.
// This increments the attempt counter, records the error details, and resets timing fields.
// Used when a job fails due to transient errors (worker crashes, timeouts) that are worth retrying.
// Returns ErrNotFound if the job does not exist.
func (s *JobStore) MarkFailedForRetry(ctx context.Context, id string, errorCode, errorMessage string) error {
	query := `
		UPDATE jobs
		SET attempt = attempt + 1,
			status = $1,
			error_code = $2,
			error_message = $3,
			started_at = NULL,
			ended_at = NULL,
			updated_at = NOW()
		WHERE id = $4
	`

	result, err := s.pool.Exec(ctx, query, types.JobStatusRetrying, errorCode, errorMessage, id)
	if err != nil {
		return fmt.Errorf("mark job failed for retry: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetStuckJobs returns jobs in RUNNING status for longer than the specified threshold duration.
// These jobs may have failed without updating their status, typically due to worker crashes.
// Jobs are ordered by started_at in ascending order (oldest stuck jobs first).
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

// GetPending returns pending jobs (PENDING or RETRYING status) up to the specified limit.
// Jobs are ordered by creation time in ascending order (oldest jobs are returned first).
// This is used by workers to fetch jobs from the queue for processing.
func (s *JobStore) GetPending(ctx context.Context, limit int) ([]*types.Job, error) {
	query := `
		SELECT id, cluster_id, job_type, status, attempt, max_attempts,
			error_code, error_message, started_at, ended_at,
			created_at, updated_at, metadata
		FROM jobs
		WHERE status IN ('PENDING', 'RETRYING')
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending jobs: %w", err)
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
			return nil, fmt.Errorf("scan pending job: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending jobs: %w", err)
	}

	return jobs, nil
}

// CountPending returns the total count of jobs with PENDING or RETRYING status.
// This is used for monitoring queue depth and worker capacity planning.
func (s *JobStore) CountPending(ctx context.Context) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM jobs
		WHERE status IN ('PENDING', 'RETRYING')
	`

	var count int
	err := s.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending jobs: %w", err)
	}

	return count, nil
}
