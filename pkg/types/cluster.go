package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// ClusterStatus represents the current state of a cluster
type ClusterStatus string

const (
	ClusterStatusPending          ClusterStatus = "PENDING"
	ClusterStatusCreating         ClusterStatus = "CREATING"
	ClusterStatusReady            ClusterStatus = "READY"
	ClusterStatusHibernating      ClusterStatus = "HIBERNATING"
	ClusterStatusHibernated       ClusterStatus = "HIBERNATED"
	ClusterStatusResuming         ClusterStatus = "RESUMING"
	ClusterStatusDestroying       ClusterStatus = "DESTROYING"
	ClusterStatusDestroyVerifying ClusterStatus = "DESTROY_VERIFYING" // Verifying all resources deleted
	ClusterStatusDestroyFailed    ClusterStatus = "DESTROY_FAILED"     // Destroy attempted but resources remain
	ClusterStatusDestroyed        ClusterStatus = "DESTROYED"
	ClusterStatusFailed           ClusterStatus = "FAILED"
)

// Platform represents the cloud platform
type Platform string

const (
	PlatformAWS      Platform = "aws"
	PlatformIBMCloud Platform = "ibmcloud"
	PlatformGCP      Platform = "gcp"
)

// ClusterType represents the type of Kubernetes cluster
type ClusterType string

const (
	ClusterTypeOpenShift ClusterType = "openshift" // OpenShift (OCP/ROSA)
	ClusterTypeEKS       ClusterType = "eks"       // AWS Elastic Kubernetes Service
	ClusterTypeIKS       ClusterType = "iks"       // IBM Cloud Kubernetes Service
	ClusterTypeGKE       ClusterType = "gke"       // Google Kubernetes Engine
)

// Tags is a map of key-value pairs stored as JSONB
type Tags map[string]string

// Value implements driver.Valuer for database serialization
func (t Tags) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}
	return json.Marshal(t)
}

// Scan implements sql.Scanner for database deserialization
func (t *Tags) Scan(value interface{}) error {
	if value == nil {
		*t = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		*t = nil
		return nil
	}

	result := make(Tags)
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}
	*t = result
	return nil
}

// Cluster represents a cluster record in the database
type Cluster struct {
	ID             string        `db:"id" json:"id"`
	Name           string        `db:"name" json:"name"`
	Platform       Platform      `db:"platform" json:"platform"`
	ClusterType    ClusterType   `db:"cluster_type" json:"cluster_type"`
	Version        string        `db:"version" json:"version"`
	Profile        string        `db:"profile" json:"profile"`
	Region         string        `db:"region" json:"region"`
	BaseDomain     *string       `db:"base_domain" json:"base_domain,omitempty"`
	Owner          string        `db:"owner" json:"owner"`           // Email for display/metadata
	OwnerID        string        `db:"owner_id" json:"owner_id"`        // Foreign key to users table
	Team           string        `db:"team" json:"team"`
	CostCenter     string        `db:"cost_center" json:"cost_center"`
	Status         ClusterStatus `db:"status" json:"status"`
	RequestedBy    string        `db:"requested_by" json:"requested_by"` // IAM principal ARN
	TTLHours       int           `db:"ttl_hours" json:"ttl_hours"`
	DestroyAt      *time.Time    `db:"destroy_at" json:"destroy_at"`
	CreatedAt      time.Time     `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time     `db:"updated_at" json:"updated_at"`
	DestroyedAt    *time.Time    `db:"destroyed_at" json:"destroyed_at"`
	RequestTags         Tags           `db:"request_tags" json:"request_tags"`
	EffectiveTags       Tags           `db:"effective_tags" json:"effective_tags"`
	SSHPublicKey        *string        `db:"ssh_public_key" json:"ssh_public_key"`
	OffhoursOptIn       bool           `db:"offhours_opt_in" json:"offhours_opt_in"`
	WorkHoursEnabled    *bool          `db:"work_hours_enabled" json:"work_hours_enabled"`       // NULL = use user default
	WorkHoursStart      *time.Time     `db:"work_hours_start" json:"work_hours_start"`
	WorkHoursEnd        *time.Time     `db:"work_hours_end" json:"work_hours_end"`
	WorkDays            *int16         `db:"work_days" json:"work_days"`
	LastWorkHoursCheck  *time.Time     `db:"last_work_hours_check" json:"last_work_hours_check"`
	PostDeployStatus    *string        `db:"post_deploy_status" json:"post_deploy_status,omitempty"`
	PostDeployCompletedAt *time.Time   `db:"post_deploy_completed_at" json:"post_deploy_completed_at,omitempty"`
	SkipPostDeployment  bool           `db:"skip_post_deployment" json:"skip_post_deployment"`
	CustomPostConfig    *CustomPostConfig `db:"custom_post_config" json:"custom_post_config,omitempty"`
	StorageConfig       *StorageConfig `db:"storage_config" json:"storage_config,omitempty"`
	PreserveOnFailure   bool           `db:"preserve_on_failure" json:"preserve_on_failure"`
	CredentialsMode     *string        `db:"credentials_mode" json:"credentials_mode,omitempty"`
	// Cluster outputs (joined from cluster_outputs table)
	APIURL              *string        `db:"api_url" json:"api_url,omitempty"`
	ConsoleURL          *string        `db:"console_url" json:"console_url,omitempty"`
}

// ClusterOutputs represents cluster access information
type ClusterOutputs struct {
	ID                  string     `db:"id" json:"id"`
	ClusterID           string     `db:"cluster_id" json:"cluster_id"`
	APIURL              *string    `db:"api_url" json:"api_url"`
	ConsoleURL          *string    `db:"console_url" json:"console_url"`
	KubeconfigS3URI     *string    `db:"kubeconfig_s3_uri" json:"kubeconfig_s3_uri"`
	KubeadminSecretRef  *string    `db:"kubeadmin_secret_ref" json:"kubeadmin_secret_ref"`
	MetadataS3URI       *string    `db:"metadata_s3_uri" json:"metadata_s3_uri"`
	DashboardToken      *string    `db:"dashboard_token" json:"dashboard_token,omitempty"`
	CreatedAt           time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at" json:"updated_at"`
}

// ArtifactType represents the type of artifact stored
type ArtifactType string

const (
	ArtifactTypeInstallDirSnapshot ArtifactType = "INSTALL_DIR_SNAPSHOT"
	ArtifactTypeLog                ArtifactType = "LOG"
	ArtifactTypeAuthBundle         ArtifactType = "AUTH_BUNDLE"
	ArtifactTypeMetadata           ArtifactType = "METADATA"
	ArtifactTypeDestroyLog         ArtifactType = "DESTROY_LOG"
)

// ClusterArtifact represents a stored artifact in S3
type ClusterArtifact struct {
	ID           string       `db:"id" json:"id"`
	ClusterID    string       `db:"cluster_id" json:"cluster_id"`
	ArtifactType ArtifactType `db:"artifact_type" json:"artifact_type"`
	S3URI        string       `db:"s3_uri" json:"s3_uri"`
	Checksum     *string      `db:"checksum" json:"checksum"`
	SizeBytes    *int64       `db:"size_bytes" json:"size_bytes"`
	CreatedAt    time.Time    `db:"created_at" json:"created_at"`
}

// ConfigType represents the type of post-deployment configuration
type ConfigType string

const (
	ConfigTypeOperator ConfigType = "operator"
	ConfigTypeScript   ConfigType = "script"
	ConfigTypeManifest ConfigType = "manifest"
	ConfigTypeHelm     ConfigType = "helm"
)

// ConfigStatus represents the status of a configuration task
type ConfigStatus string

const (
	ConfigStatusPending    ConfigStatus = "pending"
	ConfigStatusInstalling ConfigStatus = "installing"
	ConfigStatusCompleted  ConfigStatus = "completed"
	ConfigStatusFailed     ConfigStatus = "failed"
)

// ConfigSource represents the source of a configuration task
type ConfigSource string

const (
	ConfigSourceProfile ConfigSource = "profile"
	ConfigSourceAddon   ConfigSource = "addon"
	ConfigSourceCustom  ConfigSource = "custom"
)

// ClusterConfiguration represents a post-deployment configuration task
type ClusterConfiguration struct {
	ID                string        `db:"id" json:"id"`
	ClusterID         string        `db:"cluster_id" json:"cluster_id"`
	ConfigType        ConfigType    `db:"config_type" json:"config_type"`
	ConfigName        string        `db:"config_name" json:"config_name"`
	Status            ConfigStatus  `db:"status" json:"status"`
	ErrorMessage      *string       `db:"error_message" json:"error_message,omitempty"`
	CreatedAt         time.Time     `db:"created_at" json:"created_at"`
	CompletedAt       *time.Time    `db:"completed_at" json:"completed_at,omitempty"`
	Metadata          JobMetadata   `db:"metadata" json:"metadata,omitempty"`
	UserDefined       bool          `db:"user_defined" json:"user_defined"`
	CreatedByUserID   *string       `db:"created_by_user_id" json:"created_by_user_id,omitempty"`
	Source            *ConfigSource `db:"source" json:"source,omitempty"`
}

// DestroyAudit tracks destroy attempts for forensics and reconciliation
type DestroyAudit struct {
	ID                   string     `db:"id" json:"id"`
	ClusterID            string     `db:"cluster_id" json:"cluster_id"`
	JobID                string     `db:"job_id" json:"job_id"`
	WorkerID             string     `db:"worker_id" json:"worker_id"`
	DestroyStartedAt     time.Time  `db:"destroy_started_at" json:"destroy_started_at"`
	LastVerifiedAt       *time.Time `db:"last_verified_at" json:"last_verified_at,omitempty"`
	VerificationPassed   *bool      `db:"verification_passed" json:"verification_passed,omitempty"`
	LastResourcePresent  *string    `db:"last_resource_present" json:"last_resource_present,omitempty"`
	TerminalReason       *string    `db:"terminal_reason" json:"terminal_reason,omitempty"`
	ResourcesSnapshot    JobMetadata `db:"resources_snapshot" json:"resources_snapshot,omitempty"`
	VerificationSnapshot JobMetadata `db:"verification_snapshot" json:"verification_snapshot,omitempty"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
	CompletedAt          *time.Time `db:"completed_at" json:"completed_at,omitempty"`
}
