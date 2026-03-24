package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// OrphanedResourceType represents the type of AWS resource
type OrphanedResourceType string

const (
	OrphanedResourceTypeVPC               OrphanedResourceType = "VPC"
	OrphanedResourceTypeLoadBalancer      OrphanedResourceType = "LoadBalancer"
	OrphanedResourceTypeDNSRecord         OrphanedResourceType = "DNSRecord"
	OrphanedResourceTypeEC2Instance       OrphanedResourceType = "EC2Instance"
	OrphanedResourceTypeHostedZone        OrphanedResourceType = "HostedZone"
	OrphanedResourceTypeIAMRole           OrphanedResourceType = "IAMRole"
	OrphanedResourceTypeOIDCProvider      OrphanedResourceType = "OIDCProvider"
	OrphanedResourceTypeEBSVolume         OrphanedResourceType = "EBSVolume"
	OrphanedResourceTypeElasticIP         OrphanedResourceType = "ElasticIP"
	OrphanedResourceTypeCloudWatchLogGroup OrphanedResourceType = "CloudWatchLogGroup"
)

// OrphanedResourceStatus represents the status of an orphaned resource
type OrphanedResourceStatus string

const (
	OrphanedResourceStatusActive   OrphanedResourceStatus = "ACTIVE"
	OrphanedResourceStatusResolved OrphanedResourceStatus = "RESOLVED"
	OrphanedResourceStatusIgnored  OrphanedResourceStatus = "IGNORED"
)

// OrphanedResourceTags represents AWS tags stored as JSONB
type OrphanedResourceTags map[string]string

// Value implements driver.Valuer for database serialization
func (t OrphanedResourceTags) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}
	return json.Marshal(t)
}

// Scan implements sql.Scanner for database deserialization
func (t *OrphanedResourceTags) Scan(value interface{}) error {
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

	result := make(OrphanedResourceTags)
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}
	*t = result
	return nil
}

// OrphanedResource represents an AWS resource that exists but has no matching cluster
type OrphanedResource struct {
	ID              string                   `db:"id" json:"id"`
	ResourceType    OrphanedResourceType     `db:"resource_type" json:"resource_type"`
	ResourceID      string                   `db:"resource_id" json:"resource_id"`
	ResourceName    string                   `db:"resource_name" json:"resource_name"`
	Region          string                   `db:"region" json:"region"`
	ClusterName     string                   `db:"cluster_name" json:"cluster_name"`
	Tags            OrphanedResourceTags     `db:"tags" json:"tags"`
	FirstDetectedAt time.Time                `db:"first_detected_at" json:"first_detected_at"`
	LastDetectedAt  time.Time                `db:"last_detected_at" json:"last_detected_at"`
	DetectionCount  int                      `db:"detection_count" json:"detection_count"`
	Status          OrphanedResourceStatus   `db:"status" json:"status"`
	ResolvedAt      *time.Time               `db:"resolved_at" json:"resolved_at,omitempty"`
	ResolvedBy      *string                  `db:"resolved_by" json:"resolved_by,omitempty"`
	Notes           *string                  `db:"notes" json:"notes,omitempty"`
	CreatedAt       time.Time                `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time                `db:"updated_at" json:"updated_at"`
}

// OrphanedResourceStats represents summary statistics for orphaned resources
type OrphanedResourceStats struct {
	TotalActive    int            `json:"total_active"`
	TotalResolved  int            `json:"total_resolved"`
	TotalIgnored   int            `json:"total_ignored"`
	ByType         map[string]int `json:"by_type"`
	ByRegion       map[string]int `json:"by_region"`
	OldestDetected *time.Time     `json:"oldest_detected,omitempty"`
}
