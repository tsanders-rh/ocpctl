package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// DeploymentLogStore handles deployment log database operations
type DeploymentLogStore struct {
	pool *pgxpool.Pool
}

// AppendLogs batch inserts deployment log entries
// This is called by the LogStreamer to write logs incrementally during deployment
func (s *DeploymentLogStore) AppendLogs(ctx context.Context, logs []*types.DeploymentLog) error {
	if len(logs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}

	query := `
		INSERT INTO deployment_logs (
			cluster_id, job_id, sequence, timestamp, log_level, message, source
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
		ON CONFLICT (cluster_id, job_id, sequence) DO NOTHING
	`

	for _, log := range logs {
		batch.Queue(query,
			log.ClusterID,
			log.JobID,
			log.Sequence,
			log.Timestamp,
			log.LogLevel,
			log.Message,
			log.Source,
		)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	// Process all results
	for i := 0; i < len(logs); i++ {
		_, err := results.Exec()
		if err != nil {
			return fmt.Errorf("batch insert deployment logs (item %d): %w", i, err)
		}
	}

	return nil
}

// GetLogs retrieves deployment logs with cursor-based pagination
// afterSequence=0 means from the beginning
func (s *DeploymentLogStore) GetLogs(ctx context.Context, clusterID, jobID string, afterSequence int64, limit int) ([]*types.DeploymentLog, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}

	query := `
		SELECT id, cluster_id, job_id, sequence, timestamp, log_level, message, source
		FROM deployment_logs
		WHERE cluster_id = $1 AND job_id = $2 AND sequence > $3
		ORDER BY sequence ASC
		LIMIT $4
	`

	rows, err := s.pool.Query(ctx, query, clusterID, jobID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("query deployment logs: %w", err)
	}
	defer rows.Close()

	logs := []*types.DeploymentLog{}
	for rows.Next() {
		var log types.DeploymentLog
		err := rows.Scan(
			&log.ID,
			&log.ClusterID,
			&log.JobID,
			&log.Sequence,
			&log.Timestamp,
			&log.LogLevel,
			&log.Message,
			&log.Source,
		)
		if err != nil {
			return nil, fmt.Errorf("scan deployment log: %w", err)
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deployment logs: %w", err)
	}

	return logs, nil
}

// GetLogStats returns summary statistics about deployment logs for a job
func (s *DeploymentLogStore) GetLogStats(ctx context.Context, clusterID, jobID string) (*types.DeploymentLogStats, error) {
	query := `
		SELECT
			COUNT(*) as total_lines,
			COUNT(*) FILTER (WHERE log_level = 'error') as error_count,
			COUNT(*) FILTER (WHERE log_level = 'warn') as warn_count,
			MAX(timestamp) as last_updated
		FROM deployment_logs
		WHERE cluster_id = $1 AND job_id = $2
	`

	var stats types.DeploymentLogStats
	err := s.pool.QueryRow(ctx, query, clusterID, jobID).Scan(
		&stats.TotalLines,
		&stats.ErrorCount,
		&stats.WarnCount,
		&stats.LastUpdated,
	)

	if err == pgx.ErrNoRows {
		// No logs yet, return empty stats
		return &types.DeploymentLogStats{
			TotalLines:  0,
			ErrorCount:  0,
			WarnCount:   0,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query deployment log stats: %w", err)
	}

	return &stats, nil
}

// DeleteByJobID removes all deployment logs for a job
// This is called during cleanup/garbage collection
func (s *DeploymentLogStore) DeleteByJobID(ctx context.Context, jobID string) error {
	query := `
		DELETE FROM deployment_logs
		WHERE job_id = $1
	`

	_, err := s.pool.Exec(ctx, query, jobID)
	if err != nil {
		return fmt.Errorf("delete deployment logs by job ID: %w", err)
	}

	return nil
}

// DeleteByClusterID removes all deployment logs for a cluster
// This is called when a cluster is permanently deleted
func (s *DeploymentLogStore) DeleteByClusterID(ctx context.Context, clusterID string) error {
	query := `
		DELETE FROM deployment_logs
		WHERE cluster_id = $1
	`

	_, err := s.pool.Exec(ctx, query, clusterID)
	if err != nil {
		return fmt.Errorf("delete deployment logs by cluster ID: %w", err)
	}

	return nil
}
