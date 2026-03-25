package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ProfileDeploymentMetricsStore handles database operations for profile deployment time metrics.
// Metrics are calculated from the last 30 successful CREATE jobs per profile and updated
// periodically by the janitor service.
type ProfileDeploymentMetricsStore struct {
	pool *pgxpool.Pool
}

// GetByProfile retrieves deployment metrics for a specific profile.
// Returns nil if no metrics exist for the profile (e.g., no successful deployments yet).
func (s *ProfileDeploymentMetricsStore) GetByProfile(ctx context.Context, profile string) (*types.ProfileDeploymentMetrics, error) {
	query := `
		SELECT profile, avg_duration_seconds, min_duration_seconds, max_duration_seconds,
		       p50_duration_seconds, p95_duration_seconds, sample_count, success_count,
		       last_deployment_at, created_at, updated_at
		FROM profile_deployment_metrics
		WHERE profile = $1
	`

	var metrics types.ProfileDeploymentMetrics
	err := s.pool.QueryRow(ctx, query, profile).Scan(
		&metrics.Profile,
		&metrics.AvgDurationSeconds,
		&metrics.MinDurationSeconds,
		&metrics.MaxDurationSeconds,
		&metrics.P50DurationSeconds,
		&metrics.P95DurationSeconds,
		&metrics.SampleCount,
		&metrics.SuccessCount,
		&metrics.LastDeploymentAt,
		&metrics.CreatedAt,
		&metrics.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // No metrics available for this profile
		}
		return nil, fmt.Errorf("failed to get metrics for profile %s: %w", profile, err)
	}

	return &metrics, nil
}

// GetAll retrieves deployment metrics for all profiles that have data.
// Used to embed metrics in the profiles API response.
func (s *ProfileDeploymentMetricsStore) GetAll(ctx context.Context) ([]*types.ProfileDeploymentMetrics, error) {
	query := `
		SELECT profile, avg_duration_seconds, min_duration_seconds, max_duration_seconds,
		       p50_duration_seconds, p95_duration_seconds, sample_count, success_count,
		       last_deployment_at, created_at, updated_at
		FROM profile_deployment_metrics
		ORDER BY profile
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all metrics: %w", err)
	}
	defer rows.Close()

	var metricsList []*types.ProfileDeploymentMetrics
	for rows.Next() {
		var metrics types.ProfileDeploymentMetrics
		err := rows.Scan(
			&metrics.Profile,
			&metrics.AvgDurationSeconds,
			&metrics.MinDurationSeconds,
			&metrics.MaxDurationSeconds,
			&metrics.P50DurationSeconds,
			&metrics.P95DurationSeconds,
			&metrics.SampleCount,
			&metrics.SuccessCount,
			&metrics.LastDeploymentAt,
			&metrics.CreatedAt,
			&metrics.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metrics row: %w", err)
		}
		metricsList = append(metricsList, &metrics)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating metrics rows: %w", err)
	}

	return metricsList, nil
}

// Upsert creates or updates deployment metrics for a profile.
func (s *ProfileDeploymentMetricsStore) Upsert(ctx context.Context, metrics *types.ProfileDeploymentMetrics) error {
	query := `
		INSERT INTO profile_deployment_metrics (
			profile, avg_duration_seconds, min_duration_seconds, max_duration_seconds,
			p50_duration_seconds, p95_duration_seconds, sample_count, success_count,
			last_deployment_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (profile) DO UPDATE SET
			avg_duration_seconds = EXCLUDED.avg_duration_seconds,
			min_duration_seconds = EXCLUDED.min_duration_seconds,
			max_duration_seconds = EXCLUDED.max_duration_seconds,
			p50_duration_seconds = EXCLUDED.p50_duration_seconds,
			p95_duration_seconds = EXCLUDED.p95_duration_seconds,
			sample_count = EXCLUDED.sample_count,
			success_count = EXCLUDED.success_count,
			last_deployment_at = EXCLUDED.last_deployment_at,
			updated_at = NOW()
	`

	_, err := s.pool.Exec(ctx, query,
		metrics.Profile,
		metrics.AvgDurationSeconds,
		metrics.MinDurationSeconds,
		metrics.MaxDurationSeconds,
		metrics.P50DurationSeconds,
		metrics.P95DurationSeconds,
		metrics.SampleCount,
		metrics.SuccessCount,
		metrics.LastDeploymentAt,
	)

	if err != nil {
		return fmt.Errorf("failed to upsert metrics for profile %s: %w", metrics.Profile, err)
	}

	return nil
}

// UpdateMetricsForProfile calculates and updates deployment metrics for a single profile
// based on the last 30 successful CREATE jobs. Returns true if metrics were updated.
func (s *ProfileDeploymentMetricsStore) UpdateMetricsForProfile(ctx context.Context, profile string) (bool, error) {
	// Calculate metrics from last 30 successful deployments
	query := `
		WITH recent_deployments AS (
			SELECT
				EXTRACT(EPOCH FROM (j.ended_at - j.started_at))::INTEGER as duration_seconds,
				j.ended_at
			FROM jobs j
			JOIN clusters c ON j.cluster_id = c.id
			WHERE j.job_type = 'CREATE'
				AND j.status = 'SUCCEEDED'
				AND j.started_at IS NOT NULL
				AND j.ended_at IS NOT NULL
				AND c.profile = $1
			ORDER BY j.ended_at DESC
			LIMIT 30
		),
		total_deployments AS (
			SELECT COUNT(*)::INTEGER as total_count
			FROM jobs j
			JOIN clusters c ON j.cluster_id = c.id
			WHERE j.job_type = 'CREATE'
				AND j.status = 'SUCCEEDED'
				AND j.started_at IS NOT NULL
				AND j.ended_at IS NOT NULL
				AND c.profile = $1
		),
		stats AS (
			SELECT
				AVG(duration_seconds)::INTEGER as avg_duration,
				MIN(duration_seconds) as min_duration,
				MAX(duration_seconds) as max_duration,
				PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_seconds)::INTEGER as p50_duration,
				PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_seconds)::INTEGER as p95_duration,
				COUNT(*)::INTEGER as sample_count,
				MAX(ended_at) as last_deployment
			FROM recent_deployments
		)
		SELECT
			s.avg_duration,
			s.min_duration,
			s.max_duration,
			s.p50_duration,
			s.p95_duration,
			s.sample_count,
			t.total_count,
			s.last_deployment
		FROM stats s, total_deployments t
	`

	var (
		avgDuration      int
		minDuration      int
		maxDuration      int
		p50Duration      *int
		p95Duration      *int
		sampleCount      int
		totalCount       int
		lastDeploymentAt *time.Time
	)

	err := s.pool.QueryRow(ctx, query, profile).Scan(
		&avgDuration,
		&minDuration,
		&maxDuration,
		&p50Duration,
		&p95Duration,
		&sampleCount,
		&totalCount,
		&lastDeploymentAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			// No successful deployments for this profile
			return false, nil
		}
		return false, fmt.Errorf("failed to calculate metrics for profile %s: %w", profile, err)
	}

	// Skip if no deployments found
	if sampleCount == 0 {
		return false, nil
	}

	// Upsert metrics
	metrics := &types.ProfileDeploymentMetrics{
		Profile:            profile,
		AvgDurationSeconds: avgDuration,
		MinDurationSeconds: minDuration,
		MaxDurationSeconds: maxDuration,
		P50DurationSeconds: p50Duration,
		P95DurationSeconds: p95Duration,
		SampleCount:        sampleCount,
		SuccessCount:       totalCount,
		LastDeploymentAt:   lastDeploymentAt,
	}

	if err := s.Upsert(ctx, metrics); err != nil {
		return false, fmt.Errorf("failed to upsert metrics: %w", err)
	}

	return true, nil
}

// UpdateAllMetrics calculates and updates deployment metrics for all profiles that have
// successful CREATE jobs. Returns the number of profiles updated.
// This is called periodically by the janitor service.
func (s *ProfileDeploymentMetricsStore) UpdateAllMetrics(ctx context.Context) (int, error) {
	// Get list of profiles with successful deployments
	query := `
		SELECT DISTINCT c.profile
		FROM jobs j
		JOIN clusters c ON j.cluster_id = c.id
		WHERE j.job_type = 'CREATE'
			AND j.status = 'SUCCEEDED'
			AND j.started_at IS NOT NULL
			AND j.ended_at IS NOT NULL
		ORDER BY c.profile
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to get profiles with deployments: %w", err)
	}
	defer rows.Close()

	var profiles []string
	for rows.Next() {
		var profile string
		if err := rows.Scan(&profile); err != nil {
			return 0, fmt.Errorf("failed to scan profile: %w", err)
		}
		profiles = append(profiles, profile)
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating profiles: %w", err)
	}

	// Update metrics for each profile
	updatedCount := 0
	for _, profile := range profiles {
		updated, err := s.UpdateMetricsForProfile(ctx, profile)
		if err != nil {
			return updatedCount, fmt.Errorf("failed to update metrics for profile %s: %w", profile, err)
		}
		if updated {
			updatedCount++
		}
	}

	return updatedCount, nil
}
