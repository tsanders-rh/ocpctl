package types

import "time"

// ProfileDeploymentMetrics represents deployment time statistics for a specific profile.
// These metrics are calculated from the last 30 successful CREATE jobs and updated periodically
// by the janitor service. They help users understand typical deployment times when creating clusters.
type ProfileDeploymentMetrics struct {
	Profile              string     `json:"profile"`
	AvgDurationSeconds   int        `json:"avg_duration_seconds"`
	MinDurationSeconds   int        `json:"min_duration_seconds"`
	MaxDurationSeconds   int        `json:"max_duration_seconds"`
	P50DurationSeconds   *int       `json:"p50_duration_seconds,omitempty"`   // Median (50th percentile)
	P95DurationSeconds   *int       `json:"p95_duration_seconds,omitempty"`   // 95th percentile
	SampleCount          int        `json:"sample_count"`                     // Number of samples used (max 30)
	SuccessCount         int        `json:"success_count"`                    // Total successful deployments tracked
	LastDeploymentAt     *time.Time `json:"last_deployment_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// HasSufficientData returns true if there are enough samples to provide a meaningful estimate.
// We require at least 5 successful deployments to show time estimates to users.
func (m *ProfileDeploymentMetrics) HasSufficientData() bool {
	return m.SampleCount >= 5
}

// AvgDurationMinutes returns the average deployment duration in minutes (rounded).
func (m *ProfileDeploymentMetrics) AvgDurationMinutes() int {
	if m.AvgDurationSeconds == 0 {
		return 0
	}
	return (m.AvgDurationSeconds + 30) / 60 // Round to nearest minute
}

// P50DurationMinutes returns the median deployment duration in minutes (rounded).
// Returns 0 if P50 data is not available.
func (m *ProfileDeploymentMetrics) P50DurationMinutes() int {
	if m.P50DurationSeconds == nil {
		return 0
	}
	return (*m.P50DurationSeconds + 30) / 60 // Round to nearest minute
}

// P95DurationMinutes returns the 95th percentile deployment duration in minutes (rounded).
// Returns 0 if P95 data is not available.
func (m *ProfileDeploymentMetrics) P95DurationMinutes() int {
	if m.P95DurationSeconds == nil {
		return 0
	}
	return (*m.P95DurationSeconds + 30) / 60 // Round to nearest minute
}
