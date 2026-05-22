package api

import (
	"database/sql"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PoolHandler handles cluster pool management endpoints (admin only)
type PoolHandler struct {
	store *store.Store
}

// NewPoolHandler creates a new pool handler
func NewPoolHandler(st *store.Store) *PoolHandler {
	return &PoolHandler{
		store: st,
	}
}

// CreatePool creates a new cluster pool
//
//	@Summary		Create cluster pool
//	@Description	Creates a new cluster pool for pre-provisioned clusters (admin only)
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			body	body		types.CreatePoolRequest	true	"Pool creation request"
//	@Success		201		{object}	types.ClusterPool
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		409		{object}	map[string]string	"Pool name already exists"
//	@Failure		500		{object}	map[string]string	"Failed to create pool"
//	@Security		BearerAuth
//	@Router			/admin/pools [post]
func (h *PoolHandler) CreatePool(c echo.Context) error {
	ctx := c.Request().Context()

	var req types.CreatePoolRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get current user ID for created_by field
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Apply defaults
	maxLeaseDurationHours := 2
	if req.MaxLeaseDurationHours != nil {
		maxLeaseDurationHours = *req.MaxLeaseDurationHours
	}

	autoReleaseEnabled := true
	if req.AutoReleaseEnabled != nil {
		autoReleaseEnabled = *req.AutoReleaseEnabled
	}

	maxClusterAgeDays := 7
	if req.MaxClusterAgeDays != nil {
		maxClusterAgeDays = *req.MaxClusterAgeDays
	}

	autoRefreshEnabled := false
	if req.AutoRefreshEnabled != nil {
		autoRefreshEnabled = *req.AutoRefreshEnabled
	}

	scheduledMode := false
	if req.ScheduledMode != nil {
		scheduledMode = *req.ScheduledMode
	}

	scheduleStartHour := 8
	if req.ScheduleStartHour != nil {
		scheduleStartHour = *req.ScheduleStartHour
	}

	scheduleEndHour := 18
	if req.ScheduleEndHour != nil {
		scheduleEndHour = *req.ScheduleEndHour
	}

	scheduleDaysOfWeek := []int{1, 2, 3, 4, 5} // Mon-Fri by default
	if len(req.ScheduleDaysOfWeek) > 0 {
		scheduleDaysOfWeek = req.ScheduleDaysOfWeek
	}

	// Apply min/max size defaults
	minSize := 1
	if req.MinSize > 0 {
		minSize = req.MinSize
	}

	maxSize := req.TargetSize * 2 // Default max to 2x target
	if req.MaxSize > 0 {
		maxSize = req.MaxSize
	}

	// Validate size constraints
	if minSize > req.TargetSize {
		return ErrorBadRequest(c, "min_size cannot be greater than target_size")
	}
	if req.TargetSize > maxSize {
		return ErrorBadRequest(c, "target_size cannot be greater than max_size")
	}

	// Validate schedule
	if scheduledMode {
		if req.ScheduleTimezone == "" {
			return ErrorBadRequest(c, "schedule_timezone is required when scheduled_mode is enabled")
		}
		if scheduleStartHour < 0 || scheduleStartHour > 23 {
			return ErrorBadRequest(c, "schedule_start_hour must be between 0 and 23")
		}
		if scheduleEndHour < 0 || scheduleEndHour > 23 {
			return ErrorBadRequest(c, "schedule_end_hour must be between 0 and 23")
		}
	}

	pool := &types.ClusterPool{
		Name:                  req.Name,
		DisplayName:           req.DisplayName,
		Description:           req.Description,
		Profile:               req.Profile,
		TargetSize:            req.TargetSize,
		MinSize:               minSize,
		MaxSize:               maxSize,
		MaxLeaseDurationHours: maxLeaseDurationHours,
		AutoReleaseEnabled:    autoReleaseEnabled,
		MaxClusterAgeDays:     maxClusterAgeDays,
		AutoRefreshEnabled:    autoRefreshEnabled,
		ScheduledMode:         scheduledMode,
		ScheduleTimezone:      req.ScheduleTimezone,
		ScheduleStartHour:     scheduleStartHour,
		ScheduleEndHour:       scheduleEndHour,
		ScheduleDaysOfWeek:    scheduleDaysOfWeek,
		ClusterConfig:         req.ClusterConfig,
		Enabled:               true, // New pools are enabled by default
		CreatedBy:             userID,
	}

	if err := h.store.Pools.Create(ctx, nil, pool); err != nil {
		// Check for duplicate name error
		if err.Error() == `duplicate key value violates unique constraint "cluster_pools_name_key"` {
			return ErrorConflict(c, "pool with name '"+req.Name+"' already exists")
		}
		return LogAndReturnGenericError(c, err)
	}

	return SuccessCreated(c, pool)
}

// ListPools returns all cluster pools
//
//	@Summary		List cluster pools
//	@Description	Returns a list of all cluster pools. All authenticated users can list enabled pools via /pools. Admins can list all pools (including disabled) via /admin/pools.
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			enabled_only	query		boolean	false	"Filter to only enabled pools (default: true for /pools, false for /admin/pools)"
//	@Success		200				{object}	map[string]interface{}	"Returns pools array"
//	@Failure		500				{object}	map[string]string		"Failed to list pools"
//	@Security		BearerAuth
//	@Router			/pools [get]
//	@Router			/admin/pools [get]
func (h *PoolHandler) ListPools(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse enabled_only filter
	enabledOnly := c.QueryParam("enabled_only") == "true"

	pools, err := h.store.Pools.List(ctx, enabledOnly)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]interface{}{
		"pools": pools,
	})
}

// GetPool retrieves a pool by name with real-time statistics
//
//	@Summary		Get cluster pool
//	@Description	Retrieves pool details and real-time statistics by name (admin only)
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Pool name"
//	@Success		200		{object}	map[string]interface{}	"Returns pool and stats"
//	@Failure		404		{object}	map[string]string		"Pool not found"
//	@Failure		500		{object}	map[string]string		"Failed to get pool"
//	@Security		BearerAuth
//	@Router			/admin/pools/{name} [get]
func (h *PoolHandler) GetPool(c echo.Context) error {
	ctx := c.Request().Context()
	poolName := c.Param("name")

	pool, err := h.store.Pools.GetByName(ctx, poolName)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrorNotFound(c, "pool not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Get real-time statistics
	stats, err := h.store.Pools.GetStats(ctx, pool.ID)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]interface{}{
		"pool":  pool,
		"stats": stats,
	})
}

// UpdatePool updates pool configuration
//
//	@Summary		Update cluster pool
//	@Description	Updates pool configuration settings (admin only)
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string						true	"Pool name"
//	@Param			body	body		types.UpdatePoolRequest		true	"Pool update fields"
//	@Success		200		{object}	types.ClusterPool
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		404		{object}	map[string]string	"Pool not found"
//	@Failure		500		{object}	map[string]string	"Failed to update pool"
//	@Security		BearerAuth
//	@Router			/admin/pools/{name} [patch]
func (h *PoolHandler) UpdatePool(c echo.Context) error {
	ctx := c.Request().Context()
	poolName := c.Param("name")

	var req types.UpdatePoolRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get existing pool to validate constraints
	existingPool, err := h.store.Pools.GetByName(ctx, poolName)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrorNotFound(c, "pool not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Build updates map
	updates := make(map[string]interface{})

	if req.DisplayName != nil {
		updates["display_name"] = *req.DisplayName
	}

	if req.Description != nil {
		updates["description"] = *req.Description
	}

	// Validate size constraints
	minSize := existingPool.MinSize
	targetSize := existingPool.TargetSize
	maxSize := existingPool.MaxSize

	if req.MinSize != nil {
		minSize = *req.MinSize
		updates["min_size"] = minSize
	}

	if req.TargetSize != nil {
		targetSize = *req.TargetSize
		updates["target_size"] = targetSize
	}

	if req.MaxSize != nil {
		maxSize = *req.MaxSize
		updates["max_size"] = maxSize
	}

	if minSize > targetSize {
		return ErrorBadRequest(c, "min_size cannot be greater than target_size")
	}
	if targetSize > maxSize {
		return ErrorBadRequest(c, "target_size cannot be greater than max_size")
	}

	if req.MaxLeaseDurationHours != nil {
		updates["max_lease_duration_hours"] = *req.MaxLeaseDurationHours
	}

	if req.AutoReleaseEnabled != nil {
		updates["auto_release_enabled"] = *req.AutoReleaseEnabled
	}

	if req.MaxClusterAgeDays != nil {
		updates["max_cluster_age_days"] = *req.MaxClusterAgeDays
	}

	if req.AutoRefreshEnabled != nil {
		updates["auto_refresh_enabled"] = *req.AutoRefreshEnabled
	}

	if req.ScheduledMode != nil {
		updates["scheduled_mode"] = *req.ScheduledMode
	}

	if req.ScheduleTimezone != nil {
		updates["schedule_timezone"] = *req.ScheduleTimezone
	}

	if req.ScheduleStartHour != nil {
		if *req.ScheduleStartHour < 0 || *req.ScheduleStartHour > 23 {
			return ErrorBadRequest(c, "schedule_start_hour must be between 0 and 23")
		}
		updates["schedule_start_hour"] = *req.ScheduleStartHour
	}

	if req.ScheduleEndHour != nil {
		if *req.ScheduleEndHour < 0 || *req.ScheduleEndHour > 23 {
			return ErrorBadRequest(c, "schedule_end_hour must be between 0 and 23")
		}
		updates["schedule_end_hour"] = *req.ScheduleEndHour
	}

	if len(req.ScheduleDaysOfWeek) > 0 {
		updates["schedule_days_of_week"] = req.ScheduleDaysOfWeek
	}

	if req.ClusterConfig != nil {
		updates["cluster_config"] = req.ClusterConfig
	}

	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) == 0 {
		return ErrorBadRequest(c, "no fields to update")
	}

	// Update pool
	if err := h.store.Pools.Update(ctx, nil, existingPool.ID, updates); err != nil {
		if err == sql.ErrNoRows {
			return ErrorNotFound(c, "pool not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Get updated pool
	pool, err := h.store.Pools.GetByID(ctx, existingPool.ID)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, pool)
}

// DeletePool deletes a cluster pool
//
//	@Summary		Delete cluster pool
//	@Description	Deletes a cluster pool (admin only). Orphans all clusters in the pool by setting their pool_id to NULL.
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Pool name"
//	@Success		200		{object}	map[string]string
//	@Failure		404		{object}	map[string]string	"Pool not found"
//	@Failure		500		{object}	map[string]string	"Failed to delete pool"
//	@Security		BearerAuth
//	@Router			/admin/pools/{name} [delete]
func (h *PoolHandler) DeletePool(c echo.Context) error {
	ctx := c.Request().Context()
	poolName := c.Param("name")

	// Get pool to verify it exists and get ID
	pool, err := h.store.Pools.GetByName(ctx, poolName)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrorNotFound(c, "pool not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Delete pool (ON DELETE SET NULL will orphan clusters)
	if err := h.store.Pools.Delete(ctx, pool.ID); err != nil {
		if err == sql.ErrNoRows {
			return ErrorNotFound(c, "pool not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]string{
		"message": "pool deleted successfully",
	})
}
