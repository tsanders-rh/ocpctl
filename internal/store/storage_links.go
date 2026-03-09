package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterStorageLinkStore handles database operations for cluster storage links
type ClusterStorageLinkStore struct {
	pool *pgxpool.Pool
}

// NewClusterStorageLinkStore creates a new cluster storage link store
func NewClusterStorageLinkStore(pool *pgxpool.Pool) *ClusterStorageLinkStore {
	return &ClusterStorageLinkStore{pool: pool}
}

// Create inserts a new cluster storage link record
func (s *ClusterStorageLinkStore) Create(ctx context.Context, link *types.ClusterStorageLink) error {
	query := `
		INSERT INTO cluster_storage_links (
			id, cluster_id, storage_group_id, role
		) VALUES (
			$1, $2, $3, $4
		)
	`

	_, err := s.pool.Exec(ctx, query,
		link.ID,
		link.ClusterID,
		link.StorageGroupID,
		link.Role,
	)

	if err != nil {
		return fmt.Errorf("insert cluster storage link: %w", err)
	}

	return nil
}

// GetByClusterID retrieves all storage links for a cluster
func (s *ClusterStorageLinkStore) GetByClusterID(ctx context.Context, clusterID string) ([]*types.ClusterStorageLink, error) {
	query := `
		SELECT id, cluster_id, storage_group_id, role, created_at
		FROM cluster_storage_links
		WHERE cluster_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster storage links: %w", err)
	}
	defer rows.Close()

	links := []*types.ClusterStorageLink{}
	for rows.Next() {
		var link types.ClusterStorageLink
		err := rows.Scan(
			&link.ID,
			&link.ClusterID,
			&link.StorageGroupID,
			&link.Role,
			&link.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan cluster storage link: %w", err)
		}
		links = append(links, &link)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster storage links: %w", err)
	}

	return links, nil
}

// GetByStorageGroupID retrieves all cluster links for a storage group
func (s *ClusterStorageLinkStore) GetByStorageGroupID(ctx context.Context, storageGroupID string) ([]*types.ClusterStorageLink, error) {
	query := `
		SELECT id, cluster_id, storage_group_id, role, created_at
		FROM cluster_storage_links
		WHERE storage_group_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, storageGroupID)
	if err != nil {
		return nil, fmt.Errorf("query storage group links: %w", err)
	}
	defer rows.Close()

	links := []*types.ClusterStorageLink{}
	for rows.Next() {
		var link types.ClusterStorageLink
		err := rows.Scan(
			&link.ID,
			&link.ClusterID,
			&link.StorageGroupID,
			&link.Role,
			&link.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan storage group link: %w", err)
		}
		links = append(links, &link)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate storage group links: %w", err)
	}

	return links, nil
}

// GetByClusterAndGroup retrieves a specific cluster-storage group link
func (s *ClusterStorageLinkStore) GetByClusterAndGroup(ctx context.Context, clusterID, storageGroupID string) (*types.ClusterStorageLink, error) {
	query := `
		SELECT id, cluster_id, storage_group_id, role, created_at
		FROM cluster_storage_links
		WHERE cluster_id = $1 AND storage_group_id = $2
	`

	var link types.ClusterStorageLink
	err := s.pool.QueryRow(ctx, query, clusterID, storageGroupID).Scan(
		&link.ID,
		&link.ClusterID,
		&link.StorageGroupID,
		&link.Role,
		&link.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("query cluster storage link: %w", err)
	}

	return &link, nil
}

// Delete removes a cluster storage link
func (s *ClusterStorageLinkStore) Delete(ctx context.Context, clusterID, storageGroupID string) error {
	query := `
		DELETE FROM cluster_storage_links
		WHERE cluster_id = $1 AND storage_group_id = $2
	`

	_, err := s.pool.Exec(ctx, query, clusterID, storageGroupID)
	if err != nil {
		return fmt.Errorf("delete cluster storage link: %w", err)
	}

	return nil
}

// CountByStorageGroupID counts the number of clusters linked to a storage group
func (s *ClusterStorageLinkStore) CountByStorageGroupID(ctx context.Context, storageGroupID string) (int, error) {
	query := `SELECT COUNT(*) FROM cluster_storage_links WHERE storage_group_id = $1`

	var count int
	err := s.pool.QueryRow(ctx, query, storageGroupID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count storage group links: %w", err)
	}

	return count, nil
}
