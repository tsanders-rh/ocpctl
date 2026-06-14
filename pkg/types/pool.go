package types

import (
	"encoding/json"
	"time"
)

// PoolState represents the lifecycle state of a cluster in a pool
type PoolState string

const (
	PoolStateReady        PoolState = "READY"        // Available for lease
	PoolStateLeased       PoolState = "LEASED"       // Currently leased to a user/job
	PoolStateProvisioning PoolState = "PROVISIONING" // Being created for pool
	PoolStateCleaning     PoolState = "CLEANING"     // Being sanitized after release
	PoolStateExpired      PoolState = "EXPIRED"      // Exceeded max age, pending refresh
)

// ClusterPool represents a pre-provisioned pool of clusters
type ClusterPool struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	DisplayName string    `json:"display_name" db:"display_name"`
	Description string    `json:"description,omitempty" db:"description"`
	Profile     string    `json:"profile" db:"profile"`

	// Pool sizing
	TargetSize int `json:"target_size" db:"target_size"`
	MinSize    int `json:"min_size" db:"min_size"`
	MaxSize    int `json:"max_size" db:"max_size"`

	// Lease configuration
	DefaultLeaseDurationHours int  `json:"default_lease_duration_hours" db:"default_lease_duration_hours"` // Default lease duration (enforced by ServiceAccount token TTL)
	MaxLeaseDurationHours     int  `json:"max_lease_duration_hours" db:"max_lease_duration_hours"`
	AutoReleaseEnabled        bool `json:"auto_release_enabled" db:"auto_release_enabled"`

	// Cluster lifecycle
	MaxClusterAgeDays  int  `json:"max_cluster_age_days" db:"max_cluster_age_days"`
	AutoRefreshEnabled bool `json:"auto_refresh_enabled" db:"auto_refresh_enabled"`

	// Scheduling (work hours mode)
	ScheduledMode      bool   `json:"scheduled_mode" db:"scheduled_mode"`
	ScheduleTimezone   string `json:"schedule_timezone,omitempty" db:"schedule_timezone"`
	ScheduleStartHour  int    `json:"schedule_start_hour,omitempty" db:"schedule_start_hour"`
	ScheduleEndHour    int    `json:"schedule_end_hour,omitempty" db:"schedule_end_hour"`
	ScheduleDaysOfWeek []int  `json:"schedule_days_of_week,omitempty" db:"schedule_days_of_week"`

	// Configuration overrides (stored as JSONB in database)
	ClusterConfig map[string]interface{} `json:"cluster_config,omitempty" db:"cluster_config"`

	// Metadata
	Enabled   bool      `json:"enabled" db:"enabled"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty" db:"created_by"`
}

// ClusterPoolStats contains real-time statistics about a cluster pool
type ClusterPoolStats struct {
	PoolID   string `json:"pool_id"`
	PoolName string `json:"pool_name"`

	// Cluster counts by state
	TotalClusters        int `json:"total_clusters"`
	ReadyClusters        int `json:"ready_clusters"`
	LeasedClusters       int `json:"leased_clusters"`
	ProvisioningClusters int `json:"provisioning_clusters"`
	CleaningClusters     int `json:"cleaning_clusters"`
	ExpiredClusters      int `json:"expired_clusters"`

	// Utilization metrics
	UtilizationPercent float64 `json:"utilization_percent"` // (leased / (ready + leased)) * 100
	CapacityPercent    float64 `json:"capacity_percent"`    // (total / target_size) * 100

	// Age metrics
	OldestClusterAge time.Duration `json:"oldest_cluster_age,omitempty" swaggertype:"integer"`
	AvgClusterAge    time.Duration `json:"avg_cluster_age,omitempty" swaggertype:"integer"`

	// Lease metrics
	ActiveLeases   int           `json:"active_leases"`
	AvgLeaseDuration time.Duration `json:"avg_lease_duration,omitempty" swaggertype:"integer"`

	// Last update
	ComputedAt time.Time `json:"computed_at"`
}

// LeaseRequest represents a request to lease a cluster from a pool
type LeaseRequest struct {
	LeasedBy string                 `json:"leased_by"` // User, service account, or job ID
	Duration *int                   `json:"duration_hours,omitempty"` // Override default lease duration
	Metadata map[string]interface{} `json:"metadata,omitempty"` // Custom metadata (job_id, build_url, etc.)
}

// LeaseResponse contains information about a leased cluster
type LeaseResponse struct {
	ClusterID      string                 `json:"cluster_id"`
	ClusterName    string                 `json:"cluster_name"`
	LeasedBy       string                 `json:"leased_by"`
	LeasedAt       time.Time              `json:"leased_at"`
	LeaseExpiresAt time.Time              `json:"lease_expires_at"`
	LeaseMetadata  map[string]interface{} `json:"lease_metadata,omitempty"`

	// Cluster access information
	APIUrl          string     `json:"api_url,omitempty"`
	ConsoleUrl      string     `json:"console_url,omitempty"`
	KubeconfigPath  string     `json:"kubeconfig_path,omitempty"`
	SAToken         string     `json:"sa_token,omitempty"`
	SATokenExpiresAt *time.Time `json:"sa_token_expires_at,omitempty"`
	OcLoginCommand  string     `json:"oc_login_command,omitempty"`
}

// ReleaseRequest represents a request to release a leased cluster back to the pool
type ReleaseRequest struct {
	Force bool `json:"force,omitempty"` // Force release even if cluster has issues
}

// CreatePoolRequest represents a request to create a new cluster pool
type CreatePoolRequest struct {
	Name        string `json:"name" validate:"required,min=3,max=50,alphanum-dash"`
	DisplayName string `json:"display_name" validate:"required"`
	Description string `json:"description,omitempty"`
	Profile     string `json:"profile" validate:"required"`

	// Pool sizing
	TargetSize int `json:"target_size" validate:"required,min=1,max=50"`
	MinSize    int `json:"min_size,omitempty" validate:"omitempty,min=1"`
	MaxSize    int `json:"max_size,omitempty" validate:"omitempty,min=1"`

	// Lease configuration
	DefaultLeaseDurationHours *int  `json:"default_lease_duration_hours,omitempty"`
	MaxLeaseDurationHours     *int  `json:"max_lease_duration_hours,omitempty"`
	AutoReleaseEnabled        *bool `json:"auto_release_enabled,omitempty"`

	// Cluster lifecycle
	MaxClusterAgeDays  *int  `json:"max_cluster_age_days,omitempty"`
	AutoRefreshEnabled *bool `json:"auto_refresh_enabled,omitempty"`

	// Scheduling
	ScheduledMode      *bool  `json:"scheduled_mode,omitempty"`
	ScheduleTimezone   string `json:"schedule_timezone,omitempty"`
	ScheduleStartHour  *int   `json:"schedule_start_hour,omitempty"`
	ScheduleEndHour    *int   `json:"schedule_end_hour,omitempty"`
	ScheduleDaysOfWeek []int  `json:"schedule_days_of_week,omitempty"`

	// Configuration overrides
	ClusterConfig map[string]interface{} `json:"cluster_config,omitempty"`
}

// UpdatePoolRequest represents a request to update a cluster pool
type UpdatePoolRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Description *string `json:"description,omitempty"`

	// Pool sizing
	TargetSize *int `json:"target_size,omitempty" validate:"omitempty,min=1,max=50"`
	MinSize    *int `json:"min_size,omitempty" validate:"omitempty,min=1"`
	MaxSize    *int `json:"max_size,omitempty" validate:"omitempty,min=1"`

	// Lease configuration
	DefaultLeaseDurationHours *int  `json:"default_lease_duration_hours,omitempty"`
	MaxLeaseDurationHours     *int  `json:"max_lease_duration_hours,omitempty"`
	AutoReleaseEnabled        *bool `json:"auto_release_enabled,omitempty"`

	// Cluster lifecycle
	MaxClusterAgeDays  *int  `json:"max_cluster_age_days,omitempty"`
	AutoRefreshEnabled *bool `json:"auto_refresh_enabled,omitempty"`

	// Scheduling
	ScheduledMode      *bool   `json:"scheduled_mode,omitempty"`
	ScheduleTimezone   *string `json:"schedule_timezone,omitempty"`
	ScheduleStartHour  *int    `json:"schedule_start_hour,omitempty"`
	ScheduleEndHour    *int    `json:"schedule_end_hour,omitempty"`
	ScheduleDaysOfWeek []int   `json:"schedule_days_of_week,omitempty"`

	// Configuration overrides
	ClusterConfig map[string]interface{} `json:"cluster_config,omitempty"`

	// Enabled state
	Enabled *bool `json:"enabled,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for ClusterPool
// This handles the JSONB cluster_config field
func (p *ClusterPool) MarshalJSON() ([]byte, error) {
	type Alias ClusterPool
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(p),
	})
}

// IsWithinSchedule checks if the current time is within the pool's scheduled hours
func (p *ClusterPool) IsWithinSchedule(now time.Time) bool {
	if !p.ScheduledMode {
		return true // Always within schedule if not in scheduled mode
	}

	// Load timezone
	loc, err := time.LoadLocation(p.ScheduleTimezone)
	if err != nil {
		loc = time.UTC
	}
	nowInTZ := now.In(loc)

	// Check day of week (0 = Sunday, 6 = Saturday)
	weekday := int(nowInTZ.Weekday())
	dayAllowed := false
	for _, allowedDay := range p.ScheduleDaysOfWeek {
		if allowedDay == weekday {
			dayAllowed = true
			break
		}
	}
	if !dayAllowed {
		return false
	}

	// Check hour
	hour := nowInTZ.Hour()
	if p.ScheduleStartHour <= p.ScheduleEndHour {
		// Normal case: 8am-6pm
		return hour >= p.ScheduleStartHour && hour < p.ScheduleEndHour
	} else {
		// Wraparound case: 10pm-2am
		return hour >= p.ScheduleStartHour || hour < p.ScheduleEndHour
	}
}

// NeedsReplenishment checks if the pool needs more clusters
func (p *ClusterPool) NeedsReplenishment(readyCount int) bool {
	return readyCount < p.MinSize
}

// CanProvisionMore checks if the pool can provision more clusters
func (p *ClusterPool) CanProvisionMore(totalCount int) bool {
	return totalCount < p.MaxSize
}
