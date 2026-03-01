package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// ClusterStatus represents the current state of a cluster
type ClusterStatus string

const (
	ClusterStatusPending    ClusterStatus = "PENDING"
	ClusterStatusCreating   ClusterStatus = "CREATING"
	ClusterStatusReady      ClusterStatus = "READY"
	ClusterStatusDestroying ClusterStatus = "DESTROYING"
	ClusterStatusDestroyed  ClusterStatus = "DESTROYED"
	ClusterStatusFailed     ClusterStatus = "FAILED"
)

// Platform represents the cloud platform
type Platform string

const (
	PlatformAWS      Platform = "aws"
	PlatformIBMCloud Platform = "ibmcloud"
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

	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}

	return json.Unmarshal(bytes, t)
}

// Cluster represents a cluster record in the database
type Cluster struct {
	ID             string        `db:"id" json:"id"`
	Name           string        `db:"name" json:"name"`
	Platform       Platform      `db:"platform" json:"platform"`
	Version        string        `db:"version" json:"version"`
	Profile        string        `db:"profile" json:"profile"`
	Region         string        `db:"region" json:"region"`
	BaseDomain     string        `db:"base_domain" json:"base_domain"`
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
	RequestTags    Tags          `db:"request_tags" json:"request_tags"`
	EffectiveTags  Tags          `db:"effective_tags" json:"effective_tags"`
	SSHPublicKey   *string       `db:"ssh_public_key" json:"ssh_public_key"`
	OffhoursOptIn  bool          `db:"offhours_opt_in" json:"offhours_opt_in"`
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
