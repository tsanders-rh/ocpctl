package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobRetryHistory represents a retry attempt record
type JobRetryHistory struct {
	ID           int64
	JobID        string
	Attempt      int
	ErrorCode    *string
	ErrorMessage *string
	FailedAt     time.Time
}

// JobRetryHistoryStore handles job retry history operations
type JobRetryHistoryStore struct {
	pool *pgxpool.Pool
}

// RecordRetry records a failed job attempt
func (s *JobRetryHistoryStore) RecordRetry(ctx context.Context, jobID string, attempt int, errorCode, errorMessage string) error {
	query := `
		INSERT INTO job_retry_history (job_id, attempt, error_code, error_message, failed_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (job_id, attempt) DO UPDATE
		SET error_code = EXCLUDED.error_code,
		    error_message = EXCLUDED.error_message,
		    failed_at = EXCLUDED.failed_at
	`

	_, err := s.pool.Exec(ctx, query, jobID, attempt, errorCode, errorMessage)
	if err != nil {
		return fmt.Errorf("record retry: %w", err)
	}

	return nil
}

// GetRetryHistory retrieves all retry attempts for a job
func (s *JobRetryHistoryStore) GetRetryHistory(ctx context.Context, jobID string) ([]*JobRetryHistory, error) {
	query := `
		SELECT id, job_id, attempt, error_code, error_message, failed_at
		FROM job_retry_history
		WHERE job_id = $1
		ORDER BY attempt ASC
	`

	rows, err := s.pool.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("query retry history: %w", err)
	}
	defer rows.Close()

	var history []*JobRetryHistory
	for rows.Next() {
		var h JobRetryHistory
		err := rows.Scan(&h.ID, &h.JobID, &h.Attempt, &h.ErrorCode, &h.ErrorMessage, &h.FailedAt)
		if err != nil {
			return nil, fmt.Errorf("scan retry history: %w", err)
		}
		history = append(history, &h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retry history: %w", err)
	}

	return history, nil
}

// GetRecentRetries returns retry attempts within a time window
func (s *JobRetryHistoryStore) GetRecentRetries(ctx context.Context, since time.Duration) ([]*JobRetryHistory, error) {
	query := `
		SELECT id, job_id, attempt, error_code, error_message, failed_at
		FROM job_retry_history
		WHERE failed_at >= NOW() - $1::interval
		ORDER BY failed_at DESC
	`

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("query recent retries: %w", err)
	}
	defer rows.Close()

	var history []*JobRetryHistory
	for rows.Next() {
		var h JobRetryHistory
		err := rows.Scan(&h.ID, &h.JobID, &h.Attempt, &h.ErrorCode, &h.ErrorMessage, &h.FailedAt)
		if err != nil {
			return nil, fmt.Errorf("scan retry history: %w", err)
		}
		history = append(history, &h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent retries: %w", err)
	}

	return history, nil
}

// GetRetryStats returns statistics about retries
func (s *JobRetryHistoryStore) GetRetryStats(ctx context.Context, since time.Duration) (map[string]int, error) {
	query := `
		SELECT error_code, COUNT(*) as count
		FROM job_retry_history
		WHERE failed_at >= NOW() - $1::interval
		  AND error_code IS NOT NULL
		GROUP BY error_code
		ORDER BY count DESC
	`

	rows, err := s.pool.Query(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("query retry stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var errorCode string
		var count int
		if err := rows.Scan(&errorCode, &count); err != nil {
			return nil, fmt.Errorf("scan retry stats: %w", err)
		}
		stats[errorCode] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retry stats: %w", err)
	}

	return stats, nil
}
