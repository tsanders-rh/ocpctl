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

	// If cluster_id filter is provided, use ListByClusterID
	if filters.ClusterID != "" {
		log.Printf("[DEBUG] Using filtered query for cluster_id: %s", filters.ClusterID)
		jobs, err = h.store.Jobs.ListByClusterID(ctx, filters.ClusterID)
		if err != nil {
			return LogAndReturnGenericError(c, fmt.Errorf("failed to list jobs: %w", err))
		}
		total = len(jobs)
		log.Printf("[DEBUG] Filtered query returned %d jobs", total)

		// Apply manual pagination to filtered results
		start := pagination.Offset
		end := start + pagination.PerPage
		if start > len(jobs) {
			jobs = []*types.Job{}
		} else {
			if end > len(jobs) {
				end = len(jobs)
			}
			jobs = jobs[start:end]
		}
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
