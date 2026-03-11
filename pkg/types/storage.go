package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// StorageConfig represents storage configuration stored as JSONB in the clusters table
type StorageConfig struct {
	EFSEnabled      bool    `json:"efs_enabled,omitempty"`
	LocalEFSID      *string `json:"local_efs_id,omitempty"`
	LocalEFSSGID    *string `json:"local_efs_sg_id,omitempty"`
	SharedEFSID     *string `json:"shared_efs_id,omitempty"`
	SharedS3Bucket  *string `json:"shared_s3_bucket,omitempty"`
	StorageGroupID  *string `json:"storage_group_id,omitempty"`
	SecurityGroupID *string `json:"security_group_id,omitempty"`
	AuthMode        *string `json:"auth_mode,omitempty"`        // "sts" or "static"
	IAMRoleARN      *string `json:"iam_role_arn,omitempty"`     // For STS-enabled clusters
}

// Value implements driver.Valuer for database serialization
func (sc StorageConfig) Value() (driver.Value, error) {
	return json.Marshal(sc)
}

// Scan implements sql.Scanner for database deserialization
func (sc *StorageConfig) Scan(value interface{}) error {
	if value == nil {
		*sc = StorageConfig{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		*sc = StorageConfig{}
		return nil
	}

	if err := json.Unmarshal(bytes, sc); err != nil {
		return err
	}

	return nil
}

// StorageGroupStatus represents the status of a storage group
type StorageGroupStatus string

const (
	StorageGroupStatusProvisioning StorageGroupStatus = "PROVISIONING"
	StorageGroupStatusReady        StorageGroupStatus = "READY"
	StorageGroupStatusFailed       StorageGroupStatus = "FAILED"
	StorageGroupStatusDeleting     StorageGroupStatus = "DELETING"
)

// StorageGroupMetadata is arbitrary JSON metadata stored with a storage group
type StorageGroupMetadata map[string]interface{}

// Value implements driver.Valuer for database serialization
func (m StorageGroupMetadata) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner for database deserialization
func (m *StorageGroupMetadata) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}

	return json.Unmarshal(bytes, m)
}

// StorageGroup represents a shared storage resource (EFS + S3) for migration testing
type StorageGroup struct {
	ID                 string                `db:"id" json:"id"`
	Name               string                `db:"name" json:"name"`
	EFSID              *string               `db:"efs_id" json:"efs_id,omitempty"`
	EFSSecurityGroupID *string               `db:"efs_security_group_id" json:"efs_security_group_id,omitempty"`
	S3Bucket           *string               `db:"s3_bucket" json:"s3_bucket,omitempty"`
	Region             string                `db:"region" json:"region"`
	Status             StorageGroupStatus    `db:"status" json:"status"`
	Metadata           StorageGroupMetadata  `db:"metadata" json:"metadata,omitempty"`
	CreatedAt          time.Time             `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time             `db:"updated_at" json:"updated_at"`
}

// ClusterStorageLinkRole represents the role of a cluster in a storage group
type ClusterStorageLinkRole string

const (
	ClusterStorageLinkRoleSource ClusterStorageLinkRole = "source"
	ClusterStorageLinkRoleTarget ClusterStorageLinkRole = "target"
	ClusterStorageLinkRoleShared ClusterStorageLinkRole = "shared"
)

// ClusterStorageLink represents a link between a cluster and a storage group
type ClusterStorageLink struct {
	ID             string                  `db:"id" json:"id"`
	ClusterID      string                  `db:"cluster_id" json:"cluster_id"`
	StorageGroupID string                  `db:"storage_group_id" json:"storage_group_id"`
	Role           ClusterStorageLinkRole  `db:"role" json:"role"`
	CreatedAt      time.Time               `db:"created_at" json:"created_at"`
}
