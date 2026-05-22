package poolscheduler

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Config holds pool scheduler configuration
type Config struct {
	CheckInterval       time.Duration // How often to check pools
	LeaseCheckInterval  time.Duration // How often to check for expired leases
	RefreshCheckEnabled bool          // Enable checking for expired clusters
}

// DefaultConfig returns default pool scheduler configuration
func DefaultConfig() *Config {
	return &Config{
		CheckInterval:       2 * time.Minute, // Check every 2 minutes
		LeaseCheckInterval:  1 * time.Minute, // Check leases every minute
		RefreshCheckEnabled: true,
	}
}

// Scheduler performs periodic pool management tasks
type Scheduler struct {
	config  *Config
	store   *store.Store
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewScheduler creates a new pool scheduler instance
func NewScheduler(config *Config, st *store.Store) *Scheduler {
	if config == nil {
		config = DefaultConfig()
	}

	return &Scheduler{
		config:  config,
		store:   st,
		running: false,
	}
}

// Start starts the pool scheduler loop
func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running = true

	log.Printf("Pool scheduler starting (check_interval=%s, lease_check_interval=%s)",
		s.config.CheckInterval, s.config.LeaseCheckInterval)

	// Run immediately on start
	s.run()

	// Start periodic loop with two tickers
	poolTicker := time.NewTicker(s.config.CheckInterval)
	leaseTicker := time.NewTicker(s.config.LeaseCheckInterval)
	defer poolTicker.Stop()
	defer leaseTicker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Printf("Pool scheduler shutting down")
			return s.ctx.Err()

		case <-poolTicker.C:
			s.run()

		case <-leaseTicker.C:
			s.checkExpiredLeases()
		}
	}
}

// Stop stops the scheduler gracefully
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
}

// run performs all pool management tasks
func (s *Scheduler) run() {
	ctx := s.ctx

	log.Printf("Pool scheduler running management tasks")

	// 1. Check pools needing replenishment
	if err := s.checkPoolReplenishment(ctx); err != nil {
		log.Printf("Error checking pool replenishment: %v", err)
	}

	// 2. Check for expired clusters needing refresh
	if s.config.RefreshCheckEnabled {
		if err := s.checkExpiredClusters(ctx); err != nil {
			log.Printf("Error checking expired clusters: %v", err)
		}
	}
}

// checkExpiredLeases checks for and releases expired leases
func (s *Scheduler) checkExpiredLeases() {
	ctx := s.ctx

	// Find all leased clusters with expired leases
	query := `
		SELECT id, name, pool_id, leased_by, leased_at, lease_expires_at
		FROM clusters
		WHERE pool_state = 'LEASED'
		  AND lease_expires_at < NOW()
		  AND pool_id IS NOT NULL
	`

	rows, err := s.store.Pool().Query(ctx, query)
	if err != nil {
		log.Printf("Error querying expired leases: %v", err)
		return
	}
	defer rows.Close()

	expiredCount := 0
	for rows.Next() {
		var clusterID, clusterName string
		var poolID *string
		var leasedBy *string
		var leasedAt, leaseExpiresAt *time.Time

		if err := rows.Scan(&clusterID, &clusterName, &poolID, &leasedBy, &leasedAt, &leaseExpiresAt); err != nil {
			log.Printf("Error scanning expired lease: %v", err)
			continue
		}

		// Get pool details to check if auto-release is enabled
		pool, err := s.store.Pools.GetByID(ctx, *poolID)
		if err != nil {
			log.Printf("Error getting pool for expired lease (cluster=%s): %v", clusterName, err)
			continue
		}

		if !pool.AutoReleaseEnabled {
			log.Printf("Skipping auto-release for cluster %s (pool=%s, auto_release_enabled=false)",
				clusterName, pool.Name)
			continue
		}

		log.Printf("Auto-releasing expired lease: cluster=%s, pool=%s, leased_by=%s, expired_at=%s",
			clusterName, pool.Name, *leasedBy, leaseExpiresAt.Format(time.RFC3339))

		// Release the cluster (transitions to CLEANING state)
		if err := s.store.Pools.ReleaseCluster(ctx, clusterID); err != nil {
			log.Printf("Error releasing expired lease (cluster=%s): %v", clusterName, err)
			continue
		}

		// Create a POOL_CLEAN job
		cleanJob := &types.Job{
			ID:          uuid.New().String(),
			ClusterID:   clusterID,
			JobType:     types.JobTypePoolClean,
			Status:      types.JobStatusPending,
			Attempt:     1,
			MaxAttempts: 3,
			Metadata: types.JobMetadata{
				"pool_id":      *poolID,
				"pool_name":    pool.Name,
				"triggered_by": "pool_scheduler",
				"reason":       "expired_lease",
			},
		}

		if err := s.store.Jobs.Create(ctx, nil, cleanJob); err != nil {
			log.Printf("Error creating POOL_CLEAN job for cluster %s: %v", clusterName, err)
			continue
		}

		log.Printf("Created POOL_CLEAN job %s for cluster %s", cleanJob.ID, clusterName)
		expiredCount++
	}

	if expiredCount > 0 {
		log.Printf("Auto-released %d expired lease(s)", expiredCount)
	}
}

// checkPoolReplenishment checks if pools need replenishment
func (s *Scheduler) checkPoolReplenishment(ctx context.Context) error {
	// Get all enabled pools
	pools, err := s.store.Pools.List(ctx, true)
	if err != nil {
		return err
	}

	for _, pool := range pools {
		// Check if pool is within scheduled hours (if scheduled mode enabled)
		if pool.ScheduledMode && !pool.IsWithinSchedule(time.Now()) {
			continue
		}

		// Get pool statistics
		stats, err := s.store.Pools.GetStats(ctx, pool.ID)
		if err != nil {
			log.Printf("Error getting stats for pool %s: %v", pool.Name, err)
			continue
		}

		// Check if pool needs replenishment (total < target_size)
		if stats.TotalClusters < pool.TargetSize {
			log.Printf("Pool %s needs replenishment: total=%d, min=%d, target=%d",
				pool.Name, stats.TotalClusters, pool.MinSize, pool.TargetSize)

			// Check if there's already a pending POOL_REPLENISH job for this pool
			existingJob, err := s.checkExistingReplenishJob(ctx, pool.ID)
			if err != nil {
				log.Printf("Error checking existing replenish job for pool %s: %v", pool.Name, err)
				continue
			}

			if existingJob != nil {
				log.Printf("Pool %s already has pending replenish job %s, skipping", pool.Name, existingJob.ID)
				continue
			}

			// Create POOL_REPLENISH job
			replenishJob := &types.Job{
				ID:          uuid.New().String(),
				ClusterID:   "",  // POOL_REPLENISH jobs don't have a cluster_id (pool_id is in metadata)
				JobType:     types.JobTypePoolReplenish,
				Status:      types.JobStatusPending,
				Attempt:     1,
				MaxAttempts: 3,
				Metadata: types.JobMetadata{
					"pool_id":      pool.ID,
					"pool_name":    pool.Name,
					"triggered_by": "pool_scheduler",
					"current_size": stats.TotalClusters,
					"min_size":     pool.MinSize,
					"target_size":  pool.TargetSize,
				},
			}

			if err := s.store.Jobs.Create(ctx, nil, replenishJob); err != nil {
				log.Printf("Error creating POOL_REPLENISH job for pool %s: %v", pool.Name, err)
				continue
			}

			log.Printf("Created POOL_REPLENISH job %s for pool %s", replenishJob.ID, pool.Name)
		}
	}

	return nil
}

// checkExpiredClusters checks for clusters exceeding max age and creates refresh jobs
func (s *Scheduler) checkExpiredClusters(ctx context.Context) error {
	// Get all enabled pools with auto-refresh enabled
	pools, err := s.store.Pools.List(ctx, true)
	if err != nil {
		return err
	}

	for _, pool := range pools {
		if !pool.AutoRefreshEnabled {
			continue
		}

		// Find clusters in this pool that exceed max age
		maxAge := time.Duration(pool.MaxClusterAgeDays) * 24 * time.Hour
		query := `
			SELECT id, name, created_at, pool_state
			FROM clusters
			WHERE pool_id = $1
			  AND pool_state IN ('READY', 'LEASED')
			  AND created_at < NOW() - $2::interval
		`

		rows, err := s.store.Pool().Query(ctx, query, pool.ID, maxAge.String())
		if err != nil {
			log.Printf("Error querying expired clusters for pool %s: %v", pool.Name, err)
			continue
		}

		expiredClusters := []struct {
			ID        string
			Name      string
			CreatedAt time.Time
			PoolState types.PoolState
		}{}

		for rows.Next() {
			var cluster struct {
				ID        string
				Name      string
				CreatedAt time.Time
				PoolState types.PoolState
			}
			if err := rows.Scan(&cluster.ID, &cluster.Name, &cluster.CreatedAt, &cluster.PoolState); err != nil {
				log.Printf("Error scanning expired cluster: %v", err)
				continue
			}
			expiredClusters = append(expiredClusters, cluster)
		}
		rows.Close()

		for _, cluster := range expiredClusters {
			age := time.Since(cluster.CreatedAt)

			// If cluster is LEASED, just mark as EXPIRED (don't refresh while in use)
			if cluster.PoolState == types.PoolStateLeased {
				log.Printf("Cluster %s in pool %s is expired (age=%s) but currently leased, marking as EXPIRED",
					cluster.Name, pool.Name, age)

				if err := s.store.Clusters.UpdatePoolState(ctx, cluster.ID, types.PoolStateExpired); err != nil {
					log.Printf("Error marking cluster %s as EXPIRED: %v", cluster.Name, err)
				}
				continue
			}

			// Cluster is READY and expired, mark as EXPIRED and create refresh job
			log.Printf("Cluster %s in pool %s is expired (age=%s > max=%s), creating refresh job",
				cluster.Name, pool.Name, age, maxAge)

			if err := s.store.Clusters.UpdatePoolState(ctx, cluster.ID, types.PoolStateExpired); err != nil {
				log.Printf("Error marking cluster %s as EXPIRED: %v", cluster.Name, err)
				continue
			}

			// Create POOL_REFRESH job
			refreshJob := &types.Job{
				ID:          uuid.New().String(),
				ClusterID:   cluster.ID,
				JobType:     types.JobTypePoolRefresh,
				Status:      types.JobStatusPending,
				Attempt:     1,
				MaxAttempts: 3,
				Metadata: types.JobMetadata{
					"pool_id":      pool.ID,
					"pool_name":    pool.Name,
					"triggered_by": "pool_scheduler",
					"cluster_age":  age.String(),
					"max_age":      maxAge.String(),
				},
			}

			if err := s.store.Jobs.Create(ctx, nil, refreshJob); err != nil {
				log.Printf("Error creating POOL_REFRESH job for cluster %s: %v", cluster.Name, err)
				continue
			}

			log.Printf("Created POOL_REFRESH job %s for expired cluster %s", refreshJob.ID, cluster.Name)
		}
	}

	return nil
}

// checkExistingReplenishJob checks if there's already a pending POOL_REPLENISH job for a pool
func (s *Scheduler) checkExistingReplenishJob(ctx context.Context, poolID string) (*types.Job, error) {
	query := `
		SELECT id, cluster_id, job_type, status, created_at
		FROM jobs
		WHERE job_type = 'POOL_REPLENISH'
		  AND cluster_id = $1
		  AND status IN ('PENDING', 'RUNNING')
		ORDER BY created_at DESC
		LIMIT 1
	`

	var job types.Job
	err := s.store.Pool().QueryRow(ctx, query, poolID).Scan(
		&job.ID,
		&job.ClusterID,
		&job.JobType,
		&job.Status,
		&job.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}

	return &job, nil
}
