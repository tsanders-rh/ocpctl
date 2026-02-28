package janitor

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Config holds janitor configuration
type Config struct {
	CheckInterval         time.Duration
	StuckJobThreshold     time.Duration
	ExpiredLockCleanup    bool
	ExpiredKeyCleanup     bool
}

// DefaultConfig returns default janitor configuration
func DefaultConfig() *Config {
	return &Config{
		CheckInterval:      5 * time.Minute,
		StuckJobThreshold:  2 * time.Hour,
		ExpiredLockCleanup: true,
		ExpiredKeyCleanup:  true,
	}
}

// Janitor performs periodic cleanup tasks
type Janitor struct {
	config  *Config
	store   *store.Store
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewJanitor creates a new janitor instance
func NewJanitor(config *Config, st *store.Store) *Janitor {
	if config == nil {
		config = DefaultConfig()
	}

	return &Janitor{
		config:  config,
		store:   st,
		running: false,
	}
}

// Start starts the janitor loop
func (j *Janitor) Start(ctx context.Context) error {
	j.ctx, j.cancel = context.WithCancel(ctx)
	j.running = true

	log.Printf("Janitor starting (check_interval=%s)", j.config.CheckInterval)

	// Run immediately on start
	j.run()

	// Start periodic loop
	ticker := time.NewTicker(j.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-j.ctx.Done():
			log.Printf("Janitor shutting down")
			return j.ctx.Err()

		case <-ticker.C:
			j.run()
		}
	}
}

// Stop stops the janitor gracefully
func (j *Janitor) Stop() {
	if j.cancel != nil {
		j.cancel()
	}
	j.running = false
}

// run performs all cleanup tasks
func (j *Janitor) run() {
	ctx := context.Background()

	log.Printf("Janitor running cleanup tasks")

	// Check for expired clusters and create destroy jobs
	if err := j.cleanupExpiredClusters(ctx); err != nil {
		log.Printf("Error cleaning up expired clusters: %v", err)
	}

	// Detect and handle stuck jobs
	if err := j.cleanupStuckJobs(ctx); err != nil {
		log.Printf("Error cleaning up stuck jobs: %v", err)
	}

	// Cleanup expired locks
	if j.config.ExpiredLockCleanup {
		if err := j.cleanupExpiredLocks(ctx); err != nil {
			log.Printf("Error cleaning up expired locks: %v", err)
		}
	}

	// Cleanup expired idempotency keys
	if j.config.ExpiredKeyCleanup {
		if err := j.cleanupExpiredKeys(ctx); err != nil {
			log.Printf("Error cleaning up expired keys: %v", err)
		}
	}

	log.Printf("Janitor cleanup tasks completed")
}

// cleanupExpiredClusters checks for clusters past their TTL and creates destroy jobs
func (j *Janitor) cleanupExpiredClusters(ctx context.Context) error {
	expired, err := j.store.Clusters.GetExpiredClusters(ctx)
	if err != nil {
		return err
	}

	if len(expired) == 0 {
		return nil
	}

	log.Printf("Found %d expired clusters", len(expired))

	for _, cluster := range expired {
		// Check if destroy job already exists
		jobs, err := j.store.Jobs.ListByClusterID(ctx, cluster.ID)
		if err != nil {
			log.Printf("Failed to list jobs for cluster %s: %v", cluster.ID, err)
			continue
		}

		// Check if there's already a pending/running destroy job
		hasDestroyJob := false
		for _, job := range jobs {
			if job.JobType == types.JobTypeDestroy || job.JobType == types.JobTypeJanitorDestroy {
				if job.Status == types.JobStatusPending || job.Status == types.JobStatusRunning {
					hasDestroyJob = true
					break
				}
			}
		}

		if hasDestroyJob {
			log.Printf("Cluster %s already has a destroy job, skipping", cluster.Name)
			continue
		}

		// Create janitor destroy job
		job := &types.Job{
			ID:          uuid.New().String(),
			ClusterID:   cluster.ID,
			JobType:     types.JobTypeJanitorDestroy,
			Status:      types.JobStatusPending,
			Metadata:    types.JobMetadata{"reason": "TTL_EXPIRED"},
			MaxAttempts: 3,
			Attempt:     0,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := j.store.Jobs.Create(ctx, job); err != nil {
			log.Printf("Failed to create destroy job for cluster %s: %v", cluster.Name, err)
			continue
		}

		// Update cluster status to DESTROYING
		if err := j.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroying); err != nil {
			log.Printf("Failed to update cluster %s status: %v", cluster.Name, err)
		}

		log.Printf("Created destroy job for expired cluster %s (TTL expired)", cluster.Name)
	}

	return nil
}

// cleanupStuckJobs detects jobs stuck in RUNNING status
func (j *Janitor) cleanupStuckJobs(ctx context.Context) error {
	stuck, err := j.store.Jobs.GetStuckJobs(ctx, j.config.StuckJobThreshold)
	if err != nil {
		return err
	}

	if len(stuck) == 0 {
		return nil
	}

	log.Printf("Found %d stuck jobs", len(stuck))

	for _, job := range stuck {
		log.Printf("Detected stuck job %s (type=%s, cluster=%s, started=%s)",
			job.ID, job.JobType, job.ClusterID, job.StartedAt)

		// Mark job as failed
		errorCode := "STUCK_JOB_TIMEOUT"
		errorMessage := "Job exceeded maximum runtime and was terminated by janitor"

		if err := j.store.Jobs.MarkFailed(ctx, job.ID, errorCode, errorMessage); err != nil {
			log.Printf("Failed to mark job %s as failed: %v", job.ID, err)
			continue
		}

		// Release any locks held by this job
		if err := j.store.JobLocks.Release(ctx, job.ClusterID, job.ID); err != nil {
			log.Printf("Failed to release lock for cluster %s: %v", job.ClusterID, err)
		}

		// Update cluster status to FAILED
		if err := j.store.Clusters.UpdateStatus(ctx, nil, job.ClusterID, types.ClusterStatusFailed); err != nil {
			log.Printf("Failed to update cluster %s status to FAILED: %v", job.ClusterID, err)
		}

		log.Printf("Marked stuck job %s as failed and released locks", job.ID)
	}

	return nil
}

// cleanupExpiredLocks removes expired job locks
func (j *Janitor) cleanupExpiredLocks(ctx context.Context) error {
	count, err := j.store.JobLocks.CleanupExpired(ctx)
	if err != nil {
		return err
	}

	if count > 0 {
		log.Printf("Cleaned up %d expired job locks", count)
	}

	return nil
}

// cleanupExpiredKeys removes expired idempotency keys
func (j *Janitor) cleanupExpiredKeys(ctx context.Context) error {
	count, err := j.store.Idempotency.CleanupExpired(ctx)
	if err != nil {
		return err
	}

	if count > 0 {
		log.Printf("Cleaned up %d expired idempotency keys", count)
	}

	return nil
}
