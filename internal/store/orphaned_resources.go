package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// OrphanedResourceStore handles database operations for orphaned resources
type OrphanedResourceStore struct {
	pool *pgxpool.Pool
}

// NewOrphanedResourceStore creates a new orphaned resource store
func NewOrphanedResourceStore(pool *pgxpool.Pool) *OrphanedResourceStore {
	return &OrphanedResourceStore{pool: pool}
}

// Upsert creates or updates an orphaned resource record
// If the resource already exists (same type, ID, region), it updates the last_detected_at and increments detection_count
func (s *OrphanedResourceStore) Upsert(ctx context.Context, resource *types.OrphanedResource) error {
	// Generate ID if not provided
	if resource.ID == "" {
		resource.ID = uuid.New().String()
	}

	query := `
		INSERT INTO orphaned_resources (
			id, resource_type, resource_id, resource_name, region, cluster_name, tags,
			first_detected_at, last_detected_at, detection_count, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), 1, 'ACTIVE'
		)
		ON CONFLICT (resource_type, resource_id, region) DO UPDATE SET
			last_detected_at = NOW(),
			detection_count = orphaned_resources.detection_count + 1,
			updated_at = NOW(),
			-- Update name and tags if provided
			resource_name = COALESCE(EXCLUDED.resource_name, orphaned_resources.resource_name),
			tags = COALESCE(EXCLUDED.tags, orphaned_resources.tags)
		RETURNING id
	`

	err := s.pool.QueryRow(ctx, query,
		resource.ID,
		resource.ResourceType,
		resource.ResourceID,
		resource.ResourceName,
		resource.Region,
		resource.ClusterName,
		resource.Tags,
	).Scan(&resource.ID)

	if err != nil {
		return fmt.Errorf("upsert orphaned resource: %w", err)
	}

	return nil
}

// OrphanedResourceFilters represents filters for listing orphaned resources
type OrphanedResourceFilters struct {
	Status       *types.OrphanedResourceStatus
	ResourceType *types.OrphanedResourceType
	Region       *string
	Limit        int
	Offset       int
}

// List retrieves orphaned resources with optional filters
func (s *OrphanedResourceStore) List(ctx context.Context, filters OrphanedResourceFilters) ([]*types.OrphanedResource, int, error) {
	// Build query with filters
	query := `
		SELECT id, resource_type, resource_id, resource_name, region, cluster_name, tags,
			first_detected_at, last_detected_at, detection_count, status,
			resolved_at, resolved_by, notes, created_at, updated_at
		FROM orphaned_resources
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM orphaned_resources WHERE 1=1`

	args := []interface{}{}
	argPos := 1

	// Apply filters
	if filters.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		countQuery += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, *filters.Status)
		argPos++
	}

	if filters.ResourceType != nil {
		query += fmt.Sprintf(" AND resource_type = $%d", argPos)
		countQuery += fmt.Sprintf(" AND resource_type = $%d", argPos)
		args = append(args, *filters.ResourceType)
		argPos++
	}

	if filters.Region != nil {
		query += fmt.Sprintf(" AND region = $%d", argPos)
		countQuery += fmt.Sprintf(" AND region = $%d", argPos)
		args = append(args, *filters.Region)
		argPos++
	}

	// Get total count
	var total int
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count orphaned resources: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY last_detected_at DESC"

	if filters.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, filters.Limit)
		argPos++
	}

	if filters.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, filters.Offset)
		argPos++
	}

	// Execute query
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query orphaned resources: %w", err)
	}
	defer rows.Close()

	resources := []*types.OrphanedResource{}
	for rows.Next() {
		var r types.OrphanedResource
		err := rows.Scan(
			&r.ID,
			&r.ResourceType,
			&r.ResourceID,
			&r.ResourceName,
			&r.Region,
			&r.ClusterName,
			&r.Tags,
			&r.FirstDetectedAt,
			&r.LastDetectedAt,
			&r.DetectionCount,
			&r.Status,
			&r.ResolvedAt,
			&r.ResolvedBy,
			&r.Notes,
			&r.CreatedAt,
			&r.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan orphaned resource: %w", err)
		}
		resources = append(resources, &r)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate rows: %w", err)
	}

	return resources, total, nil
}

// GetByID retrieves an orphaned resource by ID
func (s *OrphanedResourceStore) GetByID(ctx context.Context, id string) (*types.OrphanedResource, error) {
	query := `
		SELECT id, resource_type, resource_id, resource_name, region, cluster_name, tags,
			first_detected_at, last_detected_at, detection_count, status,
			resolved_at, resolved_by, notes, created_at, updated_at
		FROM orphaned_resources
		WHERE id = $1
	`

	var r types.OrphanedResource
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&r.ID,
		&r.ResourceType,
		&r.ResourceID,
		&r.ResourceName,
		&r.Region,
		&r.ClusterName,
		&r.Tags,
		&r.FirstDetectedAt,
		&r.LastDetectedAt,
		&r.DetectionCount,
		&r.Status,
		&r.ResolvedAt,
		&r.ResolvedBy,
		&r.Notes,
		&r.CreatedAt,
		&r.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("orphaned resource not found: %s", id)
	}

	if err != nil {
		return nil, fmt.Errorf("query orphaned resource: %w", err)
	}

	return &r, nil
}

// MarkResolved marks an orphaned resource as resolved
func (s *OrphanedResourceStore) MarkResolved(ctx context.Context, id string, resolvedBy string, notes string) error {
	query := `
		UPDATE orphaned_resources
		SET status = 'RESOLVED',
			resolved_at = NOW(),
			resolved_by = $2,
			notes = $3,
			updated_at = NOW()
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query, id, resolvedBy, notes)
	if err != nil {
		return fmt.Errorf("update orphaned resource: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("orphaned resource not found: %s", id)
	}

	return nil
}

// MarkIgnored marks an orphaned resource as ignored (false positive or intentional)
func (s *OrphanedResourceStore) MarkIgnored(ctx context.Context, id string, notes string) error {
	query := `
		UPDATE orphaned_resources
		SET status = 'IGNORED',
			notes = $2,
			updated_at = NOW()
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query, id, notes)
	if err != nil {
		return fmt.Errorf("update orphaned resource: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("orphaned resource not found: %s", id)
	}

	return nil
}

// GetStats returns summary statistics for orphaned resources
func (s *OrphanedResourceStore) GetStats(ctx context.Context) (*types.OrphanedResourceStats, error) {
	stats := &types.OrphanedResourceStats{
		ByType:   make(map[string]int),
		ByRegion: make(map[string]int),
	}

	// Get counts by status
	statusQuery := `
		SELECT
			COUNT(*) FILTER (WHERE status = 'ACTIVE') as active,
			COUNT(*) FILTER (WHERE status = 'RESOLVED') as resolved,
			COUNT(*) FILTER (WHERE status = 'IGNORED') as ignored
		FROM orphaned_resources
	`

	err := s.pool.QueryRow(ctx, statusQuery).Scan(
		&stats.TotalActive,
		&stats.TotalResolved,
		&stats.TotalIgnored,
	)
	if err != nil {
		return nil, fmt.Errorf("query status counts: %w", err)
	}

	// Get counts by type (for active resources)
	typeQuery := `
		SELECT resource_type, COUNT(*)
		FROM orphaned_resources
		WHERE status = 'ACTIVE'
		GROUP BY resource_type
	`

	rows, err := s.pool.Query(ctx, typeQuery)
	if err != nil {
		return nil, fmt.Errorf("query type counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var resourceType string
		var count int
		if err := rows.Scan(&resourceType, &count); err != nil {
			return nil, fmt.Errorf("scan type count: %w", err)
		}
		stats.ByType[resourceType] = count
	}

	// Get counts by region (for active resources)
	regionQuery := `
		SELECT region, COUNT(*)
		FROM orphaned_resources
		WHERE status = 'ACTIVE'
		GROUP BY region
	`

	rows, err = s.pool.Query(ctx, regionQuery)
	if err != nil {
		return nil, fmt.Errorf("query region counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var region string
		var count int
		if err := rows.Scan(&region, &count); err != nil {
			return nil, fmt.Errorf("scan region count: %w", err)
		}
		stats.ByRegion[region] = count
	}

	// Get oldest detection
	oldestQuery := `
		SELECT MIN(first_detected_at)
		FROM orphaned_resources
		WHERE status = 'ACTIVE'
	`

	var oldest *time.Time
	err = s.pool.QueryRow(ctx, oldestQuery).Scan(&oldest)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("query oldest detection: %w", err)
	}
	stats.OldestDetected = oldest

	return stats, nil
}
