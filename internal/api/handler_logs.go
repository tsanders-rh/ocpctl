package api

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// LogHandler handles deployment log API endpoints
type LogHandler struct {
	store *store.Store
}

// NewLogHandler creates a new log handler
func NewLogHandler(s *store.Store) *LogHandler {
	return &LogHandler{
		store: s,
	}
}

// GetClusterLogs handles GET /api/v1/clusters/:id/logs
// Returns deployment logs for a cluster with cursor-based pagination
func (h *LogHandler) GetClusterLogs(c echo.Context) error {
	ctx := c.Request().Context()
	clusterID := c.Param("id")

	// Get cluster and verify access
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "Cluster not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Check authorization - same pattern as cluster endpoints
	if err := checkClusterAccess(c, cluster); err != nil {
		return err
	}

	// Parse query parameters
	jobID := c.QueryParam("job_id")

	// If no job_id specified, default to latest CREATE job for this cluster
	if jobID == "" {
		jobs, err := h.store.Jobs.ListByClusterID(ctx, clusterID)
		if err != nil {
			return LogAndReturnGenericError(c, err)
		}

		// Find the most recent CREATE job
		for _, job := range jobs {
			if job.JobType == types.JobTypeCreate {
				jobID = job.ID
				break
			}
		}

		// If still no job ID, return empty logs
		if jobID == "" {
			return c.JSON(http.StatusOK, map[string]interface{}{
				"logs": []types.DeploymentLog{},
				"meta": map[string]interface{}{
					"cluster_id":     clusterID,
					"job_id":         "",
					"after_sequence": 0,
					"limit":          0,
					"count":          0,
					"stats": map[string]interface{}{
						"total_lines":  0,
						"error_count":  0,
						"warn_count":   0,
						"last_updated": nil,
					},
				},
			})
		}
	}

	// Parse pagination parameters
	afterSequence := parseInt64Param(c.QueryParam("after_sequence"), 0)
	limit := parseIntParam(c.QueryParam("limit"), 500)
	if limit > 2000 {
		limit = 2000
	}
	if limit < 1 {
		limit = 500
	}

	// Fetch logs from database
	logs, err := h.store.DeploymentLogs.GetLogs(ctx, clusterID, jobID, afterSequence, limit)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Get log statistics
	stats, err := h.store.DeploymentLogs.GetLogStats(ctx, clusterID, jobID)
	if err != nil {
		// Non-fatal, log and continue
		c.Logger().Warnf("Failed to get log stats: %v", err)
		stats = &types.DeploymentLogStats{}
	}

	// Return logs with metadata
	return c.JSON(http.StatusOK, map[string]interface{}{
		"logs": logs,
		"meta": map[string]interface{}{
			"cluster_id":     clusterID,
			"job_id":         jobID,
			"after_sequence": afterSequence,
			"limit":          limit,
			"count":          len(logs),
			"stats":          stats,
		},
	})
}

// checkClusterAccess verifies the user has access to the cluster
// Extracted to package-level function so it can be reused
func checkClusterAccess(c echo.Context, cluster *types.Cluster) error {
	// Admins can access all clusters
	if auth.IsAdmin(c) {
		return nil
	}

	// Check if user owns this cluster
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	if cluster.OwnerID != userID {
		return ErrorForbidden(c, "You do not have access to this cluster")
	}

	return nil
}

// parseInt64Param parses an int64 query parameter with a default value
func parseInt64Param(param string, defaultValue int64) int64 {
	if param == "" {
		return defaultValue
	}

	value, err := strconv.ParseInt(param, 10, 64)
	if err != nil {
		return defaultValue
	}

	return value
}

// parseIntParam parses an int query parameter with a default value
func parseIntParam(param string, defaultValue int) int {
	if param == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(param)
	if err != nil {
		return defaultValue
	}

	return value
}
