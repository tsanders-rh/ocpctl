package api

import (
	"database/sql"
	"errors"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
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
	jobs, total, err := h.store.Jobs.List(ctx, pagination.Offset, pagination.PerPage)
	if err != nil {
		return ErrorInternal(c, "Failed to list jobs: "+err.Error())
	}

	// Calculate pagination metadata
	paginationMeta := CalculatePagination(pagination.Page, pagination.PerPage, total)

	return SuccessPaginated(c, jobs, paginationMeta, filterMap)
}

// Get handles GET /api/v1/jobs/:id
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
		return ErrorInternal(c, "Failed to retrieve job: "+err.Error())
	}

	return SuccessOK(c, job)
}
