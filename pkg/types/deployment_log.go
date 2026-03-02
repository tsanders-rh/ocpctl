package types

import "time"

// DeploymentLogSource represents the source of a deployment log entry
type DeploymentLogSource string

const (
	DeploymentLogSourceInstaller  DeploymentLogSource = "installer"  // openshift-install CLI output
	DeploymentLogSourceWorker     DeploymentLogSource = "worker"     // ocpctl worker logs
	DeploymentLogSourceTerraform  DeploymentLogSource = "terraform"  // future: terraform output
)

// DeploymentLogLevel represents the severity level of a log entry
type DeploymentLogLevel string

const (
	DeploymentLogLevelDebug   DeploymentLogLevel = "debug"
	DeploymentLogLevelInfo    DeploymentLogLevel = "info"
	DeploymentLogLevelWarn    DeploymentLogLevel = "warn"
	DeploymentLogLevelError   DeploymentLogLevel = "error"
)

// DeploymentLog represents a single log entry from a cluster deployment operation
type DeploymentLog struct {
	ID        int64               `db:"id" json:"id"`
	ClusterID string              `db:"cluster_id" json:"cluster_id"`
	JobID     string              `db:"job_id" json:"job_id"`
	Sequence  int64               `db:"sequence" json:"sequence"`
	Timestamp time.Time           `db:"timestamp" json:"timestamp"`
	LogLevel  *string             `db:"log_level" json:"log_level,omitempty"`
	Message   string              `db:"message" json:"message"`
	Source    DeploymentLogSource `db:"source" json:"source"`
}

// DeploymentLogStats provides summary statistics about deployment logs
type DeploymentLogStats struct {
	TotalLines  int       `json:"total_lines"`
	ErrorCount  int       `json:"error_count"`
	WarnCount   int       `json:"warn_count"`
	LastUpdated time.Time `json:"last_updated"`
}
