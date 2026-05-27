package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PoolStore handles database operations for cluster pools
type PoolStore struct {
	pool *pgxpool.Pool
}

// Create creates a new cluster pool
func (s *PoolStore) Create(ctx context.Context, tx pgx.Tx, pool *types.ClusterPool) error {
	query := `
		INSERT INTO cluster_pools (
			id, name, display_name, description, profile,
			target_size, min_size, max_size,
			max_lease_duration_hours, auto_release_enabled,
			max_cluster_age_days, auto_refresh_enabled,
			scheduled_mode, schedule_timezone, schedule_start_hour, schedule_end_hour, schedule_days_of_week,
			cluster_config, enabled, created_by
		) VALUES (
			gen_random_uuid(), $1, $2, $3, $4,
			$5, $6, $7,
			$8, $9,
			$10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, $19
		)
		RETURNING id, created_at, updated_at
	`

	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, query,
			pool.Name, pool.DisplayName, pool.Description, pool.Profile,
			pool.TargetSize, pool.MinSize, pool.MaxSize,
			pool.MaxLeaseDurationHours, pool.AutoReleaseEnabled,
			pool.MaxClusterAgeDays, pool.AutoRefreshEnabled,
			pool.ScheduledMode, pool.ScheduleTimezone, pool.ScheduleStartHour, pool.ScheduleEndHour, pool.ScheduleDaysOfWeek,
			pool.ClusterConfig, pool.Enabled, pool.CreatedBy,
		)
	} else {
		row = s.pool.QueryRow(ctx, query,
			pool.Name, pool.DisplayName, pool.Description, pool.Profile,
			pool.TargetSize, pool.MinSize, pool.MaxSize,
			pool.MaxLeaseDurationHours, pool.AutoReleaseEnabled,
			pool.MaxClusterAgeDays, pool.AutoRefreshEnabled,
			pool.ScheduledMode, pool.ScheduleTimezone, pool.ScheduleStartHour, pool.ScheduleEndHour, pool.ScheduleDaysOfWeek,
			pool.ClusterConfig, pool.Enabled, pool.CreatedBy,
		)
	}

	return row.Scan(&pool.ID, &pool.CreatedAt, &pool.UpdatedAt)
}

// GetByID retrieves a pool by ID
func (s *PoolStore) GetByID(ctx context.Context, poolID string) (*types.ClusterPool, error) {
	query := `
		SELECT
			id, name, display_name, description, profile,
			target_size, min_size, max_size,
			max_lease_duration_hours, auto_release_enabled,
			max_cluster_age_days, auto_refresh_enabled,
			scheduled_mode, schedule_timezone, schedule_start_hour, schedule_end_hour, schedule_days_of_week,
			cluster_config, enabled, created_at, updated_at, created_by
		FROM cluster_pools
		WHERE id = $1
	`

	pool := &types.ClusterPool{}
	row := s.pool.QueryRow(ctx, query, poolID)

	err := row.Scan(
		&pool.ID, &pool.Name, &pool.DisplayName, &pool.Description, &pool.Profile,
		&pool.TargetSize, &pool.MinSize, &pool.MaxSize,
		&pool.MaxLeaseDurationHours, &pool.AutoReleaseEnabled,
		&pool.MaxClusterAgeDays, &pool.AutoRefreshEnabled,
		&pool.ScheduledMode, &pool.ScheduleTimezone, &pool.ScheduleStartHour, &pool.ScheduleEndHour, &pool.ScheduleDaysOfWeek,
		&pool.ClusterConfig, &pool.Enabled, &pool.CreatedAt, &pool.UpdatedAt, &pool.CreatedBy,
	)

	if err == pgx.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	return pool, nil
}

// GetByName retrieves a pool by name
func (s *PoolStore) GetByName(ctx context.Context, name string) (*types.ClusterPool, error) {
	query := `
		SELECT
			id, name, display_name, description, profile,
			target_size, min_size, max_size,
			max_lease_duration_hours, auto_release_enabled,
			max_cluster_age_days, auto_refresh_enabled,
			scheduled_mode, schedule_timezone, schedule_start_hour, schedule_end_hour, schedule_days_of_week,
			cluster_config, enabled, created_at, updated_at, created_by
		FROM cluster_pools
		WHERE name = $1
	`

	pool := &types.ClusterPool{}
	row := s.pool.QueryRow(ctx, query, name)

	err := row.Scan(
		&pool.ID, &pool.Name, &pool.DisplayName, &pool.Description, &pool.Profile,
		&pool.TargetSize, &pool.MinSize, &pool.MaxSize,
		&pool.MaxLeaseDurationHours, &pool.AutoReleaseEnabled,
		&pool.MaxClusterAgeDays, &pool.AutoRefreshEnabled,
		&pool.ScheduledMode, &pool.ScheduleTimezone, &pool.ScheduleStartHour, &pool.ScheduleEndHour, &pool.ScheduleDaysOfWeek,
		&pool.ClusterConfig, &pool.Enabled, &pool.CreatedAt, &pool.UpdatedAt, &pool.CreatedBy,
	)

	if err == pgx.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	return pool, nil
}

// List retrieves all pools with optional filtering
func (s *PoolStore) List(ctx context.Context, enabledOnly bool) ([]*types.ClusterPool, error) {
	query := `
		SELECT
			cp.id, cp.name, cp.display_name, cp.description, cp.profile,
			cp.target_size, cp.min_size, cp.max_size,
			cp.max_lease_duration_hours, cp.auto_release_enabled,
			cp.max_cluster_age_days, cp.auto_refresh_enabled,
			cp.scheduled_mode, cp.schedule_timezone, cp.schedule_start_hour, cp.schedule_end_hour, cp.schedule_days_of_week,
			cp.cluster_config, cp.enabled, cp.created_at, cp.updated_at,
			COALESCE(u.username, cp.created_by) as created_by
		FROM cluster_pools cp
		LEFT JOIN users u ON cp.created_by = u.id::text
	`
	args := []interface{}{}

	if enabledOnly {
		query += ` WHERE cp.enabled = true`
	}

	query += ` ORDER BY cp.name ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pools []*types.ClusterPool
	for rows.Next() {
		pool := &types.ClusterPool{}
		err := rows.Scan(
			&pool.ID, &pool.Name, &pool.DisplayName, &pool.Description, &pool.Profile,
			&pool.TargetSize, &pool.MinSize, &pool.MaxSize,
			&pool.MaxLeaseDurationHours, &pool.AutoReleaseEnabled,
			&pool.MaxClusterAgeDays, &pool.AutoRefreshEnabled,
			&pool.ScheduledMode, &pool.ScheduleTimezone, &pool.ScheduleStartHour, &pool.ScheduleEndHour, &pool.ScheduleDaysOfWeek,
			&pool.ClusterConfig, &pool.Enabled, &pool.CreatedAt, &pool.UpdatedAt, &pool.CreatedBy,
		)
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}

	return pools, rows.Err()
}

// Update updates a pool
func (s *PoolStore) Update(ctx context.Context, tx pgx.Tx, poolID string, updates map[string]interface{}) error {
	// Build dynamic UPDATE query
	query := "UPDATE cluster_pools SET "
	args := []interface{}{}
	argNum := 1

	for k, v := range updates {
		if argNum > 1 {
			query += ", "
		}
		query += fmt.Sprintf("%s = $%d", k, argNum)
		args = append(args, v)
		argNum++
	}

	query += fmt.Sprintf(", updated_at = NOW() WHERE id = $%d", argNum)
	args = append(args, poolID)

	var result pgconn.CommandTag
	var err error
	if tx != nil {
		result, err = tx.Exec(ctx, query, args...)
	} else {
		result, err = s.pool.Exec(ctx, query, args...)
	}
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete deletes a pool (and orphans its clusters by setting pool_id = NULL)
func (s *PoolStore) Delete(ctx context.Context, poolID string) error {
	query := `DELETE FROM cluster_pools WHERE id = $1`

	result, err := s.pool.Exec(ctx, query, poolID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// GetStats retrieves real-time statistics for a pool
func (s *PoolStore) GetStats(ctx context.Context, poolID string) (*types.ClusterPoolStats, error) {
	query := `
		SELECT
			$1::uuid AS pool_id,
			(SELECT name FROM cluster_pools WHERE id = $1::uuid) AS pool_name,
			COUNT(*) AS total_clusters,
			COUNT(*) FILTER (WHERE pool_state = 'READY') AS ready_clusters,
			COUNT(*) FILTER (WHERE pool_state = 'LEASED') AS leased_clusters,
			COUNT(*) FILTER (WHERE pool_state = 'PROVISIONING') AS provisioning_clusters,
			COUNT(*) FILTER (WHERE pool_state = 'CLEANING') AS cleaning_clusters,
			COUNT(*) FILTER (WHERE pool_state = 'EXPIRED') AS expired_clusters,
			EXTRACT(EPOCH FROM MAX(NOW() - created_at)) AS oldest_cluster_age_secs,
			EXTRACT(EPOCH FROM AVG(NOW() - created_at)) AS avg_cluster_age_secs,
			COUNT(*) FILTER (WHERE leased_by IS NOT NULL) AS active_leases,
			AVG(EXTRACT(EPOCH FROM (COALESCE(lease_expires_at, NOW()) - leased_at))) AS avg_lease_duration
		FROM clusters
		WHERE pool_id = $1::uuid AND pool_state IS NOT NULL
	`

	stats := &types.ClusterPoolStats{ComputedAt: time.Now()}
	var oldestClusterAgeSecs sql.NullFloat64
	var avgClusterAgeSecs sql.NullFloat64
	var avgLeaseDurationSecs sql.NullFloat64

	err := s.pool.QueryRow(ctx, query, poolID).Scan(
		&stats.PoolID,
		&stats.PoolName,
		&stats.TotalClusters,
		&stats.ReadyClusters,
		&stats.LeasedClusters,
		&stats.ProvisioningClusters,
		&stats.CleaningClusters,
		&stats.ExpiredClusters,
		&oldestClusterAgeSecs,
		&avgClusterAgeSecs,
		&stats.ActiveLeases,
		&avgLeaseDurationSecs,
	)

	if err != nil {
		return nil, err
	}

	// Convert seconds to time.Duration
	if oldestClusterAgeSecs.Valid {
		stats.OldestClusterAge = time.Duration(oldestClusterAgeSecs.Float64) * time.Second
	}
	if avgClusterAgeSecs.Valid {
		stats.AvgClusterAge = time.Duration(avgClusterAgeSecs.Float64) * time.Second
	}

	// Calculate utilization percentage
	availableSlots := stats.ReadyClusters + stats.LeasedClusters
	if availableSlots > 0 {
		stats.UtilizationPercent = float64(stats.LeasedClusters) / float64(availableSlots) * 100
	}

	// Calculate capacity percentage (requires target_size from pool)
	pool, err := s.GetByID(ctx, poolID)
	if err == nil {
		stats.CapacityPercent = float64(stats.TotalClusters) / float64(pool.TargetSize) * 100
	}

	// Convert average lease duration to time.Duration
	if avgLeaseDurationSecs.Valid {
		stats.AvgLeaseDuration = time.Duration(avgLeaseDurationSecs.Float64) * time.Second
	}

	return stats, nil
}

// LeaseCluster atomically leases an available cluster from a pool
func (s *PoolStore) LeaseCluster(ctx context.Context, poolName string, request *types.LeaseRequest) (*types.Cluster, error) {
	// Get pool to determine lease duration
	pool, err := s.GetByName(ctx, poolName)
	if err != nil {
		return nil, fmt.Errorf("pool not found: %w", err)
	}

	if !pool.Enabled {
		return nil, fmt.Errorf("pool %s is disabled", poolName)
	}

	// Determine lease duration (use override if provided, otherwise pool default)
	leaseDurationHours := pool.MaxLeaseDurationHours
	if request.Duration != nil && *request.Duration > 0 && *request.Duration <= pool.MaxLeaseDurationHours {
		leaseDurationHours = *request.Duration
	}

	// Atomically find and lease a READY cluster
	query := `
		UPDATE clusters
		SET pool_state = 'LEASED',
			leased_by = $1,
			leased_at = NOW(),
			lease_expires_at = NOW() + interval '1 hour' * $2,
			lease_metadata = $3,
			updated_at = NOW()
		WHERE id = (
			SELECT id FROM clusters
			WHERE pool_id = $4
			AND pool_state = 'READY'
			AND status = 'READY'
			ORDER BY created_at ASC  -- Lease oldest cluster first
			LIMIT 1
			FOR UPDATE SKIP LOCKED  -- Skip locked rows to avoid contention
		)
		RETURNING id, name, platform, cluster_type, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours, destroy_at,
			created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in,
			work_hours_enabled, work_hours_start, work_hours_end, work_days, last_work_hours_check,
			post_deploy_status, post_deploy_completed_at,
			skip_post_deployment, custom_post_config, storage_config,
			preserve_on_failure, credentials_mode, custom_pull_secret,
			pool_id, pool_state, leased_by, leased_at, lease_expires_at, lease_metadata,
			pool_generation, last_cleaned_at
	`

	cluster := &types.Cluster{}
	row := s.pool.QueryRow(ctx, query, request.LeasedBy, leaseDurationHours, request.Metadata, pool.ID)

	err = scanCluster(row, cluster)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("no available clusters in pool %s", poolName)
	}
	if err != nil {
		return nil, err
	}

	return cluster, nil
}

// ReleaseCluster releases a leased cluster back to the pool
func (s *PoolStore) ReleaseCluster(ctx context.Context, clusterID string) error {
	// Transition cluster to CLEANING state and clear lease information
	query := `
		UPDATE clusters
		SET pool_state = 'CLEANING',
			leased_by = NULL,
			leased_at = NULL,
			lease_expires_at = NULL,
			lease_metadata = NULL,
			updated_at = NOW()
		WHERE id = $1
		AND pool_state = 'LEASED'
	`

	result, err := s.pool.Exec(ctx, query, clusterID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("cluster %s is not leased or does not exist", clusterID)
	}

	return nil
}

// Helper function to scan cluster row
func scanCluster(row pgx.Row, cluster *types.Cluster) error {
	return row.Scan(
		&cluster.ID, &cluster.Name, &cluster.Platform, &cluster.ClusterType,
		&cluster.Version, &cluster.Profile, &cluster.Region, &cluster.BaseDomain,
		&cluster.Owner, &cluster.OwnerID, &cluster.Team, &cluster.CostCenter,
		&cluster.Status, &cluster.RequestedBy, &cluster.TTLHours, &cluster.DestroyAt,
		&cluster.CreatedAt, &cluster.UpdatedAt, &cluster.DestroyedAt,
		&cluster.RequestTags, &cluster.EffectiveTags,
		&cluster.SSHPublicKey, &cluster.OffhoursOptIn,
		&cluster.WorkHoursEnabled, &cluster.WorkHoursStart, &cluster.WorkHoursEnd, &cluster.WorkDays,
		&cluster.LastWorkHoursCheck, &cluster.PostDeployStatus, &cluster.PostDeployCompletedAt,
		&cluster.SkipPostDeployment, &cluster.CustomPostConfig, &cluster.StorageConfig,
		&cluster.PreserveOnFailure, &cluster.CredentialsMode, &cluster.CustomPullSecret,
		&cluster.PoolID, &cluster.PoolState, &cluster.LeasedBy, &cluster.LeasedAt,
		&cluster.LeaseExpiresAt, &cluster.LeaseMetadata, &cluster.PoolGeneration, &cluster.LastCleanedAt,
	)
}
