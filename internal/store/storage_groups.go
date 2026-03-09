package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// StorageGroupStore handles database operations for storage groups
type StorageGroupStore struct {
	pool *pgxpool.Pool
}

// NewStorageGroupStore creates a new storage group store
func NewStorageGroupStore(pool *pgxpool.Pool) *StorageGroupStore {
	return &StorageGroupStore{pool: pool}
}

// Create inserts a new storage group record
func (s *StorageGroupStore) Create(ctx context.Context, group *types.StorageGroup) error {
	query := `
		INSERT INTO storage_groups (
			id, name, efs_id, efs_security_group_id, s3_bucket, region, status, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err := s.pool.Exec(ctx, query,
		group.ID,
		group.Name,
		group.EFSID,
		group.EFSSecurityGroupID,
		group.S3Bucket,
		group.Region,
		group.Status,
		group.Metadata,
	)

	if err != nil {
		return fmt.Errorf("insert storage group: %w", err)
	}

	return nil
}

// GetByID retrieves a storage group by ID
func (s *StorageGroupStore) GetByID(ctx context.Context, id string) (*types.StorageGroup, error) {
	query := `
		SELECT id, name, efs_id, efs_security_group_id, s3_bucket, region,
			status, metadata, created_at, updated_at
		FROM storage_groups
		WHERE id = $1
	`

	var group types.StorageGroup
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&group.ID,
		&group.Name,
		&group.EFSID,
		&group.EFSSecurityGroupID,
		&group.S3Bucket,
		&group.Region,
		&group.Status,
		&group.Metadata,
		&group.CreatedAt,
		&group.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("query storage group: %w", err)
	}

	return &group, nil
}

// GetByName retrieves a storage group by name
func (s *StorageGroupStore) GetByName(ctx context.Context, name string) (*types.StorageGroup, error) {
	query := `
		SELECT id, name, efs_id, efs_security_group_id, s3_bucket, region,
			status, metadata, created_at, updated_at
		FROM storage_groups
		WHERE name = $1
	`

	var group types.StorageGroup
	err := s.pool.QueryRow(ctx, query, name).Scan(
		&group.ID,
		&group.Name,
		&group.EFSID,
		&group.EFSSecurityGroupID,
		&group.S3Bucket,
		&group.Region,
		&group.Status,
		&group.Metadata,
		&group.CreatedAt,
		&group.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("query storage group by name: %w", err)
	}

	return &group, nil
}

// List retrieves all storage groups, optionally filtered by region
func (s *StorageGroupStore) List(ctx context.Context, region *string) ([]*types.StorageGroup, error) {
	query := `
		SELECT id, name, efs_id, efs_security_group_id, s3_bucket, region,
			status, metadata, created_at, updated_at
		FROM storage_groups
		WHERE ($1::text IS NULL OR region = $1)
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, region)
	if err != nil {
		return nil, fmt.Errorf("query storage groups: %w", err)
	}
	defer rows.Close()

	groups := []*types.StorageGroup{}
	for rows.Next() {
		var group types.StorageGroup
		err := rows.Scan(
			&group.ID,
			&group.Name,
			&group.EFSID,
			&group.EFSSecurityGroupID,
			&group.S3Bucket,
			&group.Region,
			&group.Status,
			&group.Metadata,
			&group.CreatedAt,
			&group.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan storage group: %w", err)
		}
		groups = append(groups, &group)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate storage groups: %w", err)
	}

	return groups, nil
}

// Update updates a storage group's fields
func (s *StorageGroupStore) Update(ctx context.Context, group *types.StorageGroup) error {
	query := `
		UPDATE storage_groups
		SET name = $2,
			efs_id = $3,
			efs_security_group_id = $4,
			s3_bucket = $5,
			region = $6,
			status = $7,
			metadata = $8,
			updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.pool.Exec(ctx, query,
		group.ID,
		group.Name,
		group.EFSID,
		group.EFSSecurityGroupID,
		group.S3Bucket,
		group.Region,
		group.Status,
		group.Metadata,
	)

	if err != nil {
		return fmt.Errorf("update storage group: %w", err)
	}

	return nil
}

// UpdateStatus updates a storage group's status
func (s *StorageGroupStore) UpdateStatus(ctx context.Context, id string, status types.StorageGroupStatus) error {
	query := `
		UPDATE storage_groups
		SET status = $2, updated_at = NOW()
		WHERE id = $1
	`

	_, err := s.pool.Exec(ctx, query, id, status)
	if err != nil {
		return fmt.Errorf("update storage group status: %w", err)
	}

	return nil
}

// Delete removes a storage group
func (s *StorageGroupStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM storage_groups WHERE id = $1`

	_, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete storage group: %w", err)
	}

	return nil
}
