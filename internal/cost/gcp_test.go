package cost

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestNewGCPCostTracker(t *testing.T) {
	t.Run("creates tracker when GCP_PROJECT is set", func(t *testing.T) {
		t.Setenv("GCP_PROJECT", "test-project")
		t.Setenv("GCP_BILLING_DATASET", "")

		tracker := NewGCPCostTracker()
		require.NotNil(t, tracker)
		assert.Equal(t, "test-project", tracker.project)
		assert.False(t, tracker.billingEnabled, "billing should be disabled without dataset")
	})

	t.Run("enables billing when dataset is configured", func(t *testing.T) {
		t.Setenv("GCP_PROJECT", "test-project")
		t.Setenv("GCP_BILLING_DATASET", "billing_export")

		tracker := NewGCPCostTracker()
		require.NotNil(t, tracker)
		assert.True(t, tracker.billingEnabled, "billing should be enabled with dataset")
	})

	t.Run("returns tracker without project", func(t *testing.T) {
		t.Setenv("GCP_PROJECT", "")

		tracker := NewGCPCostTracker()
		require.NotNil(t, tracker)
		assert.False(t, tracker.billingEnabled)
	})
}

func TestGCPCostTracker_GetEstimatedCosts(t *testing.T) {
	tracker := &GCPCostTracker{
		project:        "test-project",
		billingEnabled: false,
	}

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) // 24 hours

	tests := []struct {
		name           string
		cluster        *types.Cluster
		expectedCost   float64
		expectedPeriod string
	}{
		{
			name: "GKE cluster 24 hours",
			cluster: &types.Cluster{
				ID:          "test-123",
				Name:        "my-gke",
				ClusterType: types.ClusterTypeGKE,
			},
			expectedCost:   1.20, // 0.05/hr * 24hrs
			expectedPeriod: "2024-01-01 to 2024-01-02",
		},
		{
			name: "OpenShift on GCP 24 hours",
			cluster: &types.Cluster{
				ID:          "test-456",
				Name:        "my-ocp",
				ClusterType: types.ClusterTypeOpenShift,
			},
			expectedCost:   8.40, // 0.35/hr * 24hrs
			expectedPeriod: "2024-01-01 to 2024-01-02",
		},
		{
			name: "GKE cluster with same 24 hour period",
			cluster: &types.Cluster{
				ID:          "test-789",
				Name:        "third-gke",
				ClusterType: types.ClusterTypeGKE,
			},
			expectedCost:   1.20, // 0.05/hr * 24hrs
			expectedPeriod: "2024-01-01 to 2024-01-02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, err := tracker.getEstimatedCosts(tt.cluster, startDate, endDate)
			require.NoError(t, err)
			require.NotNil(t, summary)

			assert.Equal(t, tt.cluster.ID, summary.ClusterID)
			assert.Equal(t, tt.cluster.Name, summary.ClusterName)
			assert.InDelta(t, tt.expectedCost, summary.TotalCost, 0.01)
			assert.Contains(t, summary.Period, tt.expectedPeriod)
			assert.NotEmpty(t, summary.Breakdown)
		})
	}
}

func TestGCPCostTracker_BreakdownPercentages(t *testing.T) {
	tracker := &GCPCostTracker{
		project:        "test-project",
		billingEnabled: false,
	}

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	cluster := &types.Cluster{
		ID:          "test-123",
		Name:        "test-cluster",
		ClusterType: types.ClusterTypeGKE,
	}

	summary, err := tracker.getEstimatedCosts(cluster, startDate, endDate)
	require.NoError(t, err)

	// Verify breakdown percentages add up correctly
	total := summary.Breakdown["compute"] + summary.Breakdown["storage"] + summary.Breakdown["network"]
	assert.InDelta(t, summary.TotalCost, total, 0.01, "breakdown should sum to total cost")

	// Verify expected percentages
	assert.InDelta(t, summary.TotalCost*0.7, summary.Breakdown["compute"], 0.01)
	assert.InDelta(t, summary.TotalCost*0.2, summary.Breakdown["storage"], 0.01)
	assert.InDelta(t, summary.TotalCost*0.1, summary.Breakdown["network"], 0.01)
}

func TestGCPCostTracker_HourlyCostEstimates(t *testing.T) {
	tests := []struct {
		name             string
		clusterType      types.ClusterType
		expectedHourlyCost float64
	}{
		{
			name:             "GKE cluster",
			clusterType:      types.ClusterTypeGKE,
			expectedHourlyCost: 0.05,
		},
		{
			name:             "OpenShift on GCP",
			clusterType:      types.ClusterTypeOpenShift,
			expectedHourlyCost: 0.35,
		},
		{
			name:             "Unknown cluster type",
			clusterType:      "unknown",
			expectedHourlyCost: 0.10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hourlyRate := getEstimatedHourlyCost(tt.clusterType)
			assert.Equal(t, tt.expectedHourlyCost, hourlyRate)
		})
	}
}

func TestGCPCostSummary_Validation(t *testing.T) {
	summary := &GCPCostSummary{
		ClusterID:   "test-123",
		ClusterName: "my-cluster",
		TotalCost:   10.50,
		Period:      "2024-01-01 to 2024-01-31",
		Breakdown: map[string]float64{
			"compute": 7.35,
			"storage": 2.10,
			"network": 1.05,
		},
	}

	assert.NotEmpty(t, summary.ClusterID)
	assert.NotEmpty(t, summary.ClusterName)
	assert.Greater(t, summary.TotalCost, 0.0)
	assert.NotEmpty(t, summary.Breakdown)
}

func TestGCPCostTracker_DateRangeCalculations(t *testing.T) {
	tracker := &GCPCostTracker{
		project:        "test-project",
		billingEnabled: false,
	}

	cluster := &types.Cluster{
		ID:          "test-123",
		Name:        "test",
		ClusterType: types.ClusterTypeGKE,
	}

	tests := []struct {
		name         string
		startDate    time.Time
		endDate      time.Time
		expectedHours float64
	}{
		{
			name:         "1 hour",
			startDate:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			endDate:      time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
			expectedHours: 1.0,
		},
		{
			name:         "24 hours (1 day)",
			startDate:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			endDate:      time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			expectedHours: 24.0,
		},
		{
			name:         "168 hours (1 week)",
			startDate:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			endDate:      time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC),
			expectedHours: 168.0,
		},
		{
			name:         "720 hours (30 days)",
			startDate:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			endDate:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			expectedHours: 720.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, err := tracker.getEstimatedCosts(cluster, tt.startDate, tt.endDate)
			require.NoError(t, err)

			hours := tt.endDate.Sub(tt.startDate).Hours()
			assert.Equal(t, tt.expectedHours, hours)

			expectedCost := 0.05 * tt.expectedHours // GKE hourly rate
			assert.InDelta(t, expectedCost, summary.TotalCost, 0.01)
		})
	}
}

func TestGCPCostTracker_MonthlyCostEstimates(t *testing.T) {
	tracker := &GCPCostTracker{
		project:        "test-project",
		billingEnabled: false,
	}

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC) // 31 days

	tests := []struct {
		name              string
		clusterType       types.ClusterType
		expectedMonthlyCost float64
	}{
		{
			name:              "GKE monthly (31 days)",
			clusterType:       types.ClusterTypeGKE,
			expectedMonthlyCost: 37.2, // 0.05/hr * 24hrs * 31days
		},
		{
			name:              "OpenShift monthly (31 days)",
			clusterType:       types.ClusterTypeOpenShift,
			expectedMonthlyCost: 260.4, // 0.35/hr * 24hrs * 31days
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &types.Cluster{
				ID:          "test-123",
				Name:        "monthly-test",
				ClusterType: tt.clusterType,
			}

			summary, err := tracker.getEstimatedCosts(cluster, startDate, endDate)
			require.NoError(t, err)
			assert.InDelta(t, tt.expectedMonthlyCost, summary.TotalCost, 0.5)
		})
	}
}

// Helper function for testing
func getEstimatedHourlyCost(clusterType types.ClusterType) float64 {
	switch clusterType {
	case types.ClusterTypeGKE:
		return 0.05
	case types.ClusterTypeOpenShift:
		return 0.35
	default:
		return 0.10
	}
}
