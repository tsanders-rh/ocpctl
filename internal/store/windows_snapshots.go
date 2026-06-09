package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// CreateWindowsSnapshot creates a new windows snapshot record
func (s *Store) CreateWindowsSnapshot(ctx context.Context, snapshot *types.WindowsSnapshot) error {
	query := `
		INSERT INTO windows_snapshots (
			id, region, version, ebs_snapshot_id, status, ssm_parameter_path,
			s3_source_url, job_id, snapshot_size_gb
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at
	`

	return s.db.QueryRowContext(
		ctx, query,
		snapshot.ID,
		snapshot.Region,
		snapshot.Version,
		snapshot.EBSSnapshotID,
		snapshot.Status,
		snapshot.SSMParameterPath,
		snapshot.S3SourceURL,
		snapshot.JobID,
		snapshot.SnapshotSizeGB,
	).Scan(&snapshot.CreatedAt, &snapshot.UpdatedAt)
}

// GetWindowsSnapshot retrieves a snapshot by ID
func (s *Store) GetWindowsSnapshot(ctx context.Context, id string) (*types.WindowsSnapshot, error) {
	var snapshot types.WindowsSnapshot
	query := `
		SELECT id, region, version, ebs_snapshot_id, status, ssm_parameter_path,
		       s3_source_url, created_at, updated_at, validated_at, error_message,
		       job_id, snapshot_size_gb, validation_vm_booted
		FROM windows_snapshots
		WHERE id = $1
	`

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&snapshot.ID,
		&snapshot.Region,
		&snapshot.Version,
		&snapshot.EBSSnapshotID,
		&snapshot.Status,
		&snapshot.SSMParameterPath,
		&snapshot.S3SourceURL,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
		&snapshot.ValidatedAt,
		&snapshot.ErrorMessage,
		&snapshot.JobID,
		&snapshot.SnapshotSizeGB,
		&snapshot.ValidationVMBooted,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	return &snapshot, nil
}

// GetWindowsSnapshotByRegionAndVersion retrieves a snapshot by region and version
func (s *Store) GetWindowsSnapshotByRegionAndVersion(ctx context.Context, region, version string) (*types.WindowsSnapshot, error) {
	var snapshot types.WindowsSnapshot
	query := `
		SELECT id, region, version, ebs_snapshot_id, status, ssm_parameter_path,
		       s3_source_url, created_at, updated_at, validated_at, error_message,
		       job_id, snapshot_size_gb, validation_vm_booted
		FROM windows_snapshots
		WHERE region = $1 AND version = $2
	`

	err := s.db.QueryRowContext(ctx, query, region, version).Scan(
		&snapshot.ID,
		&snapshot.Region,
		&snapshot.Version,
		&snapshot.EBSSnapshotID,
		&snapshot.Status,
		&snapshot.SSMParameterPath,
		&snapshot.S3SourceURL,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
		&snapshot.ValidatedAt,
		&snapshot.ErrorMessage,
		&snapshot.JobID,
		&snapshot.SnapshotSizeGB,
		&snapshot.ValidationVMBooted,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found is not an error for this query
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	return &snapshot, nil
}

// ListWindowsSnapshots retrieves all snapshots with optional filtering
func (s *Store) ListWindowsSnapshots(ctx context.Context, region *string, status *types.WindowsSnapshotStatus) ([]*types.WindowsSnapshot, error) {
	query := `
		SELECT id, region, version, ebs_snapshot_id, status, ssm_parameter_path,
		       s3_source_url, created_at, updated_at, validated_at, error_message,
		       job_id, snapshot_size_gb, validation_vm_booted
		FROM windows_snapshots
		WHERE ($1::text IS NULL OR region = $1)
		  AND ($2::text IS NULL OR status = $2)
		ORDER BY region, version DESC, created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, region, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []*types.WindowsSnapshot
	for rows.Next() {
		var snapshot types.WindowsSnapshot
		err := rows.Scan(
			&snapshot.ID,
			&snapshot.Region,
			&snapshot.Version,
			&snapshot.EBSSnapshotID,
			&snapshot.Status,
			&snapshot.SSMParameterPath,
			&snapshot.S3SourceURL,
			&snapshot.CreatedAt,
			&snapshot.UpdatedAt,
			&snapshot.ValidatedAt,
			&snapshot.ErrorMessage,
			&snapshot.JobID,
			&snapshot.SnapshotSizeGB,
			&snapshot.ValidationVMBooted,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan snapshot: %w", err)
		}
		snapshots = append(snapshots, &snapshot)
	}

	return snapshots, rows.Err()
}

// UpdateWindowsSnapshotStatus updates the status and error message of a snapshot
func (s *Store) UpdateWindowsSnapshotStatus(ctx context.Context, id string, status types.WindowsSnapshotStatus, errorMessage *string) error {
	query := `
		UPDATE windows_snapshots
		SET status = $1, error_message = $2, updated_at = NOW()
		WHERE id = $3
	`

	result, err := s.db.ExecContext(ctx, query, status, errorMessage, id)
	if err != nil {
		return fmt.Errorf("failed to update snapshot status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	return nil
}

// UpdateWindowsSnapshotValidation marks a snapshot as validated
func (s *Store) UpdateWindowsSnapshotValidation(ctx context.Context, id string, vmBooted bool, ssmPath string) error {
	query := `
		UPDATE windows_snapshots
		SET status = $1,
		    validated_at = $2,
		    validation_vm_booted = $3,
		    ssm_parameter_path = $4,
		    error_message = NULL,
		    updated_at = NOW()
		WHERE id = $5
	`

	result, err := s.db.ExecContext(
		ctx, query,
		types.WindowsSnapshotStatusReady,
		time.Now(),
		vmBooted,
		ssmPath,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to update snapshot validation: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	return nil
}

// DeleteWindowsSnapshot deletes a snapshot record
func (s *Store) DeleteWindowsSnapshot(ctx context.Context, id string) error {
	query := `DELETE FROM windows_snapshots WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	return nil
}

// GetWindowsSnapshotCoverage calculates snapshot coverage across regions
func (s *Store) GetWindowsSnapshotCoverage(ctx context.Context, allRegions []string, latestVersion string) (*types.WindowsSnapshotCoverage, error) {
	// Get all ready snapshots
	readyStatus := types.WindowsSnapshotStatusReady
	snapshots, err := s.ListWindowsSnapshots(ctx, nil, &readyStatus)
	if err != nil {
		return nil, err
	}

	// Build coverage map
	snapshotsByRegion := make(map[string]*types.WindowsSnapshot)
	for _, snap := range snapshots {
		if existing, ok := snapshotsByRegion[snap.Region]; !ok || snap.Version > existing.Version {
			snapshotsByRegion[snap.Region] = snap
		}
	}

	// Calculate coverage
	var missingRegions []string
	var outdatedRegions []string

	for _, region := range allRegions {
		if snap, ok := snapshotsByRegion[region]; !ok {
			missingRegions = append(missingRegions, region)
		} else if snap.Version != latestVersion {
			outdatedRegions = append(outdatedRegions, region)
		}
	}

	coveredCount := len(snapshotsByRegion)
	totalCount := len(allRegions)
	coveragePercent := 0.0
	if totalCount > 0 {
		coveragePercent = float64(coveredCount) / float64(totalCount) * 100
	}

	return &types.WindowsSnapshotCoverage{
		TotalRegions:     totalCount,
		CoveredRegions:   coveredCount,
		CoveragePercent:  coveragePercent,
		LatestVersion:    latestVersion,
		MissingRegions:   missingRegions,
		OutdatedRegions:  outdatedRegions,
		SnapshotsByRegion: snapshotsByRegion,
	}, nil
}

// CreateWindowsSnapshotAudit creates an audit log entry for a snapshot operation
func (s *Store) CreateWindowsSnapshotAudit(ctx context.Context, audit *types.WindowsSnapshotAudit) error {
	query := `
		INSERT INTO windows_snapshot_audit (
			id, snapshot_id, action, user_id, details
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`

	return s.db.QueryRowContext(
		ctx, query,
		audit.ID,
		audit.SnapshotID,
		audit.Action,
		audit.UserID,
		audit.Details,
	).Scan(&audit.CreatedAt)
}
