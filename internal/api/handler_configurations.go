package api

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ConfigurationHandler handles configuration-related API endpoints
type ConfigurationHandler struct {
	store *store.Store
}

// NewConfigurationHandler creates a new configuration handler
func NewConfigurationHandler(store *store.Store) *ConfigurationHandler {
	return &ConfigurationHandler{
		store: store,
	}
}

// ListClusterConfigurations handles GET /api/v1/clusters/:id/configurations
//
//	@Summary		List cluster configurations
//	@Description	Returns all post-deployment configurations for a cluster
//	@Tags			Configurations
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	map[string]interface{}
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/configurations [get]
func (h *ConfigurationHandler) ListClusterConfigurations(c echo.Context) error {
	clusterID := c.Param("id")
	ctx := c.Request().Context()

	// Verify cluster exists
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return ErrorNotFound(c, "Cluster not found")
	}

	// Get configurations
	configs, err := h.store.ClusterConfigurations.ListByClusterID(ctx, clusterID)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("list configurations: %w", err))
	}

	// If no configurations, return empty array
	if configs == nil {
		configs = []*types.ClusterConfiguration{}
	}

	return SuccessOK(c, map[string]interface{}{
		"cluster_id":      cluster.ID,
		"cluster_name":    cluster.Name,
		"configurations":  configs,
		"total":           len(configs),
	})
}

// RetryConfiguration handles PATCH /api/v1/clusters/:id/configurations/:config_id/retry
//
//	@Summary		Retry failed configuration
//	@Description	Retries a failed post-deployment configuration by creating a new job
//	@Tags			Configurations
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string	true	"Cluster ID"
//	@Param			config_id	path		string	true	"Configuration ID"
//	@Success		200			{object}	map[string]interface{}
//	@Failure		400			{object}	map[string]string	"Configuration not failed or doesn't belong to cluster"
//	@Failure		404			{object}	map[string]string	"Cluster or configuration not found"
//	@Failure		500			{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/configurations/{config_id}/retry [patch]
func (h *ConfigurationHandler) RetryConfiguration(c echo.Context) error {
	clusterID := c.Param("id")
	configID := c.Param("config_id")
	ctx := c.Request().Context()

	// Verify cluster exists
	_, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return ErrorNotFound(c, "Cluster not found")
	}

	// Get configuration
	config, err := h.store.ClusterConfigurations.GetByID(ctx, configID)
	if err != nil {
		return ErrorNotFound(c, "Configuration not found")
	}

	// Verify it belongs to this cluster
	if config.ClusterID != clusterID {
		return ErrorBadRequest(c, "Configuration does not belong to this cluster")
	}

	// Only allow retry if failed
	if config.Status != types.ConfigStatusFailed {
		return ErrorBadRequest(c, fmt.Sprintf("Can only retry failed configurations (current status: %s)", config.Status))
	}

	// Reset to pending status
	if err := h.store.ClusterConfigurations.UpdateStatus(ctx, configID, types.ConfigStatusPending, nil); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("update configuration status: %w", err))
	}

	// Create a new POST_CONFIGURE job to retry
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   clusterID,
		JobType:     types.JobTypePostConfigure,
		Status:      types.JobStatusPending,
		Attempt:     0,
		MaxAttempts: 3,
		Metadata: types.JobMetadata{
			"retry_config_id": configID,
		},
	}

	if err := h.store.Jobs.Create(ctx, job); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("create retry job: %w", err))
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":        "Configuration retry initiated",
		"configuration_id": configID,
		"job_id":         job.ID,
	})
}

// TriggerPostConfiguration handles POST /api/v1/clusters/:id/configure
//
//	@Summary		Trigger post-deployment configuration
//	@Description	Manually triggers post-deployment configuration for a ready cluster (useful if skipped during creation)
//	@Tags			Configurations
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Cluster ID"
//	@Success		200	{object}	map[string]interface{}
//	@Failure		400	{object}	map[string]string	"Cluster not ready, already configured, or job already running"
//	@Failure		404	{object}	map[string]string	"Cluster not found"
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/clusters/{id}/configure [post]
func (h *ConfigurationHandler) TriggerPostConfiguration(c echo.Context) error {
	clusterID := c.Param("id")
	ctx := c.Request().Context()

	// Verify cluster exists
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return ErrorNotFound(c, "Cluster not found")
	}

	// Only allow if cluster is READY
	if cluster.Status != types.ClusterStatusReady {
		return ErrorBadRequest(c, fmt.Sprintf("Cluster must be in READY status (current: %s)", cluster.Status))
	}

	// Check if post-deployment was skipped
	if !cluster.SkipPostDeployment && cluster.PostDeployStatus != nil && *cluster.PostDeployStatus == "completed" {
		return ErrorBadRequest(c, "Post-deployment configuration already completed")
	}

	// Check if there's already a pending or running POST_CONFIGURE job
	existingJobs, err := h.store.Jobs.ListByClusterID(ctx, clusterID)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("check existing jobs: %w", err))
	}

	for _, job := range existingJobs {
		if job.JobType == types.JobTypePostConfigure &&
		   (job.Status == types.JobStatusPending || job.Status == types.JobStatusRunning) {
			return ErrorBadRequest(c, "Post-configuration job already in progress")
		}
	}

	// Create POST_CONFIGURE job
	job := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   clusterID,
		JobType:     types.JobTypePostConfigure,
		Status:      types.JobStatusPending,
		Attempt:     0,
		MaxAttempts: 3,
		Metadata: types.JobMetadata{
			"manual_trigger": true,
		},
	}

	if err := h.store.Jobs.Create(ctx, job); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("create post-configure job: %w", err))
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":    "Post-deployment configuration triggered",
		"cluster_id": clusterID,
		"job_id":     job.ID,
	})
}
