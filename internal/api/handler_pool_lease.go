package api

import (
	"database/sql"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PoolLeaseHandler handles cluster pool lease/release endpoints (CI/CD integration)
type PoolLeaseHandler struct {
	store *store.Store
}

// NewPoolLeaseHandler creates a new pool lease handler
func NewPoolLeaseHandler(st *store.Store) *PoolLeaseHandler {
	return &PoolLeaseHandler{
		store: st,
	}
}

// LeaseCluster atomically leases an available cluster from a pool
//
//	@Summary		Lease cluster from pool
//	@Description	Atomically leases an available cluster from a pool for CI/CD use. Returns cluster credentials.
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			pool_name	path		string					true	"Pool name"
//	@Param			body		body		types.LeaseRequest		true	"Lease request"
//	@Success		200			{object}	types.LeaseResponse
//	@Failure		400			{object}	map[string]string	"Invalid request or pool disabled"
//	@Failure		404			{object}	map[string]string	"Pool not found or no available clusters"
//	@Failure		500			{object}	map[string]string	"Failed to lease cluster"
//	@Security		BearerAuth
//	@Router			/pools/{pool_name}/lease [post]
func (h *PoolLeaseHandler) LeaseCluster(c echo.Context) error {
	ctx := c.Request().Context()
	poolName := c.Param("pool_name")

	var req types.LeaseRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get current user for audit trail
	user, err := auth.GetUser(c)
	if err == nil && req.LeasedBy == "" {
		// If no leased_by provided, use current user's email
		req.LeasedBy = user.Email
	}

	// Validate leased_by is set
	if req.LeasedBy == "" {
		return ErrorBadRequest(c, "leased_by field is required")
	}

	// Lease cluster
	cluster, err := h.store.Pools.LeaseCluster(ctx, poolName, &req)
	if err != nil {
		if err.Error() == "pool not found: sql: no rows in result set" {
			return ErrorNotFound(c, "pool '"+poolName+"' not found")
		}
		if err.Error() == "pool "+poolName+" is disabled" {
			return ErrorBadRequest(c, "pool '"+poolName+"' is disabled")
		}
		if err.Error() == "no available clusters in pool "+poolName {
			return ErrorNotFound(c, "no available clusters in pool '"+poolName+"'")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Get cluster outputs for credentials
	outputs, err := h.store.ClusterOutputs.GetByClusterID(ctx, cluster.ID)
	if err != nil && err != sql.ErrNoRows {
		// Log error but don't fail the lease
		LogWarning(c, "Failed to fetch cluster outputs", "cluster_id", cluster.ID, "error", err)
	}

	// Build lease response
	response := &types.LeaseResponse{
		ClusterID:      cluster.ID,
		ClusterName:    cluster.Name,
		LeasedBy:       *cluster.LeasedBy,
		LeasedAt:       *cluster.LeasedAt,
		LeaseExpiresAt: *cluster.LeaseExpiresAt,
		LeaseMetadata:  cluster.LeaseMetadata,
	}

	// Add cluster access information if available
	if outputs != nil {
		if outputs.APIURL != nil {
			response.APIUrl = *outputs.APIURL
		}
		if outputs.ConsoleURL != nil {
			response.ConsoleUrl = *outputs.ConsoleURL
		}
		if outputs.KubeconfigS3URI != nil {
			response.KubeconfigPath = *outputs.KubeconfigS3URI
		}
		// Add ServiceAccount token credentials for pool clusters
		if outputs.SAToken != nil {
			response.SAToken = *outputs.SAToken
		}
		if outputs.SATokenExpiresAt != nil {
			response.SATokenExpiresAt = outputs.SATokenExpiresAt
		}
		if outputs.OcLoginCommand != nil {
			response.OcLoginCommand = *outputs.OcLoginCommand
		}
	}

	LogInfo(c, "Cluster leased from pool",
		"pool_name", poolName,
		"cluster_id", cluster.ID,
		"cluster_name", cluster.Name,
		"leased_by", req.LeasedBy,
		"lease_expires_at", cluster.LeaseExpiresAt,
	)

	return SuccessOK(c, response)
}

// ReleaseCluster releases a leased cluster back to the pool
//
//	@Summary		Release cluster back to pool
//	@Description	Releases a leased cluster back to the pool for sanitization and reuse.
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			cluster_id	path		string	true	"Cluster ID"
//	@Success		200			{object}	map[string]string
//	@Failure		400			{object}	map[string]string	"Cluster is not leased"
//	@Failure		404			{object}	map[string]string	"Cluster not found"
//	@Failure		500			{object}	map[string]string	"Failed to release cluster"
//	@Security		BearerAuth
//	@Router			/pools/clusters/{cluster_id}/release [post]
func (h *PoolLeaseHandler) ReleaseCluster(c echo.Context) error {
	ctx := c.Request().Context()
	clusterID := c.Param("cluster_id")

	// Get cluster to retrieve pool information
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrorNotFound(c, "cluster not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Verify cluster belongs to a pool
	if cluster.PoolID == nil {
		return ErrorBadRequest(c, "cluster does not belong to a pool")
	}

	// Get pool details
	pool, err := h.store.Pools.GetByID(ctx, *cluster.PoolID)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Release cluster (transitions to CLEANING state)
	if err := h.store.Pools.ReleaseCluster(ctx, clusterID); err != nil {
		if err.Error() == "cluster "+clusterID+" is not leased or does not exist" {
			return ErrorBadRequest(c, "cluster is not leased or does not exist")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Get current user for audit trail
	user, _ := auth.GetUser(c)
	releasedBy := "unknown"
	if user != nil {
		releasedBy = user.Email
	}

	// Create POOL_CLEAN job to sanitize the cluster
	cleanJob := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   clusterID,
		JobType:     types.JobTypePoolClean,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 3,
		Metadata: types.JobMetadata{
			"pool_id":      *cluster.PoolID,
			"pool_name":    pool.Name,
			"triggered_by": "api",
			"released_by":  releasedBy,
		},
	}

	if err := h.store.Jobs.Create(ctx, nil, cleanJob); err != nil {
		LogWarning(c, "Failed to create POOL_CLEAN job", "cluster_id", clusterID, "error", err)
		// Don't fail the release if job creation fails - cluster is already in CLEANING state
	} else {
		LogInfo(c, "Created POOL_CLEAN job", "cluster_id", clusterID, "job_id", cleanJob.ID)
	}

	LogInfo(c, "Cluster released back to pool", "cluster_id", clusterID, "pool_name", pool.Name)

	return SuccessOK(c, map[string]string{
		"message":    "cluster released successfully",
		"cluster_id": clusterID,
		"next_state": "CLEANING",
		"job_id":     cleanJob.ID,
	})
}

// GetPoolStats returns real-time statistics for a pool
//
//	@Summary		Get pool statistics
//	@Description	Returns real-time statistics for a cluster pool (available clusters, utilization, etc.)
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			pool_name	path		string	true	"Pool name"
//	@Success		200			{object}	types.ClusterPoolStats
//	@Failure		404			{object}	map[string]string	"Pool not found"
//	@Failure		500			{object}	map[string]string	"Failed to get stats"
//	@Security		BearerAuth
//	@Router			/pools/{pool_name}/stats [get]
func (h *PoolLeaseHandler) GetPoolStats(c echo.Context) error {
	ctx := c.Request().Context()
	poolName := c.Param("pool_name")

	// Get pool to verify it exists and get ID
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

	return SuccessOK(c, stats)
}

// GetPoolClusters returns all clusters in a pool (optionally filtered by pool state)
//
//	@Summary		Get clusters in pool
//	@Description	Returns all clusters in a specific pool, optionally filtered by pool state (READY, LEASED, etc.)
//	@Tags			Pools
//	@Accept			json
//	@Produce		json
//	@Param			pool_name	path		string	true	"Pool name"
//	@Param			pool_state	query		string	false	"Filter by pool state: READY, LEASED, PROVISIONING, CLEANING"
//	@Success		200			{object}	map[string]interface{}	"Returns clusters array"
//	@Failure		404			{object}	map[string]string		"Pool not found"
//	@Failure		500			{object}	map[string]string		"Failed to get clusters"
//	@Security		BearerAuth
//	@Router			/pools/{pool_name}/clusters [get]
func (h *PoolLeaseHandler) GetPoolClusters(c echo.Context) error {
	ctx := c.Request().Context()
	poolName := c.Param("pool_name")

	// Get pool to verify it exists and get ID
	pool, err := h.store.Pools.GetByName(ctx, poolName)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrorNotFound(c, "pool not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Parse optional pool_state filter
	var poolState *types.PoolState
	if stateParam := c.QueryParam("pool_state"); stateParam != "" {
		state := types.PoolState(stateParam)
		poolState = &state
	}

	// Get clusters
	clusters, err := h.store.Clusters.GetPoolClusters(ctx, pool.ID, poolState)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]interface{}{
		"clusters": clusters,
		"count":    len(clusters),
	})
}
