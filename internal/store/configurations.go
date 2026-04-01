package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterConfigurationStore handles cluster configuration operations
type ClusterConfigurationStore struct {
	pool *pgxpool.Pool
}

// ListByClusterID returns all configurations for a cluster
func (s *ClusterConfigurationStore) ListByClusterID(ctx context.Context, clusterID string) ([]*types.ClusterConfiguration, error) {
	query := `
		SELECT id, cluster_id, config_type, config_name, status, error_message, created_at, completed_at, metadata
		FROM cluster_configurations
		WHERE cluster_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query configurations: %w", err)
	}
	defer rows.Close()

	var configs []*types.ClusterConfiguration
	for rows.Next() {
		config := &types.ClusterConfiguration{}
		if err := rows.Scan(
			&config.ID,
			&config.ClusterID,
			&config.ConfigType,
			&config.ConfigName,
			&config.Status,
			&config.ErrorMessage,
			&config.CreatedAt,
			&config.CompletedAt,
			&config.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan configuration: %w", err)
		}
		configs = append(configs, config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate configurations: %w", err)
	}

	return configs, nil
}

// GetByID returns a configuration by ID
func (s *ClusterConfigurationStore) GetByID(ctx context.Context, id string) (*types.ClusterConfiguration, error) {
	query := `
		SELECT id, cluster_id, config_type, config_name, status, error_message, created_at, completed_at, metadata
		FROM cluster_configurations
		WHERE id = $1
	`

	config := &types.ClusterConfiguration{}
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&config.ID,
		&config.ClusterID,
		&config.ConfigType,
		&config.ConfigName,
		&config.Status,
		&config.ErrorMessage,
		&config.CreatedAt,
		&config.CompletedAt,
		&config.Metadata,
	)

	if err != nil {
		return nil, fmt.Errorf("get configuration: %w", err)
	}

	return config, nil
}

// Create creates a new cluster configuration task
func (s *ClusterConfigurationStore) Create(ctx context.Context, clusterID string, configType types.ConfigType, configName string) (string, error) {
	return s.CreateWithTracking(ctx, clusterID, configType, configName, false, nil, nil)
}

// CreateWithTracking creates a new cluster configuration task with user tracking
func (s *ClusterConfigurationStore) CreateWithTracking(ctx context.Context, clusterID string, configType types.ConfigType, configName string, userDefined bool, createdByUserID *string, source *types.ConfigSource) (string, error) {
	query := `
		INSERT INTO cluster_configurations (cluster_id, config_type, config_name, status, user_defined, created_by_user_id, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	var configID string
	err := s.pool.QueryRow(ctx, query, clusterID, configType, configName, types.ConfigStatusPending, userDefined, createdByUserID, source).Scan(&configID)
	if err != nil {
		return "", fmt.Errorf("create configuration: %w", err)
	}

	return configID, nil
}

// UpdateStatus updates the status of a configuration
func (s *ClusterConfigurationStore) UpdateStatus(ctx context.Context, id string, status types.ConfigStatus, errorMessage *string) error {
	query := `
		UPDATE cluster_configurations
		SET status = $1::text,
		    error_message = $2,
		    completed_at = CASE WHEN $1::text IN ('completed', 'failed') THEN NOW() ELSE NULL END
		WHERE id = $3
	`

	result, err := s.pool.Exec(ctx, query, status, errorMessage, id)
	if err != nil {
		return fmt.Errorf("update configuration status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("configuration not found: %s", id)
	}

	return nil
}
