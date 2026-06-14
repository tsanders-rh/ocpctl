package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterOutputsStore handles cluster outputs operations
type ClusterOutputsStore struct {
	pool *pgxpool.Pool
}

// Create inserts cluster outputs
func (s *ClusterOutputsStore) Create(ctx context.Context, outputs *types.ClusterOutputs) error {
	query := `
		INSERT INTO cluster_outputs (
			id, cluster_id, api_url, console_url, kubeconfig_s3_uri,
			kubeadmin_secret_ref, metadata_s3_uri, dashboard_token
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err := s.pool.Exec(ctx, query,
		outputs.ID,
		outputs.ClusterID,
		outputs.APIURL,
		outputs.ConsoleURL,
		outputs.KubeconfigS3URI,
		outputs.KubeadminSecretRef,
		outputs.MetadataS3URI,
		outputs.DashboardToken,
	)

	if err != nil {
		return fmt.Errorf("insert cluster outputs: %w", err)
	}

	return nil
}

// Update updates existing cluster outputs
func (s *ClusterOutputsStore) Update(ctx context.Context, outputs *types.ClusterOutputs) error {
	query := `
		UPDATE cluster_outputs
		SET api_url = $1,
			console_url = $2,
			kubeconfig_s3_uri = $3,
			kubeadmin_secret_ref = $4,
			metadata_s3_uri = $5,
			dashboard_token = $6,
			updated_at = NOW()
		WHERE cluster_id = $7
	`

	result, err := s.pool.Exec(ctx, query,
		outputs.APIURL,
		outputs.ConsoleURL,
		outputs.KubeconfigS3URI,
		outputs.KubeadminSecretRef,
		outputs.MetadataS3URI,
		outputs.DashboardToken,
		outputs.ClusterID,
	)

	if err != nil {
		return fmt.Errorf("update cluster outputs: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Upsert creates or updates cluster outputs (insert or update if exists)
func (s *ClusterOutputsStore) Upsert(ctx context.Context, outputs *types.ClusterOutputs) error {
	query := `
		INSERT INTO cluster_outputs (
			id, cluster_id, api_url, console_url, kubeconfig_s3_uri,
			kubeadmin_secret_ref, metadata_s3_uri, dashboard_token,
			sa_name, sa_namespace, sa_token, sa_token_expires_at, oc_login_command
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
		ON CONFLICT (cluster_id)
		DO UPDATE SET
			api_url = COALESCE(EXCLUDED.api_url, cluster_outputs.api_url),
			console_url = COALESCE(EXCLUDED.console_url, cluster_outputs.console_url),
			kubeconfig_s3_uri = COALESCE(EXCLUDED.kubeconfig_s3_uri, cluster_outputs.kubeconfig_s3_uri),
			kubeadmin_secret_ref = COALESCE(EXCLUDED.kubeadmin_secret_ref, cluster_outputs.kubeadmin_secret_ref),
			metadata_s3_uri = COALESCE(EXCLUDED.metadata_s3_uri, cluster_outputs.metadata_s3_uri),
			dashboard_token = COALESCE(EXCLUDED.dashboard_token, cluster_outputs.dashboard_token),
			sa_name = COALESCE(EXCLUDED.sa_name, cluster_outputs.sa_name),
			sa_namespace = COALESCE(EXCLUDED.sa_namespace, cluster_outputs.sa_namespace),
			sa_token = COALESCE(EXCLUDED.sa_token, cluster_outputs.sa_token),
			sa_token_expires_at = COALESCE(EXCLUDED.sa_token_expires_at, cluster_outputs.sa_token_expires_at),
			oc_login_command = COALESCE(EXCLUDED.oc_login_command, cluster_outputs.oc_login_command),
			updated_at = NOW()
	`

	_, err := s.pool.Exec(ctx, query,
		outputs.ID,
		outputs.ClusterID,
		outputs.APIURL,
		outputs.ConsoleURL,
		outputs.KubeconfigS3URI,
		outputs.KubeadminSecretRef,
		outputs.MetadataS3URI,
		outputs.DashboardToken,
		outputs.SAName,
		outputs.SANamespace,
		outputs.SAToken,
		outputs.SATokenExpiresAt,
		outputs.OcLoginCommand,
	)

	if err != nil {
		return fmt.Errorf("upsert cluster outputs: %w", err)
	}

	return nil
}

// GetByClusterID retrieves outputs for a cluster
func (s *ClusterOutputsStore) GetByClusterID(ctx context.Context, clusterID string) (*types.ClusterOutputs, error) {
	query := `
		SELECT id, cluster_id, api_url, console_url, kubeconfig_s3_uri,
			kubeadmin_secret_ref, metadata_s3_uri, dashboard_token,
			sa_name, sa_namespace, sa_token, sa_token_expires_at, oc_login_command,
			created_at, updated_at
		FROM cluster_outputs
		WHERE cluster_id = $1
	`

	var outputs types.ClusterOutputs
	err := s.pool.QueryRow(ctx, query, clusterID).Scan(
		&outputs.ID,
		&outputs.ClusterID,
		&outputs.APIURL,
		&outputs.ConsoleURL,
		&outputs.KubeconfigS3URI,
		&outputs.KubeadminSecretRef,
		&outputs.MetadataS3URI,
		&outputs.DashboardToken,
		&outputs.SAName,
		&outputs.SANamespace,
		&outputs.SAToken,
		&outputs.SATokenExpiresAt,
		&outputs.OcLoginCommand,
		&outputs.CreatedAt,
		&outputs.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query cluster outputs: %w", err)
	}

	return &outputs, nil
}

// ArtifactStore handles cluster artifacts operations
type ArtifactStore struct {
	pool *pgxpool.Pool
}

// Create inserts a cluster artifact record
func (s *ArtifactStore) Create(ctx context.Context, artifact *types.ClusterArtifact) error {
	query := `
		INSERT INTO cluster_artifacts (
			id, cluster_id, artifact_type, s3_uri, checksum, size_bytes
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
	`

	_, err := s.pool.Exec(ctx, query,
		artifact.ID,
		artifact.ClusterID,
		artifact.ArtifactType,
		artifact.S3URI,
		artifact.Checksum,
		artifact.SizeBytes,
	)

	if err != nil {
		return fmt.Errorf("insert cluster artifact: %w", err)
	}

	return nil
}

// ListByClusterID retrieves all artifacts for a cluster
func (s *ArtifactStore) ListByClusterID(ctx context.Context, clusterID string) ([]*types.ClusterArtifact, error) {
	query := `
		SELECT id, cluster_id, artifact_type, s3_uri, checksum, size_bytes, created_at
		FROM cluster_artifacts
		WHERE cluster_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster artifacts: %w", err)
	}
	defer rows.Close()

	artifacts := []*types.ClusterArtifact{}
	for rows.Next() {
		var artifact types.ClusterArtifact
		err := rows.Scan(
			&artifact.ID,
			&artifact.ClusterID,
			&artifact.ArtifactType,
			&artifact.S3URI,
			&artifact.Checksum,
			&artifact.SizeBytes,
			&artifact.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan cluster artifact: %w", err)
		}
		artifacts = append(artifacts, &artifact)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster artifacts: %w", err)
	}

	return artifacts, nil
}

// GetLatestSnapshot retrieves the most recent install dir snapshot for a cluster
func (s *ArtifactStore) GetLatestSnapshot(ctx context.Context, clusterID string) (*types.ClusterArtifact, error) {
	query := `
		SELECT id, cluster_id, artifact_type, s3_uri, checksum, size_bytes, created_at
		FROM cluster_artifacts
		WHERE cluster_id = $1 AND artifact_type = $2
		ORDER BY created_at DESC
		LIMIT 1
	`

	var artifact types.ClusterArtifact
	err := s.pool.QueryRow(ctx, query, clusterID, types.ArtifactTypeInstallDirSnapshot).Scan(
		&artifact.ID,
		&artifact.ClusterID,
		&artifact.ArtifactType,
		&artifact.S3URI,
		&artifact.Checksum,
		&artifact.SizeBytes,
		&artifact.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query latest snapshot: %w", err)
	}

	return &artifact, nil
}
