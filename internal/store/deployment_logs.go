package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// DeploymentLogStore handles deployment log database operations
type DeploymentLogStore struct {
	pool *pgxpool.Pool
}

// AppendLogs inserts deployment logs in batch
func (s *DeploymentLogStore) AppendLogs(ctx context.Context, logs []*types.DeploymentLog) error {
	if len(logs) == 0 {
		return nil
	}

	query := `
		INSERT INTO deployment_logs (
			cluster_id, job_id, sequence, timestamp, log_level, message, source
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`

	batch := &pgx.Batch{}
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

	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(logs); i++ {
		_, err := br.Exec()
		if err != nil {
			return fmt.Errorf("insert log %d: %w", i, err)
		}
	}

	return nil
}

// GetLogs retrieves deployment logs with cursor-based pagination
func (s *DeploymentLogStore) GetLogs(ctx context.Context, clusterID, jobID string, afterSequence int64, limit int) ([]*types.DeploymentLog, error) {
	query := `
		SELECT id, cluster_id, job_id, sequence, timestamp, log_level, message, source
		FROM deployment_logs
		WHERE cluster_id = $1 AND job_id = $2 AND sequence > $3
		ORDER BY sequence ASC
		LIMIT $4
	`

	rows, err := s.pool.Query(ctx, query, clusterID, jobID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
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
			return nil, fmt.Errorf("scan log: %w", err)
		}
		logs = append(logs, &log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate logs: %w", err)
	}

	return logs, nil
}

// GetLogStats returns statistics about deployment logs
func (s *DeploymentLogStore) GetLogStats(ctx context.Context, clusterID, jobID string) (*types.DeploymentLogStats, error) {
	query := `
		SELECT
			COUNT(*) as total_lines,
			COUNT(*) FILTER (WHERE log_level = 'error') as error_count,
			COUNT(*) FILTER (WHERE log_level = 'warn' OR log_level = 'warning') as warn_count,
			MAX(timestamp) as last_updated
		FROM deployment_logs
		WHERE cluster_id = $1 AND job_id = $2
	`

	var stats types.DeploymentLogStats
	var lastUpdated *time.Time

	err := s.pool.QueryRow(ctx, query, clusterID, jobID).Scan(
		&stats.TotalLines,
		&stats.ErrorCount,
		&stats.WarnCount,
		&lastUpdated,
	)
	if err != nil {
		return nil, fmt.Errorf("query stats: %w", err)
	}

	if lastUpdated != nil {
		stats.LastUpdated = *lastUpdated
	}

	return &stats, nil
}

// DeleteByJobID deletes all logs for a job
func (s *DeploymentLogStore) DeleteByJobID(ctx context.Context, jobID string) error {
	query := `DELETE FROM deployment_logs WHERE job_id = $1`

	_, err := s.pool.Exec(ctx, query, jobID)
	if err != nil {
		return fmt.Errorf("delete logs: %w", err)
	}

	return nil
}
