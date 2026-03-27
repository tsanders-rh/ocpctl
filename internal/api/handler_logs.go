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
//
//	@Summary		Get cluster deployment logs
//	@Description	Returns deployment logs for a cluster with cursor-based pagination. Returns logs from all jobs (CREATE, POST_CONFIGURE, etc.) if job_id not specified.
//	@Tags			Clusters
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string	true	"Cluster ID"
//	@Param			job_id	query		string	false	"Job ID to get logs for (returns logs from all jobs if not specified)"
//	@Param			cursor	query		string	false	"Cursor for pagination"
//	@Param			limit	query		int		false	"Number of log lines to return (default 500)"
//	@Success		200		{object}	map[string]interface{}
//	@Failure		403		{object}	map[string]string	"Forbidden - not cluster owner"
//	@Failure		404		{object}	map[string]string	"Cluster or logs not found"
//	@Failure		500		{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/logs [get]
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

	// Parse pagination parameters
	// For backwards compatibility, accept both after_sequence (old) and after_id (new)
	// When no job_id is specified, we use after_id for global ordering
	afterSequence := parseInt64Param(c.QueryParam("after_sequence"), 0)
	afterID := parseInt64Param(c.QueryParam("after_id"), 0)
	limit := parseIntParam(c.QueryParam("limit"), 500)
	if limit > 2000 {
		limit = 2000
	}
	if limit < 1 {
		limit = 500
	}

	// If no job_id specified, return logs from ALL jobs for this cluster
	// This allows UI to see CREATE, POST_CONFIGURE, and other job logs together
	if jobID == "" {
		// Use after_id for pagination (not after_sequence, since sequence is per-job)
		// Fetch all logs from database
		logs, err := h.store.DeploymentLogs.GetAllLogs(ctx, clusterID, afterID, limit)
		if err != nil {
			return LogAndReturnGenericError(c, err)
		}

		// Get log statistics for all jobs
		stats, err := h.store.DeploymentLogs.GetAllLogStats(ctx, clusterID)
		if err != nil {
			// Non-fatal, log and continue
			c.Logger().Warnf("Failed to get all log stats: %v", err)
			stats = &types.DeploymentLogStats{}
		}

		// Return logs with metadata
		return c.JSON(http.StatusOK, map[string]interface{}{
			"logs": logs,
			"meta": map[string]interface{}{
				"cluster_id":     clusterID,
				"job_id":         "", // Empty indicates all jobs
				"after_id":       afterID,
				"after_sequence": afterSequence, // Kept for backwards compatibility
				"limit":          limit,
				"count":          len(logs),
				"stats":          stats,
			},
		})
	}

	// Fetch logs from database for specific job
	logs, err := h.store.DeploymentLogs.GetLogs(ctx, clusterID, jobID, afterSequence, limit)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Get log statistics for specific job
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
