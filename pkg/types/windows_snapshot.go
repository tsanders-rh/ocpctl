package types

import (
	"time"
)

// WindowsSnapshotStatus represents the lifecycle state of a regional Windows snapshot
type WindowsSnapshotStatus string

const (
	WindowsSnapshotStatusCreating   WindowsSnapshotStatus = "creating"
	WindowsSnapshotStatusValidating WindowsSnapshotStatus = "validating"
	WindowsSnapshotStatusReady      WindowsSnapshotStatus = "ready"
	WindowsSnapshotStatusFailed     WindowsSnapshotStatus = "failed"
	WindowsSnapshotStatusDeleting   WindowsSnapshotStatus = "deleting"
)

// WindowsSnapshot represents a pre-created EBS snapshot for fast Windows VM provisioning
type WindowsSnapshot struct {
	ID                 string                `json:"id" db:"id"`
	Region             string                `json:"region" db:"region"`
	Version            string                `json:"version" db:"version"`
	EBSSnapshotID      string                `json:"ebs_snapshot_id" db:"ebs_snapshot_id"`
	Status             WindowsSnapshotStatus `json:"status" db:"status"`
	SSMParameterPath   *string               `json:"ssm_parameter_path,omitempty" db:"ssm_parameter_path"`
	S3SourceURL        *string               `json:"s3_source_url,omitempty" db:"s3_source_url"`
	CreatedAt          time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at" db:"updated_at"`
	ValidatedAt        *time.Time            `json:"validated_at,omitempty" db:"validated_at"`
	ErrorMessage       *string               `json:"error_message,omitempty" db:"error_message"`
	JobID              *string               `json:"job_id,omitempty" db:"job_id"`
	SnapshotSizeGB     *int                  `json:"snapshot_size_gb,omitempty" db:"snapshot_size_gb"`
	ValidationVMBooted bool                  `json:"validation_vm_booted" db:"validation_vm_booted"`
}

// CreateWindowsSnapshotRequest represents a request to create a new regional snapshot
type CreateWindowsSnapshotRequest struct {
	Region      string  `json:"region" validate:"required"`
	Version     string  `json:"version" validate:"required"`
	S3SourceURL *string `json:"s3_source_url,omitempty"`
}

// WindowsSnapshotCoverage represents snapshot availability across regions
type WindowsSnapshotCoverage struct {
	TotalRegions     int                        `json:"total_regions"`
	CoveredRegions   int                        `json:"covered_regions"`
	CoveragePercent  float64                    `json:"coverage_percent"`
	LatestVersion    string                     `json:"latest_version"`
	MissingRegions   []string                   `json:"missing_regions"`
	OutdatedRegions  []string                   `json:"outdated_regions"`
	SnapshotsByRegion map[string]*WindowsSnapshot `json:"snapshots_by_region"`
}

// WindowsSnapshotAudit represents an audit log entry for snapshot operations
type WindowsSnapshotAudit struct {
	ID         string                 `json:"id" db:"id"`
	SnapshotID string                 `json:"snapshot_id" db:"snapshot_id"`
	Action     string                 `json:"action" db:"action"`
	UserID     *string                `json:"user_id,omitempty" db:"user_id"`
	Details    map[string]interface{} `json:"details,omitempty" db:"details"`
	CreatedAt  time.Time              `json:"created_at" db:"created_at"`
}
