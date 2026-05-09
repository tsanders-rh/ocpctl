package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// JobHandler handles job-related API endpoints
type JobHandler struct {
	store *store.Store
}

// NewJobHandler creates a new job handler
func NewJobHandler(s *store.Store) *JobHandler {
	return &JobHandler{
		store: s,
	}
}

// ListJobsFilters holds filter parameters for listing jobs
type ListJobsFilters struct {
	ClusterID string
	Type      string
	Status    string
}

// List handles GET /api/v1/jobs
//
//	@Summary		List jobs
//	@Description	Returns a paginated list of jobs. Can be filtered by cluster_id, type (create/destroy), and status.
//	@Tags			Jobs
//	@Accept			json
//	@Produce		json
//	@Param			cluster_id	query		string	false	"Filter by cluster ID"
//	@Param			type		query		string	false	"Filter by job type (create, destroy)"
//	@Param			status		query		string	false	"Filter by status (pending, running, completed, failed)"
//	@Param			page		query		int		false	"Page number (default 1)"
//	@Param			per_page	query		int		false	"Items per page (default 50, max 100)"
//	@Success		200			{object}	map[string]interface{}
//	@Failure		500			{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/jobs [get]
func (h *JobHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse pagination
	pagination := ParsePaginationParams(c)

	// Parse filters
	filters := &ListJobsFilters{
		ClusterID: c.QueryParam("cluster_id"),
		Type:      c.QueryParam("type"),
		Status:    c.QueryParam("status"),
	}

	// Debug logging
	log.Printf("[DEBUG] Jobs API called with cluster_id filter: '%s'", filters.ClusterID)

	// Build filter map for response
	filterMap := make(map[string]interface{})
	if filters.ClusterID != "" {
		filterMap["cluster_id"] = filters.ClusterID
	}
	if filters.Type != "" {
		filterMap["type"] = filters.Type
	}
	if filters.Status != "" {
		filterMap["status"] = filters.Status
	}

	// Get jobs with total count
	var jobs []*types.Job
	var total int
	var err error

	// If cluster_id filter is provided, use ListByClusterIDPaginated
	if filters.ClusterID != "" {
		log.Printf("[DEBUG] Using filtered paginated query for cluster_id: %s", filters.ClusterID)
		jobs, total, err = h.store.Jobs.ListByClusterIDPaginated(ctx, filters.ClusterID, pagination.PerPage, pagination.Offset)
		if err != nil {
			return LogAndReturnGenericError(c, fmt.Errorf("failed to list jobs: %w", err))
		}
		log.Printf("[DEBUG] Filtered query returned %d jobs (total: %d)", len(jobs), total)
	} else {
		log.Printf("[DEBUG] Using unfiltered query (no cluster_id)")
		jobs, total, err = h.store.Jobs.List(ctx, pagination.Offset, pagination.PerPage)
		if err != nil {
			return LogAndReturnGenericError(c, fmt.Errorf("failed to list jobs: %w", err))
		}
		log.Printf("[DEBUG] Unfiltered query returned %d jobs (total: %d)", len(jobs), total)
	}

	// Calculate pagination metadata
	paginationMeta := CalculatePagination(pagination.Page, pagination.PerPage, total)

	// Debug headers
	if filters.ClusterID != "" {
		c.Response().Header().Set("X-Debug-Filtered", "true")
		c.Response().Header().Set("X-Debug-Cluster-ID", filters.ClusterID)
	} else {
		c.Response().Header().Set("X-Debug-Filtered", "false")
	}
	c.Response().Header().Set("X-Debug-Total-Jobs", fmt.Sprintf("%d", total))

	return SuccessPaginated(c, jobs, paginationMeta, filterMap)
}

// Get handles GET /api/v1/jobs/:id
//
//	@Summary		Get job
//	@Description	Retrieves details of a specific job by ID
//	@Tags			Jobs
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Job ID"
//	@Success		200	{object}	types.Job
//	@Failure		404	{object}	map[string]string	"Job not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/jobs/{id} [get]
func (h *JobHandler) Get(c echo.Context) error {
	ctx := c.Request().Context()

	// Get job ID
	id := c.Param("id")

	// Get job
	job, err := h.store.Jobs.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorNotFound(c, "Job not found")
		}
		return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve job: %w", err))
	}

	return SuccessOK(c, job)
}
