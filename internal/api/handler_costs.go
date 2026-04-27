package api

import (
	"context"
	"log"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/cost"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// CostHandler handles cost tracking API endpoints
type CostHandler struct {
	store          *store.Store
	gcpCostTracker *cost.GCPCostTracker
}

// NewCostHandler creates a new cost handler
func NewCostHandler(s *store.Store) *CostHandler {
	return &CostHandler{
		store:          s,
		gcpCostTracker: cost.NewGCPCostTracker(),
	}
}

// GetGCPCosts handles GET /api/v1/costs/gcp
//
//	@Summary		Get GCP costs
//	@Description	Retrieves GCP billing costs for a time period, aggregated by cluster or service
//	@Tags			costs
//	@Produce		json
//	@Param			start_date	query		string	false	"Start date (YYYY-MM-DD)"	default(30 days ago)
//	@Param			end_date	query		string	false	"End date (YYYY-MM-DD)"		default(today)
//	@Param			group_by	query		string	false	"Group by: cluster, service, profile"	default(cluster)
//	@Success		200			{object}	map[string]interface{}
//	@Failure		400			{object}	ErrorResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		500			{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/costs/gcp [get]
func (h *CostHandler) GetGCPCosts(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse date range
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -30) // Default to last 30 days

	if startDateStr := c.QueryParam("start_date"); startDateStr != "" {
		parsed, err := time.Parse("2006-01-02", startDateStr)
		if err != nil {
			return ErrorBadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
		}
		startDate = parsed
	}

	if endDateStr := c.QueryParam("end_date"); endDateStr != "" {
		parsed, err := time.Parse("2006-01-02", endDateStr)
		if err != nil {
			return ErrorBadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
		}
		endDate = parsed
	}

	groupBy := c.QueryParam("group_by")
	if groupBy == "" {
		groupBy = "cluster"
	}

	var result interface{}
	var err error

	switch groupBy {
	case "cluster":
		result, err = h.getCostsByCluster(ctx, startDate, endDate)
	case "service":
		result, err = h.gcpCostTracker.GetServiceCosts(ctx, startDate, endDate)
	case "profile":
		result, err = h.gcpCostTracker.GetCostsByLabel(ctx, "profile", startDate, endDate)
	default:
		return ErrorBadRequest(c, "Invalid group_by parameter, must be: cluster, service, or profile")
	}

	if err != nil {
		log.Printf("Error getting GCP costs: %v", err)
		return ErrorBadRequest(c, "Failed to retrieve GCP costs: "+err.Error())
	}

	return SuccessOK(c, map[string]interface{}{
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
		"group_by":   groupBy,
		"costs":      result,
	})
}

// getCostsByCluster retrieves costs aggregated by cluster
func (h *CostHandler) getCostsByCluster(ctx context.Context, startDate, endDate time.Time) (map[string]interface{}, error) {
	// Get all GCP clusters
	gcpPlatform := types.PlatformGCP
	clusters, _, err := h.store.Clusters.List(ctx, store.ListFilters{
		Platform: &gcpPlatform,
		Limit:    1000,
	})
	if err != nil {
		return nil, err
	}

	clusterCosts := make(map[string]interface{})
	var totalCost float64

	for _, cluster := range clusters {
		summary, err := h.gcpCostTracker.GetClusterCosts(ctx, cluster, startDate, endDate)
		if err != nil {
			log.Printf("Warning: failed to get costs for cluster %s: %v", cluster.Name, err)
			continue
		}

		clusterCosts[cluster.Name] = map[string]interface{}{
			"cluster_id":  cluster.ID,
			"total_cost":  summary.TotalCost,
			"breakdown":   summary.Breakdown,
			"period":      summary.Period,
			"profile":     cluster.Profile,
			"cluster_type": cluster.ClusterType,
		}

		totalCost += summary.TotalCost
	}

	return map[string]interface{}{
		"clusters":   clusterCosts,
		"total_cost": totalCost,
	}, nil
}

// GetGCPProjectCosts handles GET /api/v1/costs/gcp/project
//
//	@Summary		Get total GCP project costs
//	@Description	Retrieves total GCP billing costs for the project
//	@Tags			costs
//	@Produce		json
//	@Param			start_date	query		string	false	"Start date (YYYY-MM-DD)"	default(30 days ago)
//	@Param			end_date	query		string	false	"End date (YYYY-MM-DD)"		default(today)
//	@Success		200			{object}	map[string]interface{}
//	@Failure		400			{object}	ErrorResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		500			{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/costs/gcp/project [get]
func (h *CostHandler) GetGCPProjectCosts(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse date range
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -30)

	if startDateStr := c.QueryParam("start_date"); startDateStr != "" {
		parsed, err := time.Parse("2006-01-02", startDateStr)
		if err != nil {
			return ErrorBadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
		}
		startDate = parsed
	}

	if endDateStr := c.QueryParam("end_date"); endDateStr != "" {
		parsed, err := time.Parse("2006-01-02", endDateStr)
		if err != nil {
			return ErrorBadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
		}
		endDate = parsed
	}

	totalCost, err := h.gcpCostTracker.GetProjectCosts(ctx, startDate, endDate)
	if err != nil {
		log.Printf("Error getting GCP project costs: %v", err)
		return ErrorBadRequest(c, "Failed to retrieve GCP project costs: "+err.Error())
	}

	return SuccessOK(c, map[string]interface{}{
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
		"total_cost": totalCost,
	})
}
