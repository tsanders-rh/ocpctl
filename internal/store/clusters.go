package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterStore handles cluster database operations
type ClusterStore struct {
	pool *pgxpool.Pool
}

// Create inserts a new cluster record
func (s *ClusterStore) Create(ctx context.Context, cluster *types.Cluster) error {
	query := `
		INSERT INTO clusters (
			id, name, platform, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, request_tags, effective_tags, ssh_public_key,
			offhours_opt_in
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $19
		)
	`

	_, err := s.pool.Exec(ctx, query,
		cluster.ID,
		cluster.Name,
		cluster.Platform,
		cluster.Version,
		cluster.Profile,
		cluster.Region,
		cluster.BaseDomain,
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
	)

	if err != nil {
		return fmt.Errorf("insert cluster: %w", err)
	}

	return nil
}

// GetByID retrieves a cluster by ID
func (s *ClusterStore) GetByID(ctx context.Context, id string) (*types.Cluster, error) {
	query := `
		SELECT id, name, platform, version, profile, region, base_domain,
			owner, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in
		FROM clusters
		WHERE id = $1
	`

	var cluster types.Cluster
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&cluster.ID,
		&cluster.Name,
		&cluster.Platform,
		&cluster.Version,
		&cluster.Profile,
		&cluster.Region,
		&cluster.BaseDomain,
		&cluster.Owner,
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
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query cluster: %w", err)
	}

	return &cluster, nil
}

// GetByIDForUpdate retrieves a cluster by ID with row lock for update
func (s *ClusterStore) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*types.Cluster, error) {
	query := `
		SELECT id, name, platform, version, profile, region, base_domain,
			owner, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in
		FROM clusters
		WHERE id = $1
		FOR UPDATE
	`

	var cluster types.Cluster
	err := tx.QueryRow(ctx, query, id).Scan(
		&cluster.ID,
		&cluster.Name,
		&cluster.Platform,
		&cluster.Version,
		&cluster.Profile,
		&cluster.Region,
		&cluster.BaseDomain,
		&cluster.Owner,
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

// List retrieves clusters with optional filtering
func (s *ClusterStore) List(ctx context.Context, filters ListFilters) ([]*types.Cluster, int, error) {
	// Build query dynamically based on filters
	query := `
		SELECT id, name, platform, version, profile, region, base_domain,
			owner, owner_id, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in
		FROM clusters
		WHERE 1=1
	`
	countQuery := "SELECT COUNT(*) FROM clusters WHERE 1=1"

	args := []interface{}{}
	argPos := 1

	if filters.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		countQuery += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, *filters.Status)
		argPos++
	}

	if filters.Platform != nil {
		query += fmt.Sprintf(" AND platform = $%d", argPos)
		countQuery += fmt.Sprintf(" AND platform = $%d", argPos)
		args = append(args, *filters.Platform)
		argPos++
	}

	if filters.Owner != nil {
		query += fmt.Sprintf(" AND owner = $%d", argPos)
		countQuery += fmt.Sprintf(" AND owner = $%d", argPos)
		args = append(args, *filters.Owner)
		argPos++
	}

	if filters.OwnerID != nil {
		query += fmt.Sprintf(" AND owner_id = $%d", argPos)
		countQuery += fmt.Sprintf(" AND owner_id = $%d", argPos)
		args = append(args, *filters.OwnerID)
		argPos++
	}

	if filters.Team != nil {
		query += fmt.Sprintf(" AND team = $%d", argPos)
		countQuery += fmt.Sprintf(" AND team = $%d", argPos)
		args = append(args, *filters.Team)
		argPos++
	}

	if filters.Profile != nil {
		query += fmt.Sprintf(" AND profile = $%d", argPos)
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
	query += " ORDER BY created_at DESC"
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
			&cluster.Version,
			&cluster.Profile,
			&cluster.Region,
			&cluster.BaseDomain,
			&cluster.Owner,
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

// UpdateStatus updates a cluster's status and updated_at timestamp
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

// MarkDestroyed updates a cluster to DESTROYED status and sets destroyed_at
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

// UpdateTTL updates a cluster's TTL and destroy_at timestamp
func (s *ClusterStore) UpdateTTL(ctx context.Context, id string, ttlHours int) error {
	query := `
		UPDATE clusters
		SET ttl_hours = $1, destroy_at = NOW() + ($1 || ' hours')::interval, updated_at = NOW()
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

// GetExpiredClusters returns clusters past their TTL that should be destroyed
func (s *ClusterStore) GetExpiredClusters(ctx context.Context) ([]*types.Cluster, error) {
	query := `
		SELECT id, name, platform, version, profile, region, base_domain,
			owner, team, cost_center, status, requested_by, ttl_hours,
			destroy_at, created_at, updated_at, destroyed_at,
			request_tags, effective_tags, ssh_public_key, offhours_opt_in
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
			&cluster.Version,
			&cluster.Profile,
			&cluster.Region,
			&cluster.BaseDomain,
			&cluster.Owner,
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

// CheckNameExists checks if a cluster name already exists for the platform/domain
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
