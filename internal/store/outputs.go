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
			kubeadmin_secret_ref, metadata_s3_uri
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
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
	)

	if err != nil {
		return fmt.Errorf("insert cluster outputs: %w", err)
	}

	return nil
}

// GetByClusterID retrieves outputs for a cluster
func (s *ClusterOutputsStore) GetByClusterID(ctx context.Context, clusterID string) (*types.ClusterOutputs, error) {
	query := `
		SELECT id, cluster_id, api_url, console_url, kubeconfig_s3_uri,
			kubeadmin_secret_ref, metadata_s3_uri, created_at, updated_at
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
