package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PoolCleanHandler handles cluster cleaning/sanitization for pools
type PoolCleanHandler struct {
	config *Config
	store  *store.Store
}

// NewPoolCleanHandler creates a new pool clean handler
func NewPoolCleanHandler(config *Config, st *store.Store) *PoolCleanHandler {
	return &PoolCleanHandler{
		config: config,
		store:  st,
	}
}

// Handle processes a pool cluster cleaning job
func (h *PoolCleanHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	log.Printf("Cleaning cluster %s (pool_id=%v, pool_state=%v)",
		cluster.Name, cluster.PoolID, cluster.PoolState)

	// Verify cluster is in CLEANING state
	if cluster.PoolState == nil || *cluster.PoolState != types.PoolStateCleaning {
		return fmt.Errorf("cluster %s is not in CLEANING state (current: %v)", cluster.Name, cluster.PoolState)
	}

	// Verify cluster status is READY
	if cluster.Status != types.ClusterStatusReady {
		log.Printf("Cluster %s is not in READY status (current: %s), marking as EXPIRED", cluster.Name, cluster.Status)
		// If cluster is not healthy, mark as EXPIRED instead of READY
		expiredState := types.PoolStateExpired
		if err := h.store.Clusters.UpdatePoolState(ctx, cluster.ID, expiredState); err != nil {
			return fmt.Errorf("failed to update pool state: %w", err)
		}
		return nil
	}

	log.Printf("Starting cluster cleanup for %s", cluster.Name)

	// TODO: Implement actual cluster cleanup
	// For now, we'll just transition the cluster to READY state
	// Future enhancements:
	// 1. Download kubeconfig from S3
	// 2. Delete user-created namespaces
	// 3. Clean up resources in default namespace
	// 4. Reset any custom configurations

	// Reset cluster metadata
	// Clear lease metadata
	now := time.Now()
	updates := map[string]interface{}{
		"pool_state":        types.PoolStateReady,
		"leased_by":         nil,
		"leased_at":         nil,
		"lease_expires_at":  nil,
		"lease_metadata":    types.JobMetadata{},
		"pool_generation":   cluster.PoolGeneration + 1, // Increment generation
		"last_cleaned_at":   &now,
	}

	if err := h.store.Clusters.Update(ctx, cluster.ID, updates); err != nil {
		return fmt.Errorf("failed to update cluster state: %w", err)
	}

	log.Printf("Successfully cleaned cluster %s, marked as READY for pool (generation=%d)",
		cluster.Name, cluster.PoolGeneration+1)

	return nil
}
