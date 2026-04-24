package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JobType represents the type of async job
type JobType string

const (
	JobTypeCreate                 JobType = "CREATE"
	JobTypeDestroy                JobType = "DESTROY"
	JobTypeJanitorDestroy         JobType = "JANITOR_DESTROY"
	JobTypeConfigureEFS           JobType = "CONFIGURE_EFS"
	JobTypeProvisionSharedStorage JobType = "PROVISION_SHARED_STORAGE"
	JobTypeUnlinkSharedStorage    JobType = "UNLINK_SHARED_STORAGE"
	JobTypeHibernate              JobType = "HIBERNATE"
	JobTypeResume                 JobType = "RESUME"
	JobTypePostConfigure          JobType = "POST_CONFIGURE"

	// Future job types (not yet implemented):
	// JobTypeScaleWorkers           JobType = "SCALE_WORKERS"  // Off-hours worker scaling
	// JobTypeOrphanSweep            JobType = "ORPHAN_SWEEP"   // Automated orphan resource cleanup
)

// JobStatus represents the current state of a job
type JobStatus string

const (
	JobStatusPending  JobStatus = "PENDING"
	JobStatusRunning  JobStatus = "RUNNING"
	JobStatusSucceeded JobStatus = "SUCCEEDED"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusRetrying  JobStatus = "RETRYING"
)

// JobMetadata is arbitrary JSON metadata stored with a job
type JobMetadata map[string]interface{}

// Value implements driver.Valuer for database serialization
func (m JobMetadata) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner for database deserialization
func (m *JobMetadata) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("unsupported type for JobMetadata: %T", value)
	}

	return json.Unmarshal(bytes, m)
}

// Job represents an async job record
type Job struct {
	ID          string      `db:"id" json:"id"`
	ClusterID   string      `db:"cluster_id" json:"cluster_id"`
	JobType     JobType     `db:"job_type" json:"job_type"`
	Status      JobStatus   `db:"status" json:"status"`
	Attempt     int         `db:"attempt" json:"attempt"`
	MaxAttempts int         `db:"max_attempts" json:"max_attempts"`
	ErrorCode   *string     `db:"error_code" json:"error_code"`
	ErrorMessage *string    `db:"error_message" json:"error_message"`
	StartedAt   *time.Time  `db:"started_at" json:"started_at"`
	EndedAt     *time.Time  `db:"ended_at" json:"ended_at"`
	CreatedAt   time.Time   `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time   `db:"updated_at" json:"updated_at"`
	Metadata    JobMetadata `db:"metadata" json:"metadata"`
}

// JobLock represents a cluster lock held by a worker
type JobLock struct {
	ClusterID string    `db:"cluster_id" json:"cluster_id"`
	JobID     string    `db:"job_id" json:"job_id"`
	LockedAt  time.Time `db:"locked_at" json:"locked_at"`
	LockedBy  string    `db:"locked_by" json:"locked_by"` // Worker instance ID
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
}

// PreflightCheckError represents a preflight validation error that should not be retried
type PreflightCheckError struct {
	Message string
}

func (e *PreflightCheckError) Error() string {
	return e.Message
}

// NewPreflightCheckError creates a new preflight check error
func NewPreflightCheckError(format string, args ...interface{}) error {
	return &PreflightCheckError{
		Message: fmt.Sprintf(format, args...),
	}
}

// IsPreflightCheckError checks if an error is a preflight check error
func IsPreflightCheckError(err error) bool {
	_, ok := err.(*PreflightCheckError)
	return ok
}
