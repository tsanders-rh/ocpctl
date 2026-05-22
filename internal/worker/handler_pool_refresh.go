package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PoolRefreshHandler handles cluster refresh for pools (replacing expired clusters)
type PoolRefreshHandler struct {
	config *Config
	store  *store.Store
}

// NewPoolRefreshHandler creates a new pool refresh handler
func NewPoolRefreshHandler(config *Config, st *store.Store) *PoolRefreshHandler {
	return &PoolRefreshHandler{
		config: config,
		store:  st,
	}
}

// Handle processes a pool cluster refresh job
func (h *PoolRefreshHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	log.Printf("Refreshing expired cluster %s (pool_id=%v, age=%s)",
		cluster.Name, cluster.PoolID, time.Since(cluster.CreatedAt))

	// Verify cluster is in EXPIRED state
	if cluster.PoolState == nil || *cluster.PoolState != types.PoolStateExpired {
		return fmt.Errorf("cluster %s is not in EXPIRED state (current: %v)", cluster.Name, cluster.PoolState)
	}

	// Get pool details
	if cluster.PoolID == nil {
		return fmt.Errorf("cluster %s has no pool_id", cluster.Name)
	}

	pool, err := h.store.Pools.GetByID(ctx, *cluster.PoolID)
	if err != nil {
		return fmt.Errorf("failed to get pool: %w", err)
	}

	// Check if auto-refresh is enabled
	if !pool.AutoRefreshEnabled {
		log.Printf("Auto-refresh is disabled for pool %s, skipping refresh", pool.Name)
		return nil
	}

	// Get pool statistics
	stats, err := h.store.Pools.GetStats(ctx, pool.ID)
	if err != nil {
		return fmt.Errorf("failed to get pool stats: %w", err)
	}

	log.Printf("Pool %s stats before refresh: total=%d, ready=%d, expired=%d",
		pool.Name, stats.TotalClusters, stats.ReadyClusters, stats.ExpiredClusters)

	// Create replacement cluster first (before destroying the expired one)
	// This ensures pool capacity is maintained
	newClusterName := fmt.Sprintf("%s-%s", pool.Name, uuid.New().String()[:8])

	// Load profile registry
	profileRegistry, err := h.loadProfileRegistry()
	if err != nil {
		return fmt.Errorf("failed to load profile registry: %w", err)
	}

	prof, err := profileRegistry.Get(pool.Profile)
	if err != nil {
		return fmt.Errorf("profile %s not found: %w", pool.Profile, err)
	}

	// Get default version based on cluster type
	var defaultVersion string
	if prof.ClusterType == types.ClusterTypeOpenShift || prof.ClusterType == types.ClusterTypeROSA || prof.ClusterType == types.ClusterTypeARO {
		if prof.OpenshiftVersions != nil {
			defaultVersion = prof.OpenshiftVersions.Default
		}
	} else {
		if prof.KubernetesVersions != nil {
			defaultVersion = prof.KubernetesVersions.Default
		}
	}

	// Get pool creator username for cluster ownership
	var ownerUsername string
	err = h.store.Pool().QueryRow(ctx, "SELECT username FROM users WHERE id = $1", pool.CreatedBy).Scan(&ownerUsername)
	if err != nil {
		// If username not found, use the user ID as fallback
		log.Printf("Warning: Could not fetch username for user %s: %v", pool.CreatedBy, err)
		ownerUsername = pool.CreatedBy
	}

	// Get base domain from environment or use default
	baseDomainStr := os.Getenv("BASE_DOMAIN")
	if baseDomainStr == "" {
		baseDomainStr = "mg.dog8code.com" // Default domain
	}
	baseDomain := &baseDomainStr

	// Create new cluster record
	newCluster := &types.Cluster{
		ID:          uuid.New().String(),
		Name:        newClusterName,
		Platform:    prof.Platform,
		ClusterType: prof.ClusterType,
		Version:     defaultVersion,
		Profile:     pool.Profile,
		Region:      prof.Regions.Default,
		BaseDomain:  baseDomain, // Required for OpenShift clusters
		Owner:       ownerUsername,  // Use username for display
		OwnerID:     pool.CreatedBy, // Pool creator owns pool clusters
		Team:        "pool-managed",
		CostCenter:  "pool-" + pool.Name,
		Status:      types.ClusterStatusPending,
		RequestedBy: "pool-refresh-job",
		TTLHours:    pool.MaxClusterAgeDays * 24,
		PoolID:      &pool.ID,
		PoolState:   (*types.PoolState)(&[]types.PoolState{types.PoolStateProvisioning}[0]),
	}

	// Apply pool cluster configuration overrides
	if pool.ClusterConfig != nil {
		// Merge pool-specific config (this would need implementation)
		// For now, we'll use defaults from the profile
	}

	// Calculate destroy_at timestamp
	destroyAt := time.Now().Add(time.Duration(newCluster.TTLHours) * time.Hour)
	newCluster.DestroyAt = &destroyAt

	// Create new cluster in database
	if err := h.store.Clusters.Create(ctx, newCluster); err != nil {
		return fmt.Errorf("failed to create replacement cluster: %w", err)
	}

	log.Printf("Created replacement cluster %s for expired cluster %s", newClusterName, cluster.Name)

	// Create a CREATE job for the new cluster
	createJob := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   newCluster.ID,
		JobType:     types.JobTypeCreate,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 3,
		Metadata: types.JobMetadata{
			"pool_id":         pool.ID,
			"pool_name":       pool.Name,
			"triggered_by":    "pool_refresh",
			"replaced_cluster": cluster.ID,
		},
	}

	if err := h.store.Jobs.Create(ctx, nil, createJob); err != nil {
		return fmt.Errorf("failed to create CREATE job for replacement cluster: %w", err)
	}

	log.Printf("Created CREATE job %s for replacement cluster %s", createJob.ID, newClusterName)

	// Mark the expired cluster for destruction
	// Remove it from the pool first (set pool_id to NULL)
	updates := map[string]interface{}{
		"pool_id":    nil,
		"pool_state": nil,
		"status":     types.ClusterStatusDestroying,
	}

	if err := h.store.Clusters.Update(ctx, cluster.ID, updates); err != nil {
		return fmt.Errorf("failed to update expired cluster state: %w", err)
	}

	log.Printf("Marked expired cluster %s for destruction (removed from pool)", cluster.Name)

	// Create a DESTROY job for the expired cluster
	destroyJob := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypeDestroy,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 3,
		Metadata: types.JobMetadata{
			"pool_id":       pool.ID,
			"pool_name":     pool.Name,
			"triggered_by":  "pool_refresh",
			"reason":        "expired",
			"replacement":   newCluster.ID,
		},
	}

	if err := h.store.Jobs.Create(ctx, nil, destroyJob); err != nil {
		return fmt.Errorf("failed to create DESTROY job for expired cluster: %w", err)
	}

	log.Printf("Created DESTROY job %s for expired cluster %s", destroyJob.ID, cluster.Name)
	log.Printf("Pool refresh complete: %s -> %s (old cluster will be destroyed)", cluster.Name, newClusterName)

	return nil
}

// loadProfileRegistry loads the profile registry
func (h *PoolRefreshHandler) loadProfileRegistry() (*profile.Registry, error) {
	// Get profiles directory from environment (same as API and other workers)
	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "/opt/ocpctl/profiles" // Default path
	}
	loader := profile.NewLoader(profilesDir)
	return profile.NewRegistry(loader)
}
