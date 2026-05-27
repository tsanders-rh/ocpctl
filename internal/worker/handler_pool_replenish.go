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

// PoolReplenishHandler handles pool replenishment jobs
type PoolReplenishHandler struct {
	config       *Config
	store        *store.Store
	createHandler *CreateHandler
}

// NewPoolReplenishHandler creates a new pool replenish handler
func NewPoolReplenishHandler(config *Config, st *store.Store) *PoolReplenishHandler {
	return &PoolReplenishHandler{
		config:        config,
		store:         st,
		createHandler: NewCreateHandler(config, st),
	}
}

// Handle processes a pool replenishment job
func (h *PoolReplenishHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get pool ID from job metadata
	poolID, ok := job.Metadata["pool_id"].(string)
	if !ok {
		return fmt.Errorf("pool_id not found in job metadata")
	}

	log.Printf("Processing POOL_REPLENISH job for pool %s", poolID)

	// Get pool details
	pool, err := h.store.Pools.GetByID(ctx, poolID)
	if err != nil {
		return fmt.Errorf("failed to get pool: %w", err)
	}

	// Check if pool is enabled
	if !pool.Enabled {
		log.Printf("Pool %s is disabled, skipping replenishment", pool.Name)
		return nil
	}

	// Check if pool is within scheduled hours (if scheduled mode enabled)
	if pool.ScheduledMode && !pool.IsWithinSchedule(time.Now()) {
		log.Printf("Pool %s is outside scheduled hours, skipping replenishment", pool.Name)
		return nil
	}

	// Get current pool statistics
	stats, err := h.store.Pools.GetStats(ctx, poolID)
	if err != nil {
		return fmt.Errorf("failed to get pool stats: %w", err)
	}

	log.Printf("Pool %s stats: total=%d, ready=%d, provisioning=%d, target=%d",
		pool.Name, stats.TotalClusters, stats.ReadyClusters, stats.ProvisioningClusters, pool.TargetSize)

	// Check for recent cluster failures - if multiple clusters failed recently, there's likely
	// a systemic issue (AWS quota, network, profile misconfiguration, etc.) and we should stop
	var recentFailureCount int
	err = h.store.Pool().QueryRow(ctx, `
		SELECT COUNT(*)
		FROM clusters
		WHERE pool_id = $1
		AND status = 'FAILED'
		AND created_at > NOW() - INTERVAL '1 hour'
	`, poolID).Scan(&recentFailureCount)
	if err != nil {
		log.Printf("Warning: Failed to check recent cluster failures: %v", err)
	} else if recentFailureCount >= 2 {
		// Disable the pool to prevent continuous failure loop
		log.Printf("ERROR: Pool %s has %d cluster(s) that failed in the last hour - disabling pool to prevent continuous failures",
			pool.Name, recentFailureCount)

		updateErr := h.store.Pool().QueryRow(ctx, `
			UPDATE cluster_pools
			SET enabled = false
			WHERE id = $1
			RETURNING id
		`, poolID).Scan(&poolID)

		if updateErr != nil {
			log.Printf("ERROR: Failed to disable pool %s: %v", pool.Name, updateErr)
		} else {
			log.Printf("Pool %s has been DISABLED due to repeated cluster failures. Admin intervention required.", pool.Name)
		}

		return fmt.Errorf("pool %s disabled due to %d recent cluster failures - admin intervention required", pool.Name, recentFailureCount)
	}

	// Calculate how many clusters we need to provision
	// Total includes READY, LEASED, PROVISIONING, CLEANING, EXPIRED
	// We want to provision enough to reach target_size
	clustersNeeded := pool.TargetSize - stats.TotalClusters

	if clustersNeeded <= 0 {
		log.Printf("Pool %s has sufficient clusters (%d/%d), no replenishment needed",
			pool.Name, stats.TotalClusters, pool.TargetSize)
		return nil
	}

	// Check max_size limit
	if stats.TotalClusters >= pool.MaxSize {
		log.Printf("Pool %s is at max capacity (%d/%d), cannot provision more clusters",
			pool.Name, stats.TotalClusters, pool.MaxSize)
		return nil
	}

	// Don't exceed max_size
	if stats.TotalClusters+clustersNeeded > pool.MaxSize {
		clustersNeeded = pool.MaxSize - stats.TotalClusters
	}

	log.Printf("Provisioning %d cluster(s) for pool %s (current: %d, target: %d, max: %d)",
		clustersNeeded, pool.Name, stats.TotalClusters, pool.TargetSize, pool.MaxSize)

	// Get pool creator username for cluster ownership
	var ownerUsername string
	err = h.store.Pool().QueryRow(ctx, "SELECT username FROM users WHERE id = $1", pool.CreatedBy).Scan(&ownerUsername)
	if err != nil {
		// If username not found, use the user ID as fallback
		log.Printf("Warning: Could not fetch username for user %s: %v", pool.CreatedBy, err)
		ownerUsername = pool.CreatedBy
	}

	// Get profile details for cluster creation
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

	// Get base domain from environment or use default
	baseDomainStr := os.Getenv("BASE_DOMAIN")
	if baseDomainStr == "" {
		baseDomainStr = "mg.dog8code.com" // Default domain
	}
	baseDomain := &baseDomainStr

	// Provision clusters
	for i := 0; i < clustersNeeded; i++ {
		clusterName := fmt.Sprintf("%s-%s", pool.Name, uuid.New().String()[:8])

		// Create cluster record
		cluster := &types.Cluster{
			ID:          uuid.New().String(),
			Name:        clusterName,
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
			RequestedBy: "pool-replenish-job",
			TTLHours:    pool.MaxClusterAgeDays * 24, // Use pool max age as TTL
			PoolID:      &pool.ID,
			PoolState:   (*types.PoolState)(&[]types.PoolState{types.PoolStateProvisioning}[0]),
		}

		// Apply pool cluster configuration overrides
		if pool.ClusterConfig != nil {
			// Merge pool-specific config (this would need implementation)
			// For now, we'll use defaults from the profile
		}

		// Calculate destroy_at timestamp
		destroyAt := time.Now().Add(time.Duration(cluster.TTLHours) * time.Hour)
		cluster.DestroyAt = &destroyAt

		// Create cluster in database
		if err := h.store.Clusters.Create(ctx, cluster); err != nil {
			log.Printf("Failed to create cluster %s for pool %s: %v", clusterName, pool.Name, err)
			continue
		}

		log.Printf("Created cluster %s for pool %s (pool_state=PROVISIONING)", clusterName, pool.Name)

		// Create a CREATE job for the cluster
		createJob := &types.Job{
			ID:          uuid.New().String(),
			ClusterID:   cluster.ID,
			JobType:     types.JobTypeCreate,
			Status:      types.JobStatusPending,
			Attempt:     1,
			MaxAttempts: 3,
			Metadata: types.JobMetadata{
				"pool_id":   poolID,
				"pool_name": pool.Name,
				"triggered_by": "pool_replenish",
			},
		}

		if err := h.store.Jobs.Create(ctx, nil, createJob); err != nil {
			log.Printf("Failed to create CREATE job for cluster %s: %v", clusterName, err)
			// Mark cluster as failed
			h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusFailed)
			continue
		}

		log.Printf("Created CREATE job %s for cluster %s", createJob.ID, clusterName)
	}

	log.Printf("Pool replenishment complete for %s: provisioned %d cluster(s)", pool.Name, clustersNeeded)
	return nil
}

// loadProfileRegistry loads the profile registry
func (h *PoolReplenishHandler) loadProfileRegistry() (*profile.Registry, error) {
	// Get profiles directory from environment (same as API and other workers)
	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "/opt/ocpctl/profiles" // Default path
	}
	loader := profile.NewLoader(profilesDir)
	return profile.NewRegistry(loader)
}
