package types

import "time"

// UsageReport is the platform-wide, date-ranged usage/cost report returned by
// GET /api/v1/admin/reports/usage. All figures are estimates derived from
// profile hourly rates and cluster runtime within the requested window.
type UsageReport struct {
	StartDate   string    `json:"start_date"` // YYYY-MM-DD (inclusive)
	EndDate     string    `json:"end_date"`   // YYYY-MM-DD (inclusive)
	GeneratedAt time.Time `json:"generated_at"`

	Cost      UsageCostSummary `json:"cost"`
	Profiles  []ProfileUsage   `json:"profiles"` // sorted by cluster count desc
	Users     []UserUsage      `json:"users"`    // sorted by est. cost desc
	Versions  []VersionUsage   `json:"versions"` // OpenShift-family versions, sorted by cluster count desc
	Addons    []AddonUsage     `json:"addons"`   // sorted by cluster count desc
	Lifecycle LifecycleStats   `json:"lifecycle"`
}

// UsageCostSummary captures the headline cost numbers for the report window.
type UsageCostSummary struct {
	TotalCost             float64           `json:"total_cost"`                        // estimated $ across all clusters in-window
	TotalRuntimeHours     float64           `json:"total_runtime_hours"`               // summed cluster runtime hours in-window
	ClustersActive        int               `json:"clusters_active"`                   // clusters active at any point in-window
	PriorPeriodComparison *PeriodComparison `json:"prior_period_comparison,omitempty"` // vs. the equally-sized preceding window
}

// ProfileUsage aggregates usage for a single profile within the window. Clusters
// holds the individual clusters that make up the aggregate, for drill-down.
type ProfileUsage struct {
	Profile       string         `json:"profile"`
	ClusterCount  int            `json:"cluster_count"`
	RuntimeHours  float64        `json:"runtime_hours"`
	EstimatedCost float64        `json:"estimated_cost"`
	Clusters      []ClusterUsage `json:"clusters"` // per-cluster detail, sorted by est. cost desc
}

// ClusterUsage is the per-cluster detail behind an aggregate row, with the
// cluster's runtime and estimated cost over the report window.
type ClusterUsage struct {
	Name          string     `json:"name"`
	Owner         string     `json:"owner"` // email when resolvable, else owner_id
	Region        string     `json:"region"`
	Status        string     `json:"status"`
	ClusterType   string     `json:"cluster_type"`
	CreatedAt     time.Time  `json:"created_at"`
	DestroyedAt   *time.Time `json:"destroyed_at,omitempty"`
	RuntimeHours  float64    `json:"runtime_hours"`
	EstimatedCost float64    `json:"estimated_cost"`
}

// UserUsage aggregates usage for a single owner within the window.
type UserUsage struct {
	Owner         string  `json:"owner"` // email when resolvable, else owner_id
	ClusterCount  int     `json:"cluster_count"`
	RuntimeHours  float64 `json:"runtime_hours"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// VersionUsage aggregates usage for a single OpenShift-family version within the
// window. Only clusters whose type is openshift/rosa/aro (i.e. that report an
// OpenShift version rather than a plain Kubernetes version) are counted.
type VersionUsage struct {
	Version       string  `json:"version"`
	ClusterCount  int     `json:"cluster_count"`
	RuntimeHours  float64 `json:"runtime_hours"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// AddonUsage aggregates usage for a single addon within the window. A cluster
// contributes to every addon it selected, so ClusterCount is "clusters that use
// this addon" and the runtime/cost figures are that set's combined footprint.
type AddonUsage struct {
	Addon         string  `json:"addon"`
	ClusterCount  int     `json:"cluster_count"`
	RuntimeHours  float64 `json:"runtime_hours"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// LifecycleStats summarizes cluster lifecycle activity in the window. Counts are
// derived from jobs created in-window; breakdowns are over clusters active
// in-window.
type LifecycleStats struct {
	Created           int     `json:"created"`             // CREATE jobs that succeeded
	Destroyed         int     `json:"destroyed"`           // DESTROY/JANITOR_DESTROY jobs that succeeded
	Hibernated        int     `json:"hibernated"`          // HIBERNATE jobs that succeeded
	CreateSuccess     int     `json:"create_success"`      // successful CREATE jobs
	CreateFailure     int     `json:"create_failure"`      // failed CREATE jobs
	CreateSuccessRate float64 `json:"create_success_rate"` // 0..1, over completed CREATE jobs
	AvgLifetimeHours  float64 `json:"avg_lifetime_hours"`  // avg lifetime of clusters active in-window

	ByPlatform    map[string]int `json:"by_platform"`
	ByClusterType map[string]int `json:"by_cluster_type"`
	ByStatus      map[string]int `json:"by_status"`
}

// JobStats is the raw aggregate of jobs within a window, keyed by job type and
// status, produced by the store and folded into LifecycleStats by the handler.
type JobStats struct {
	// Counts[jobType][status] = count
	Counts map[string]map[string]int `json:"counts"`
}
