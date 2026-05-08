package store

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterStore handles cluster database operations
type ClusterStore struct {
	pool *pgxpool.Pool
}

// Create inserts a new cluster record into the database.
// Returns an error if the cluster ID already exists or if the database operation fails.
func (s *ClusterStore) Create(ctx context.Context, cluster *types.Cluster) error {
	query := `
		INSERT INTO clusters (
			id, name, platform, cluster_type, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, request_tags, effective_tags, ssh_public_key,
			offhours_opt_in, work_hours_enabled, work_hours_start, work_hours_end, work_days,
			skip_post_deployment, custom_post_config, post_deploy_status, preserve_on_failure, credentials_mode, custom_pull_secret
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30
		)
	`

	_, err := s.pool.Exec(ctx, query,
		cluster.ID,
		cluster.Name,
		cluster.Platform,
		cluster.ClusterType,
		cluster.Version,
		cluster.Profile,
		cluster.Region,
		cluster.BaseDomain, // nil pointer becomes NULL in database
		cluster.Owner,
		cluster.OwnerID,
		cluster.Team,
		cluster.CostCenter,
		cluster.Status,
		cluster.RequestedBy,
		cluster.TTLHours,
		cluster.DestroyAt,
		cluster.RequestTags,
		cluster.EffectiveTags,
		cluster.SSHPublicKey,
		cluster.OffhoursOptIn,
		cluster.WorkHoursEnabled,
		cluster.WorkHoursStart,
		cluster.WorkHoursEnd,
		cluster.WorkDays,
		cluster.SkipPostDeployment,
		cluster.CustomPostConfig,
		cluster.PostDeployStatus,
		cluster.PreserveOnFailure,
		cluster.CredentialsMode,
		cluster.CustomPullSecret,
	)

	if err != nil {
		return fmt.Errorf("insert cluster: %w", err)
	}

	return nil
}

// GetByID retrieves a cluster by its unique identifier.
// Returns ErrNotFound if no cluster exists with the given ID.
func (s *ClusterStore) GetByID(ctx context.Context, id string) (*types.Cluster, error) {
	query := `
		SELECT id, name, platform, cluster_type, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in,
			work_hours_enabled, work_hours_start, work_hours_end, work_days, last_work_hours_check,
			skip_post_deployment, custom_post_config, post_deploy_status, preserve_on_failure, credentials_mode, custom_pull_secret
		FROM clusters
		WHERE id = $1
	`

	var cluster types.Cluster
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&cluster.ID,
		&cluster.Name,
		&cluster.Platform,
		&cluster.ClusterType,
		&cluster.Version,
		&cluster.Profile,
		&cluster.Region,
		&cluster.BaseDomain,
		&cluster.Owner,
		&cluster.OwnerID,
		&cluster.Team,
		&cluster.CostCenter,
		&cluster.Status,
		&cluster.RequestedBy,
		&cluster.TTLHours,
		&cluster.DestroyAt,
		&cluster.CreatedAt,
		&cluster.UpdatedAt,
		&cluster.DestroyedAt,
		&cluster.RequestTags,
		&cluster.EffectiveTags,
		&cluster.SSHPublicKey,
		&cluster.OffhoursOptIn,
		&cluster.WorkHoursEnabled,
		&cluster.WorkHoursStart,
		&cluster.WorkHoursEnd,
		&cluster.WorkDays,
		&cluster.LastWorkHoursCheck,
		&cluster.SkipPostDeployment,
		&cluster.CustomPostConfig,
		&cluster.PostDeployStatus,
		&cluster.PreserveOnFailure,
		&cluster.CredentialsMode,
		&cluster.CustomPullSecret,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query cluster: %w", err)
	}

	return &cluster, nil
}

// GetByIDs retrieves multiple clusters by their IDs in a single query
// This prevents N+1 query patterns when fetching clusters for multiple jobs
func (s *ClusterStore) GetByIDs(ctx context.Context, ids []string) ([]*types.Cluster, error) {
	if len(ids) == 0 {
		return []*types.Cluster{}, nil
	}

	query := `
		SELECT id, name, platform, cluster_type, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in,
			work_hours_enabled, work_hours_start, work_hours_end, work_days, last_work_hours_check,
			skip_post_deployment, custom_post_config, post_deploy_status, preserve_on_failure, credentials_mode, custom_pull_secret
		FROM clusters
		WHERE id = ANY($1)
	`

	rows, err := s.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("query clusters by IDs: %w", err)
	}
	defer rows.Close()

	clusters := []*types.Cluster{}
	for rows.Next() {
		var cluster types.Cluster
		err := rows.Scan(
			&cluster.ID,
			&cluster.Name,
			&cluster.Platform,
			&cluster.ClusterType,
			&cluster.Version,
			&cluster.Profile,
			&cluster.Region,
			&cluster.BaseDomain,
			&cluster.Owner,
			&cluster.OwnerID,
			&cluster.Team,
			&cluster.CostCenter,
			&cluster.Status,
			&cluster.RequestedBy,
			&cluster.TTLHours,
			&cluster.DestroyAt,
			&cluster.CreatedAt,
			&cluster.UpdatedAt,
			&cluster.DestroyedAt,
			&cluster.RequestTags,
			&cluster.EffectiveTags,
			&cluster.SSHPublicKey,
			&cluster.OffhoursOptIn,
			&cluster.WorkHoursEnabled,
			&cluster.WorkHoursStart,
			&cluster.WorkHoursEnd,
			&cluster.WorkDays,
			&cluster.LastWorkHoursCheck,
			&cluster.SkipPostDeployment,
			&cluster.CustomPostConfig,
			&cluster.PostDeployStatus,
			&cluster.PreserveOnFailure,
			&cluster.CredentialsMode,
			&cluster.CustomPullSecret,
		)
		if err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		clusters = append(clusters, &cluster)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clusters: %w", err)
	}

	return clusters, nil
}

// GetByIDForUpdate retrieves a cluster by ID with a row-level lock (FOR UPDATE) to prevent concurrent modifications.
// Must be called within a transaction. Returns ErrNotFound if the cluster does not exist.
func (s *ClusterStore) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*types.Cluster, error) {
	query := `
		SELECT id, name, platform, cluster_type, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in,
			work_hours_enabled, work_hours_start, work_hours_end, work_days, last_work_hours_check,
			skip_post_deployment, custom_post_config, post_deploy_status, preserve_on_failure, credentials_mode, custom_pull_secret
		FROM clusters
		WHERE id = $1
		FOR UPDATE
	`

	var cluster types.Cluster
	err := tx.QueryRow(ctx, query, id).Scan(
		&cluster.ID,
		&cluster.Name,
		&cluster.Platform,
		&cluster.ClusterType,
		&cluster.Version,
		&cluster.Profile,
		&cluster.Region,
		&cluster.BaseDomain,
		&cluster.Owner,
		&cluster.OwnerID,
		&cluster.Team,
		&cluster.CostCenter,
		&cluster.Status,
		&cluster.RequestedBy,
		&cluster.TTLHours,
		&cluster.DestroyAt,
		&cluster.CreatedAt,
		&cluster.UpdatedAt,
		&cluster.DestroyedAt,
		&cluster.RequestTags,
		&cluster.EffectiveTags,
		&cluster.SSHPublicKey,
		&cluster.OffhoursOptIn,
		&cluster.WorkHoursEnabled,
		&cluster.WorkHoursStart,
		&cluster.WorkHoursEnd,
		&cluster.WorkDays,
		&cluster.LastWorkHoursCheck,
		&cluster.SkipPostDeployment,
		&cluster.CustomPostConfig,
		&cluster.PostDeployStatus,
		&cluster.PreserveOnFailure,
		&cluster.CredentialsMode,
		&cluster.CustomPullSecret,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query cluster for update: %w", err)
	}

	return &cluster, nil
}

// ListFilters contains filter options for listing clusters
type ListFilters struct {
	Status   *types.ClusterStatus
	Platform *types.Platform
	Owner    *string  // Filter by owner email
	OwnerID  *string  // Filter by owner user ID
	Team     *string
	Profile  *string
	Limit    int
	Offset   int
}

// List retrieves clusters with optional filtering and pagination.
// Returns a slice of clusters, the total count (before pagination), and an error if the query fails.
// Clusters are ordered by creation time in descending order (newest first).
func (s *ClusterStore) List(ctx context.Context, filters ListFilters) ([]*types.Cluster, int, error) {
	// Build query dynamically based on filters
	query := `
		SELECT c.id, c.name, c.platform, c.cluster_type, c.version, c.profile, c.region, c.base_domain,
			c.owner, c.owner_id, c.team, c.cost_center, c.status, c.requested_by, c.ttl_hours,
			c.destroy_at, c.created_at, c.updated_at, c.destroyed_at,
			c.request_tags, c.effective_tags, c.ssh_public_key, c.offhours_opt_in,
			c.work_hours_enabled, c.work_hours_start, c.work_hours_end, c.work_days, c.last_work_hours_check,
			c.skip_post_deployment, c.custom_post_config, c.post_deploy_status, c.preserve_on_failure, c.credentials_mode, c.custom_pull_secret,
			co.api_url, co.console_url
		FROM clusters c
		LEFT JOIN cluster_outputs co ON c.id = co.cluster_id
		WHERE 1=1
	`
	countQuery := "SELECT COUNT(*) FROM clusters WHERE 1=1"

	args := []interface{}{}
	argPos := 1

	if filters.Status != nil {
		query += fmt.Sprintf(" AND c.status = $%d", argPos)
		countQuery += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, *filters.Status)
		argPos++
	}

	if filters.Platform != nil {
		query += fmt.Sprintf(" AND c.platform = $%d", argPos)
		countQuery += fmt.Sprintf(" AND platform = $%d", argPos)
		args = append(args, *filters.Platform)
		argPos++
	}

	if filters.Owner != nil {
		query += fmt.Sprintf(" AND c.owner = $%d", argPos)
		countQuery += fmt.Sprintf(" AND owner = $%d", argPos)
		args = append(args, *filters.Owner)
		argPos++
	}

	if filters.OwnerID != nil {
		query += fmt.Sprintf(" AND c.owner_id = $%d", argPos)
		countQuery += fmt.Sprintf(" AND owner_id = $%d", argPos)
		args = append(args, *filters.OwnerID)
		argPos++
	}

	if filters.Team != nil {
		query += fmt.Sprintf(" AND c.team = $%d", argPos)
		countQuery += fmt.Sprintf(" AND team = $%d", argPos)
		args = append(args, *filters.Team)
		argPos++
	}

	if filters.Profile != nil {
		query += fmt.Sprintf(" AND c.profile = $%d", argPos)
		countQuery += fmt.Sprintf(" AND profile = $%d", argPos)
		args = append(args, *filters.Profile)
		argPos++
	}

	// Get total count
	var total int
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count clusters: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY c.created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, filters.Limit, filters.Offset)

	// Execute query
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query clusters: %w", err)
	}
	defer rows.Close()

	clusters := []*types.Cluster{}
	for rows.Next() {
		var cluster types.Cluster
		err := rows.Scan(
			&cluster.ID,
			&cluster.Name,
			&cluster.Platform,
			&cluster.ClusterType,
			&cluster.Version,
			&cluster.Profile,
			&cluster.Region,
			&cluster.BaseDomain,
			&cluster.Owner,
			&cluster.OwnerID,
			&cluster.Team,
			&cluster.CostCenter,
			&cluster.Status,
			&cluster.RequestedBy,
			&cluster.TTLHours,
			&cluster.DestroyAt,
			&cluster.CreatedAt,
			&cluster.UpdatedAt,
			&cluster.DestroyedAt,
			&cluster.RequestTags,
			&cluster.EffectiveTags,
			&cluster.SSHPublicKey,
			&cluster.OffhoursOptIn,
			&cluster.WorkHoursEnabled,
			&cluster.WorkHoursStart,
			&cluster.WorkHoursEnd,
			&cluster.WorkDays,
			&cluster.LastWorkHoursCheck,
			&cluster.SkipPostDeployment,
			&cluster.CustomPostConfig,
			&cluster.PostDeployStatus,
			&cluster.PreserveOnFailure,
			&cluster.CredentialsMode,
			&cluster.CustomPullSecret,
			&cluster.APIURL,
			&cluster.ConsoleURL,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan cluster: %w", err)
		}
		clusters = append(clusters, &cluster)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate clusters: %w", err)
	}

	return clusters, total, nil
}

// UpdateStatus updates a cluster's status and automatically updates the updated_at timestamp.
// Can be called with or without a transaction (tx can be nil for non-transactional updates).
// Returns ErrNotFound if the cluster does not exist.
func (s *ClusterStore) UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status types.ClusterStatus) error {
	query := `
		UPDATE clusters
		SET status = $1, updated_at = NOW()
		WHERE id = $2
	`

	var result pgconn.CommandTag
	var err error

	if tx != nil {
		result, err = tx.Exec(ctx, query, status, id)
	} else {
		result, err = s.pool.Exec(ctx, query, status, id)
	}

	if err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkDestroyed updates a cluster to DESTROYED status and sets the destroyed_at timestamp.
// This is typically called after successful cluster resource cleanup.
// Returns ErrNotFound if the cluster does not exist.
func (s *ClusterStore) MarkDestroyed(ctx context.Context, id string) error {
	query := `
		UPDATE clusters
		SET status = $1, destroyed_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`

	result, err := s.pool.Exec(ctx, query, types.ClusterStatusDestroyed, id)
	if err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateTTL updates a cluster's TTL (time-to-live) in hours and recalculates the destroy_at timestamp.
// The destroy_at timestamp is set to the current time plus ttlHours.
// Returns ErrNotFound if the cluster does not exist.
func (s *ClusterStore) UpdateTTL(ctx context.Context, id string, ttlHours int) error {
	query := `
		UPDATE clusters
		SET ttl_hours = $1, destroy_at = NOW() + ($1::text || ' hours')::interval, updated_at = NOW()
		WHERE id = $2
	`

	result, err := s.pool.Exec(ctx, query, ttlHours, id)
	if err != nil {
		return fmt.Errorf("update cluster TTL: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetExpiredClusters returns clusters past their TTL that should be destroyed.
// Only returns clusters with status READY or FAILED whose destroy_at timestamp has passed.
// Clusters are ordered by destroy_at in ascending order (oldest expiration first).
func (s *ClusterStore) GetExpiredClusters(ctx context.Context) ([]*types.Cluster, error) {
	query := `
		SELECT id, name, platform, cluster_type, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in,
			work_hours_enabled, work_hours_start, work_hours_end, work_days, last_work_hours_check,
			preserve_on_failure
		FROM clusters
		WHERE destroy_at <= NOW()
			AND status IN ('READY', 'FAILED')
		ORDER BY destroy_at ASC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query expired clusters: %w", err)
	}
	defer rows.Close()

	clusters := []*types.Cluster{}
	for rows.Next() {
		var cluster types.Cluster
		err := rows.Scan(
			&cluster.ID,
			&cluster.Name,
			&cluster.Platform,
			&cluster.ClusterType,
			&cluster.Version,
			&cluster.Profile,
			&cluster.Region,
			&cluster.BaseDomain,
			&cluster.Owner,
			&cluster.OwnerID,
			&cluster.Team,
			&cluster.CostCenter,
			&cluster.Status,
			&cluster.RequestedBy,
			&cluster.TTLHours,
			&cluster.DestroyAt,
			&cluster.CreatedAt,
			&cluster.UpdatedAt,
			&cluster.DestroyedAt,
			&cluster.RequestTags,
			&cluster.EffectiveTags,
			&cluster.SSHPublicKey,
			&cluster.OffhoursOptIn,
			&cluster.WorkHoursEnabled,
			&cluster.WorkHoursStart,
			&cluster.WorkHoursEnd,
			&cluster.WorkDays,
			&cluster.LastWorkHoursCheck,
			&cluster.PreserveOnFailure,
		)
		if err != nil {
			return nil, fmt.Errorf("scan expired cluster: %w", err)
		}
		clusters = append(clusters, &cluster)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired clusters: %w", err)
	}

	return clusters, nil
}

// ListAll retrieves all active clusters (excludes DESTROYED status) for orphan resource detection.
// IMPORTANT: This query has a safety limit of 100,000 clusters to prevent memory issues.
// In production environments with >100k active clusters, consider:
// 1. Implementing pagination for janitor operations
// 2. Archiving DESTROYED clusters to a separate table
// 3. Using a streaming approach for resource detection
// ListAll returns all non-destroyed clusters.
// DEPRECATED: For large cluster counts (100k+), use ListAllStreaming to prevent OOM.
// This method loads all clusters into memory at once and may cause out-of-memory errors.
func (s *ClusterStore) ListAll(ctx context.Context) ([]*types.Cluster, error) {
	query := `
		SELECT id, name, platform, cluster_type, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in,
			work_hours_enabled, work_hours_start, work_hours_end, work_days, last_work_hours_check,
			preserve_on_failure
		FROM clusters
		WHERE status != 'DESTROYED'
		ORDER BY created_at DESC
		LIMIT 100000
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query all clusters: %w", err)
	}
	defer rows.Close()

	clusters := []*types.Cluster{}
	for rows.Next() {
		var cluster types.Cluster
		err := rows.Scan(
			&cluster.ID,
			&cluster.Name,
			&cluster.Platform,
			&cluster.ClusterType,
			&cluster.Version,
			&cluster.Profile,
			&cluster.Region,
			&cluster.BaseDomain,
			&cluster.Owner,
			&cluster.OwnerID,
			&cluster.Team,
			&cluster.CostCenter,
			&cluster.Status,
			&cluster.RequestedBy,
			&cluster.TTLHours,
			&cluster.DestroyAt,
			&cluster.CreatedAt,
			&cluster.UpdatedAt,
			&cluster.DestroyedAt,
			&cluster.RequestTags,
			&cluster.EffectiveTags,
			&cluster.SSHPublicKey,
			&cluster.OffhoursOptIn,
			&cluster.WorkHoursEnabled,
			&cluster.WorkHoursStart,
			&cluster.WorkHoursEnd,
			&cluster.WorkDays,
			&cluster.LastWorkHoursCheck,
			&cluster.PreserveOnFailure,
		)
		if err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		clusters = append(clusters, &cluster)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clusters: %w", err)
	}

	return clusters, nil
}

// ListAllStreaming returns all non-destroyed clusters in batches via channels to prevent OOM.
// This method is memory-efficient for large cluster counts (100k+) and is used by the janitor.
// The batchSize parameter controls how many clusters are fetched and returned per batch.
// Returns two channels: one for cluster batches, one for errors.
// The channels are closed when iteration completes or an error occurs.
//
// Example usage:
//
//	clustersCh, errCh := store.Clusters.ListAllStreaming(ctx, 1000)
//	for {
//	    select {
//	    case batch, ok := <-clustersCh:
//	        if !ok {
//	            // All batches received
//	            goto Done
//	        }
//	        // Process batch...
//	    case err := <-errCh:
//	        if err != nil {
//	            return err
//	        }
//	    case <-ctx.Done():
//	        return ctx.Err()
//	    }
//	}
//	Done:
//	// Check for final error
//	if err := <-errCh; err != nil {
//	    return err
//	}
func (s *ClusterStore) ListAllStreaming(ctx context.Context, batchSize int) (<-chan []*types.Cluster, <-chan error) {
	clustersCh := make(chan []*types.Cluster)
	errCh := make(chan error, 1)

	go func() {
		defer close(clustersCh)
		defer close(errCh)

		offset := 0
		for {
			// Fetch a batch of clusters using the existing List method
			clusters, _, err := s.List(ctx, ListFilters{
				Limit:  batchSize,
				Offset: offset,
			})
			if err != nil {
				errCh <- err
				return
			}

			// No more clusters to fetch
			if len(clusters) == 0 {
				return
			}

			// Send batch to channel
			select {
			case clustersCh <- clusters:
				// Batch sent successfully
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}

			offset += batchSize
		}
	}()

	return clustersCh, errCh
}

// UpdateLastWorkHoursCheck updates the last_work_hours_check timestamp to the current time.
// This is used to track when work hours enforcement was last checked for a cluster.
func (s *ClusterStore) UpdateLastWorkHoursCheck(ctx context.Context, clusterID string) error {
	query := `UPDATE clusters SET last_work_hours_check = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, clusterID)
	return err
}

// SetLastWorkHoursCheck sets the last_work_hours_check timestamp to a specific time
// This is used to set a grace period after manual resume during hibernate hours
func (s *ClusterStore) SetLastWorkHoursCheck(ctx context.Context, clusterID string, checkTime time.Time) error {
	query := `UPDATE clusters SET last_work_hours_check = $1 WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, checkTime, clusterID)
	return err
}

// CheckNameExists checks if a cluster name already exists for the given platform and base domain combination.
// Only considers clusters that are not DESTROYED or FAILED. Returns true if a matching cluster exists.
func (s *ClusterStore) CheckNameExists(ctx context.Context, name string, platform types.Platform, baseDomain string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM clusters
			WHERE name = $1
				AND platform = $2
				AND base_domain = $3
				AND status NOT IN ('DESTROYED', 'FAILED')
		)
	`

	var exists bool
	err := s.pool.QueryRow(ctx, query, name, platform, baseDomain).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check cluster name exists: %w", err)
	}

	return exists, nil
}

// DeleteDestroyedClusters deletes DESTROYED clusters older than the specified time
// This is used by the janitor to cleanup old cluster records from the database
// Returns the number of clusters deleted
//
// Safety validations:
// - olderThan must not be zero
// - olderThan must be at least 24 hours in the past
// - Only DESTROYED status clusters with destroyed_at set are deleted
// - All deletions are logged for audit trail
func (s *ClusterStore) DeleteDestroyedClusters(ctx context.Context, olderThan time.Time) (int, error) {
	// Validate inputs to prevent accidental data loss
	if olderThan.IsZero() {
		return 0, fmt.Errorf("olderThan time cannot be zero (safety check)")
	}

	// Prevent deleting recent data (safety check)
	minOldTime := time.Now().Add(-24 * time.Hour)
	if olderThan.After(minOldTime) {
		return 0, fmt.Errorf("olderThan must be at least 24 hours in the past (got: %s, minimum: %s)",
			olderThan.Format(time.RFC3339), minOldTime.Format(time.RFC3339))
	}

	query := `
		DELETE FROM clusters
		WHERE status = $1
			AND destroyed_at IS NOT NULL
			AND destroyed_at < $2
	`

	result, err := s.pool.Exec(ctx, query, types.ClusterStatusDestroyed, olderThan)
	if err != nil {
		return 0, fmt.Errorf("delete destroyed clusters: %w", err)
	}

	deleted := int(result.RowsAffected())

	// Log deletions for audit trail
	if deleted > 0 {
		log.Printf("Deleted %d destroyed clusters older than %s", deleted, olderThan.Format(time.RFC3339))
	}

	return deleted, nil
}

// GetClustersForWorkHoursEnforcement returns clusters that need work hours enforcement
// Returns clusters where:
// - work_hours_enabled = TRUE (cluster-level override), OR
// - work_hours_enabled IS NULL AND user has work_hours_enabled = TRUE (use user default)
// - AND status IN ('READY', 'HIBERNATED')
// - AND last_work_hours_check is NULL or <= NOW() (skips clusters in grace period)
// Ordered by last_work_hours_check ASC NULLS FIRST for efficient processing
func (s *ClusterStore) GetClustersForWorkHoursEnforcement(ctx context.Context) ([]*types.Cluster, error) {
	query := `
		SELECT DISTINCT c.id, c.name, c.platform, c.cluster_type, c.version, c.profile, c.region, c.base_domain,
			c.owner, c.owner_id, c.team, c.cost_center, c.status, c.requested_by, c.ttl_hours,
			c.destroy_at, c.created_at, c.updated_at, c.destroyed_at,
			c.request_tags, c.effective_tags, c.ssh_public_key, c.offhours_opt_in,
			c.work_hours_enabled, c.work_hours_start, c.work_hours_end, c.work_days, c.last_work_hours_check
		FROM clusters c
		JOIN users u ON c.owner_id = u.id
		WHERE c.status IN ('READY', 'HIBERNATED')
			AND (c.work_hours_enabled = TRUE OR (c.work_hours_enabled IS NULL AND u.work_hours_enabled = TRUE))
			AND (c.last_work_hours_check IS NULL OR c.last_work_hours_check <= NOW())
		ORDER BY c.last_work_hours_check ASC NULLS FIRST
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query clusters for work hours enforcement: %w", err)
	}
	defer rows.Close()

	clusters := []*types.Cluster{}
	for rows.Next() {
		var cluster types.Cluster
		err := rows.Scan(
			&cluster.ID,
			&cluster.Name,
			&cluster.Platform,
			&cluster.ClusterType,
			&cluster.Version,
			&cluster.Profile,
			&cluster.Region,
			&cluster.BaseDomain,
			&cluster.Owner,
			&cluster.OwnerID,
			&cluster.Team,
			&cluster.CostCenter,
			&cluster.Status,
			&cluster.RequestedBy,
			&cluster.TTLHours,
			&cluster.DestroyAt,
			&cluster.CreatedAt,
			&cluster.UpdatedAt,
			&cluster.DestroyedAt,
			&cluster.RequestTags,
			&cluster.EffectiveTags,
			&cluster.SSHPublicKey,
			&cluster.OffhoursOptIn,
			&cluster.WorkHoursEnabled,
			&cluster.WorkHoursStart,
			&cluster.WorkHoursEnd,
			&cluster.WorkDays,
			&cluster.LastWorkHoursCheck,
		)
		if err != nil {
			return nil, fmt.Errorf("scan cluster for work hours enforcement: %w", err)
		}
		clusters = append(clusters, &cluster)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clusters for work hours enforcement: %w", err)
	}

	return clusters, nil
}

// UpdatePostDeployStatus updates the cluster's post-deployment status.
// When status is "completed", automatically sets the post_deploy_completed_at timestamp.
func (s *ClusterStore) UpdatePostDeployStatus(ctx context.Context, clusterID, status string) error {
	query := `
		UPDATE clusters
		SET post_deploy_status = $1,
		    post_deploy_completed_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE NULL END,
		    updated_at = NOW()
		WHERE id = $3
	`

	_, err := s.pool.Exec(ctx, query, status, status, clusterID)
	if err != nil {
		return fmt.Errorf("update post-deploy status: %w", err)
	}

	return nil
}

// ClusterStatsByStatus represents cluster count and total by status
type ClusterStatsByStatus struct {
	Status string
	Count  int
}

// ClusterStatsByProfile represents cluster count by profile with owner info for cost calculation
type ClusterStatsByProfile struct {
	Profile  string
	Status   string
	OwnerID  string
	Count    int
}

// ClusterStatsByPlatform represents cluster count by platform
type ClusterStatsByPlatform struct {
	Platform string
	Count    int
}

// LongRunningCluster represents a cluster with running duration information
// Used for admin dashboard to identify clusters that may need hibernation
type LongRunningCluster struct {
	*types.Cluster
	RunningDurationHours float64    `json:"running_duration_hours"`
	LastHibernatedAt     *time.Time `json:"last_hibernated_at,omitempty"`
}

// GetStatisticsByStatus returns aggregated cluster counts by status
// Excludes DESTROYED and FAILED clusters by default
func (s *ClusterStore) GetStatisticsByStatus(ctx context.Context) ([]*ClusterStatsByStatus, error) {
	query := `
		SELECT status, COUNT(*) as count
		FROM clusters
		WHERE status NOT IN ('DESTROYED', 'FAILED')
		GROUP BY status
		ORDER BY count DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query statistics by status: %w", err)
	}
	defer rows.Close()

	stats := []*ClusterStatsByStatus{}
	for rows.Next() {
		var stat ClusterStatsByStatus
		err := rows.Scan(&stat.Status, &stat.Count)
		if err != nil {
			return nil, fmt.Errorf("scan statistics: %w", err)
		}
		stats = append(stats, &stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate statistics: %w", err)
	}

	return stats, nil
}

// GetStatisticsByProfile returns aggregated cluster counts by profile and status
// Returns profile, status, owner_id, and count for cost calculation
func (s *ClusterStore) GetStatisticsByProfile(ctx context.Context) ([]*ClusterStatsByProfile, error) {
	query := `
		SELECT profile, status, owner_id, COUNT(*) as count
		FROM clusters
		WHERE status NOT IN ('DESTROYED', 'FAILED')
		GROUP BY profile, status, owner_id
		ORDER BY profile, status, owner_id
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query statistics by profile: %w", err)
	}
	defer rows.Close()

	stats := []*ClusterStatsByProfile{}
	for rows.Next() {
		var stat ClusterStatsByProfile
		err := rows.Scan(&stat.Profile, &stat.Status, &stat.OwnerID, &stat.Count)
		if err != nil {
			return nil, fmt.Errorf("scan statistics: %w", err)
		}
		stats = append(stats, &stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate statistics: %w", err)
	}

	return stats, nil
}

// GetStatisticsByPlatform returns aggregated cluster counts grouped by platform
func (s *ClusterStore) GetStatisticsByPlatform(ctx context.Context) ([]*ClusterStatsByPlatform, error) {
	query := `
		SELECT platform, COUNT(*) as count
		FROM clusters
		WHERE status NOT IN ('DESTROYED', 'FAILED')
		GROUP BY platform
		ORDER BY count DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query statistics by platform: %w", err)
	}
	defer rows.Close()

	stats := []*ClusterStatsByPlatform{}
	for rows.Next() {
		var stat ClusterStatsByPlatform
		err := rows.Scan(&stat.Platform, &stat.Count)
		if err != nil {
			return nil, fmt.Errorf("scan statistics: %w", err)
		}
		stats = append(stats, &stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate statistics: %w", err)
	}

	return stats, nil
}

// GetTotalClusterCount returns total count of all clusters (including DESTROYED/FAILED)
func (s *ClusterStore) GetTotalClusterCount(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM clusters`

	var count int
	err := s.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query total cluster count: %w", err)
	}

	return count, nil
}

// GetActiveClusterCount returns count of active clusters (not DESTROYED/FAILED)
func (s *ClusterStore) GetActiveClusterCount(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM clusters WHERE status NOT IN ('DESTROYED', 'FAILED')`

	var count int
	err := s.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query active cluster count: %w", err)
	}

	return count, nil
}

// GetLongRunningClusters returns READY clusters running for specified hours without hibernation
// Excludes clusters that have successfully hibernated in the last minHours
// Orders by running duration DESC (longest running first)
func (s *ClusterStore) GetLongRunningClusters(ctx context.Context, minHours int) ([]*LongRunningCluster, error) {
	query := `
		SELECT
			c.id, c.name, c.platform, c.cluster_type, c.version, c.profile,
			c.region, c.base_domain, c.owner, c.owner_id, c.team, c.cost_center,
			c.status, c.requested_by, c.ttl_hours, c.destroy_at, c.created_at,
			c.updated_at, c.destroyed_at, c.request_tags, c.effective_tags,
			c.ssh_public_key, c.offhours_opt_in, c.work_hours_enabled,
			c.work_hours_start, c.work_hours_end, c.work_days, c.last_work_hours_check,
			c.skip_post_deployment, c.custom_post_config, c.post_deploy_status,
			c.preserve_on_failure, c.credentials_mode, c.custom_pull_secret,
			EXTRACT(EPOCH FROM (NOW() - c.updated_at)) / 3600 as running_duration_hours,
			(
				SELECT MAX(j.ended_at)
				FROM jobs j
				WHERE j.cluster_id = c.id
				  AND j.job_type = 'HIBERNATE'
				  AND j.status = 'SUCCEEDED'
			) as last_hibernated_at
		FROM clusters c
		WHERE c.status = 'READY'
		  AND c.updated_at <= NOW() - ($1 * INTERVAL '1 hour')
		  AND c.id NOT IN (
			  SELECT DISTINCT cluster_id
			  FROM jobs
			  WHERE job_type = 'HIBERNATE'
				AND status = 'SUCCEEDED'
				AND ended_at >= NOW() - ($1 * INTERVAL '1 hour')
		  )
		ORDER BY running_duration_hours DESC
	`

	rows, err := s.pool.Query(ctx, query, minHours)
	if err != nil {
		return nil, fmt.Errorf("query long-running clusters: %w", err)
	}
	defer rows.Close()

	clusters := []*LongRunningCluster{}
	for rows.Next() {
		lrc := &LongRunningCluster{
			Cluster: &types.Cluster{},
		}

		err := rows.Scan(
			&lrc.Cluster.ID,
			&lrc.Cluster.Name,
			&lrc.Cluster.Platform,
			&lrc.Cluster.ClusterType,
			&lrc.Cluster.Version,
			&lrc.Cluster.Profile,
			&lrc.Cluster.Region,
			&lrc.Cluster.BaseDomain,
			&lrc.Cluster.Owner,
			&lrc.Cluster.OwnerID,
			&lrc.Cluster.Team,
			&lrc.Cluster.CostCenter,
			&lrc.Cluster.Status,
			&lrc.Cluster.RequestedBy,
			&lrc.Cluster.TTLHours,
			&lrc.Cluster.DestroyAt,
			&lrc.Cluster.CreatedAt,
			&lrc.Cluster.UpdatedAt,
			&lrc.Cluster.DestroyedAt,
			&lrc.Cluster.RequestTags,
			&lrc.Cluster.EffectiveTags,
			&lrc.Cluster.SSHPublicKey,
			&lrc.Cluster.OffhoursOptIn,
			&lrc.Cluster.WorkHoursEnabled,
			&lrc.Cluster.WorkHoursStart,
			&lrc.Cluster.WorkHoursEnd,
			&lrc.Cluster.WorkDays,
			&lrc.Cluster.LastWorkHoursCheck,
			&lrc.Cluster.SkipPostDeployment,
			&lrc.Cluster.CustomPostConfig,
			&lrc.Cluster.PostDeployStatus,
			&lrc.Cluster.PreserveOnFailure,
			&lrc.Cluster.CredentialsMode,
			&lrc.Cluster.CustomPullSecret,
			&lrc.RunningDurationHours,
			&lrc.LastHibernatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan long-running cluster: %w", err)
		}

		clusters = append(clusters, lrc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate long-running clusters: %w", err)
	}

	return clusters, nil
}

// CheckInfraIDCollision checks if a cluster name could collide with recently destroyed clusters
// Returns a list of recently destroyed cluster names with similar prefixes (within last 7 days)
func (s *ClusterStore) CheckInfraIDCollision(ctx context.Context, clusterName string, platform types.Platform) ([]string, error) {
	// OpenShift truncates cluster names to 27 chars for AWS (32 char limit - 5 char suffix)
	const maxInfraPrefix = 27
	infraPrefix := clusterName
	if len(clusterName) > maxInfraPrefix {
		infraPrefix = clusterName[:maxInfraPrefix]
	}

	// Query for recently destroyed clusters with similar names (within last 7 days)
	query := `
		SELECT name
		FROM clusters
		WHERE platform = $1
		  AND status = 'DESTROYED'
		  AND destroyed_at > NOW() - INTERVAL '7 days'
		  AND name LIKE $2
		ORDER BY destroyed_at DESC
		LIMIT 5
	`

	rows, err := s.pool.Query(ctx, query, platform, infraPrefix+"%")
	if err != nil {
		return nil, fmt.Errorf("query clusters for infra ID collision: %w", err)
	}
	defer rows.Close()

	var recentClusters []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan cluster name: %w", err)
		}
		recentClusters = append(recentClusters, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clusters: %w", err)
	}

	return recentClusters, nil
}
