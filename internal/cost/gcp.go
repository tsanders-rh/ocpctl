package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// GCPCostTracker tracks actual GCP costs using the Cloud Billing API
type GCPCostTracker struct {
	project       string
	billingEnabled bool
}

// NewGCPCostTracker creates a new GCP cost tracker
func NewGCPCostTracker() *GCPCostTracker {
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		log.Printf("[GCP Cost Tracker] GCP_PROJECT not set, cost tracking disabled")
		return &GCPCostTracker{
			billingEnabled: false,
		}
	}

	// Check if billing export is configured
	// GCP billing export requires:
	// 1. BigQuery dataset with billing export enabled
	// 2. Appropriate IAM permissions
	billingDataset := os.Getenv("GCP_BILLING_DATASET")
	billingEnabled := billingDataset != ""

	if !billingEnabled {
		log.Printf("[GCP Cost Tracker] GCP_BILLING_DATASET not set, using estimate-based tracking only")
	}

	return &GCPCostTracker{
		project:       project,
		billingEnabled: billingEnabled,
	}
}

// GCPCostSummary represents cost summary for a cluster
type GCPCostSummary struct {
	ClusterID   string
	ClusterName string
	TotalCost   float64
	Period      string
	Breakdown   map[string]float64 // Service -> Cost
}

// GetClusterCosts retrieves actual costs from GCP Billing for a specific cluster
// Falls back to estimate-based calculation if billing API is not available
func (t *GCPCostTracker) GetClusterCosts(ctx context.Context, cluster *types.Cluster, startDate, endDate time.Time) (*GCPCostSummary, error) {
	if !t.billingEnabled {
		// Return estimate-based cost
		return t.getEstimatedCosts(cluster, startDate, endDate)
	}

	// Query BigQuery billing export for actual costs
	return t.queryBillingData(ctx, cluster, startDate, endDate)
}

// getEstimatedCosts calculates estimated costs based on cluster state and profile
func (t *GCPCostTracker) getEstimatedCosts(cluster *types.Cluster, startDate, endDate time.Time) (*GCPCostSummary, error) {
	// This is a simplified estimate - actual implementation should:
	// 1. Load profile to get EstimatedHourlyCost
	// 2. Calculate hours in period
	// 3. Account for cluster state changes (hibernation)

	hours := endDate.Sub(startDate).Hours()

	// Use a default estimate - this should be improved to use actual profile data
	var estimatedHourlyCost float64
	switch cluster.ClusterType {
	case types.ClusterTypeGKE:
		// GKE Standard: 3 e2-medium nodes at ~$0.0168/hr each = ~$0.05/hr
		estimatedHourlyCost = 0.05
	case types.ClusterTypeOpenShift:
		// OpenShift on GCP: Similar to AWS, ~$0.30-0.40/hr
		estimatedHourlyCost = 0.35
	default:
		estimatedHourlyCost = 0.10
	}

	totalCost := estimatedHourlyCost * hours

	return &GCPCostSummary{
		ClusterID:   cluster.ID,
		ClusterName: cluster.Name,
		TotalCost:   totalCost,
		Period:      fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02")),
		Breakdown: map[string]float64{
			"compute": totalCost * 0.7,
			"storage": totalCost * 0.2,
			"network": totalCost * 0.1,
		},
	}, nil
}

// queryBillingData queries GCP BigQuery billing export for actual costs
func (t *GCPCostTracker) queryBillingData(ctx context.Context, cluster *types.Cluster, startDate, endDate time.Time) (*GCPCostSummary, error) {
	billingDataset := os.Getenv("GCP_BILLING_DATASET")
	billingTable := os.Getenv("GCP_BILLING_TABLE")
	if billingTable == "" {
		billingTable = "gcp_billing_export_v1" // Default table name
	}

	// Build BigQuery SQL query to get costs for this cluster
	// Filter by cluster-id or cluster-name label
	query := fmt.Sprintf(`
		SELECT
			service.description AS service,
			SUM(cost) AS total_cost
		FROM %s.%s
		WHERE
			DATE(usage_start_time) >= '%s'
			AND DATE(usage_start_time) <= '%s'
			AND (
				labels.value = '%s'  -- cluster-id label
				OR labels.value = '%s'  -- cluster-name label
			)
		GROUP BY service
	`, billingDataset, billingTable,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
		cluster.ID,
		cluster.Name)

	// Execute BigQuery query using bq command-line tool
	cmd := exec.CommandContext(ctx, "bq", "query",
		"--use_legacy_sql=false",
		"--format=json",
		"--project_id", t.project,
		query)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query billing data: %w", err)
	}

	// Parse query results
	var results []struct {
		Service    string  `json:"service"`
		TotalCost  float64 `json:"total_cost"`
	}

	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to parse billing query results: %w", err)
	}

	// Aggregate costs
	breakdown := make(map[string]float64)
	var totalCost float64

	for _, result := range results {
		breakdown[result.Service] = result.TotalCost
		totalCost += result.TotalCost
	}

	return &GCPCostSummary{
		ClusterID:   cluster.ID,
		ClusterName: cluster.Name,
		TotalCost:   totalCost,
		Period:      fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02")),
		Breakdown:   breakdown,
	}, nil
}

// GetProjectCosts retrieves total GCP project costs for a time period
func (t *GCPCostTracker) GetProjectCosts(ctx context.Context, startDate, endDate time.Time) (float64, error) {
	if !t.billingEnabled {
		return 0, fmt.Errorf("billing API not enabled")
	}

	billingDataset := os.Getenv("GCP_BILLING_DATASET")
	billingTable := os.Getenv("GCP_BILLING_TABLE")
	if billingTable == "" {
		billingTable = "gcp_billing_export_v1"
	}

	query := fmt.Sprintf(`
		SELECT
			SUM(cost) AS total_cost
		FROM %s.%s
		WHERE
			DATE(usage_start_time) >= '%s'
			AND DATE(usage_start_time) <= '%s'
			AND project.id = '%s'
	`, billingDataset, billingTable,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
		t.project)

	cmd := exec.CommandContext(ctx, "bq", "query",
		"--use_legacy_sql=false",
		"--format=json",
		"--project_id", t.project,
		query)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to query project costs: %w", err)
	}

	var results []struct {
		TotalCost float64 `json:"total_cost"`
	}

	if err := json.Unmarshal(output, &results); err != nil {
		return 0, fmt.Errorf("failed to parse cost query results: %w", err)
	}

	if len(results) == 0 {
		return 0, nil
	}

	return results[0].TotalCost, nil
}

// GetCostsByLabel retrieves costs aggregated by a specific label (e.g., cluster-name, profile)
func (t *GCPCostTracker) GetCostsByLabel(ctx context.Context, labelKey string, startDate, endDate time.Time) (map[string]float64, error) {
	if !t.billingEnabled {
		return nil, fmt.Errorf("billing API not enabled")
	}

	billingDataset := os.Getenv("GCP_BILLING_DATASET")
	billingTable := os.Getenv("GCP_BILLING_TABLE")
	if billingTable == "" {
		billingTable = "gcp_billing_export_v1"
	}

	// Convert label key to GCP format (hyphen instead of underscore)
	gcpLabelKey := strings.ReplaceAll(labelKey, "_", "-")

	query := fmt.Sprintf(`
		SELECT
			labels.value AS label_value,
			SUM(cost) AS total_cost
		FROM %s.%s,
		UNNEST(labels) AS labels
		WHERE
			DATE(usage_start_time) >= '%s'
			AND DATE(usage_start_time) <= '%s'
			AND project.id = '%s'
			AND labels.key = '%s'
		GROUP BY label_value
		ORDER BY total_cost DESC
	`, billingDataset, billingTable,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
		t.project,
		gcpLabelKey)

	cmd := exec.CommandContext(ctx, "bq", "query",
		"--use_legacy_sql=false",
		"--format=json",
		"--project_id", t.project,
		query)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query costs by label: %w", err)
	}

	var results []struct {
		LabelValue string  `json:"label_value"`
		TotalCost  float64 `json:"total_cost"`
	}

	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to parse label cost query results: %w", err)
	}

	costs := make(map[string]float64)
	for _, result := range results {
		costs[result.LabelValue] = result.TotalCost
	}

	return costs, nil
}

// GetServiceCosts retrieves costs broken down by GCP service
func (t *GCPCostTracker) GetServiceCosts(ctx context.Context, startDate, endDate time.Time) (map[string]float64, error) {
	if !t.billingEnabled {
		return nil, fmt.Errorf("billing API not enabled")
	}

	billingDataset := os.Getenv("GCP_BILLING_DATASET")
	billingTable := os.Getenv("GCP_BILLING_TABLE")
	if billingTable == "" {
		billingTable = "gcp_billing_export_v1"
	}

	query := fmt.Sprintf(`
		SELECT
			service.description AS service,
			SUM(cost) AS total_cost
		FROM %s.%s
		WHERE
			DATE(usage_start_time) >= '%s'
			AND DATE(usage_start_time) <= '%s'
			AND project.id = '%s'
		GROUP BY service
		ORDER BY total_cost DESC
	`, billingDataset, billingTable,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
		t.project)

	cmd := exec.CommandContext(ctx, "bq", "query",
		"--use_legacy_sql=false",
		"--format=json",
		"--project_id", t.project,
		query)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query service costs: %w", err)
	}

	var results []struct {
		Service   string  `json:"service"`
		TotalCost float64 `json:"total_cost"`
	}

	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to parse service cost query results: %w", err)
	}

	costs := make(map[string]float64)
	for _, result := range results {
		costs[result.Service] = result.TotalCost
	}

	return costs, nil
}
