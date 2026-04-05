package janitor

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/metrics"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Config holds janitor configuration
type Config struct {
	CheckInterval                  time.Duration
	StuckJobThreshold              time.Duration
	ExpiredLockCleanup             bool
	ExpiredKeyCleanup              bool
	OrphanDetection                bool
	OrphanCheckInterval            time.Duration
	DestroyedClusterRetentionDays  int  // Days to keep DESTROYED cluster records before deleting
	FailedClusterDirRetentionDays  int  // Days to keep work directories for FAILED clusters
	OrphanedDirCleanup             bool // Enable cleanup of orphaned work directories
}

// DefaultConfig returns default janitor configuration
func DefaultConfig() *Config {
	return &Config{
		CheckInterval:                 5 * time.Minute,
		StuckJobThreshold:             90 * time.Minute, // 1.5 hours to allow Windows virtual clusters to complete
		ExpiredLockCleanup:            true,
		ExpiredKeyCleanup:             true,
		OrphanDetection:               true,
		OrphanCheckInterval:           15 * time.Minute, // Less frequent to avoid AWS API rate limits
		DestroyedClusterRetentionDays: 30,               // Keep DESTROYED records for 30 days
		FailedClusterDirRetentionDays: 7,                // Keep FAILED directories for 7 days
		OrphanedDirCleanup:            true,             // Enable orphaned directory cleanup
	}
}

// Janitor performs periodic cleanup tasks
type Janitor struct {
	config           *Config
	store            *store.Store
	workDir          string
	running          bool
	ctx              context.Context
	cancel           context.CancelFunc
	lastOrphanCheck  time.Time
	metricsPublisher *metrics.Publisher
}

// NewJanitor creates a new janitor instance
func NewJanitor(config *Config, st *store.Store, workDir string) *Janitor {
	if config == nil {
		config = DefaultConfig()
	}

	// Create metrics publisher (best effort - don't fail if it can't be created)
	metricsPublisher, err := metrics.NewPublisher(context.Background())
	if err != nil {
		log.Printf("Warning: failed to create metrics publisher for janitor: %v", err)
	}

	return &Janitor{
		config:           config,
		store:            st,
		workDir:          workDir,
		running:          false,
		metricsPublisher: metricsPublisher,
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

	// Cleanup old DESTROYED cluster records from database
	if err := j.cleanupDestroyedClusters(ctx); err != nil {
		log.Printf("Error cleaning up destroyed clusters: %v", err)
	}

	// Cleanup work directories for old FAILED clusters
	if err := j.cleanupFailedClusterDirs(ctx); err != nil {
		log.Printf("Error cleaning up failed cluster directories: %v", err)
	}

	// Cleanup orphaned work directories
	if j.config.OrphanedDirCleanup {
		if err := j.cleanupOrphanedDirs(ctx); err != nil {
			log.Printf("Error cleaning up orphaned directories: %v", err)
		}
	}

	// Enforce work hours (hibernate/resume clusters)
	if err := j.enforceWorkHours(ctx); err != nil {
		log.Printf("Error enforcing work hours: %v", err)
	}

	// Detect orphaned AWS resources (less frequently to avoid rate limits)
	if j.config.OrphanDetection {
		// Check if enough time has passed since last orphan check
		if time.Since(j.lastOrphanCheck) >= j.config.OrphanCheckInterval {
			if err := j.detectOrphanedResources(ctx); err != nil {
				log.Printf("Error detecting orphaned resources: %v", err)
			}
			j.lastOrphanCheck = time.Now()
		}
	}

	// Update deployment time metrics
	if err := j.updateDeploymentMetrics(ctx); err != nil {
		log.Printf("Error updating deployment metrics: %v", err)
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
			Attempt:     1,
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
		log.Printf("Detected stuck job %s (type=%s, cluster=%s, started=%s, attempt=%d/%d)",
			job.ID, job.JobType, job.ClusterID, job.StartedAt, job.Attempt, job.MaxAttempts)

		// Release any locks held by this job first
		if err := j.store.JobLocks.Release(ctx, job.ClusterID, job.ID); err != nil {
			log.Printf("Failed to release lock for cluster %s: %v", job.ClusterID, err)
		}

		// Determine if job can be retried
		canRetry := job.Attempt < job.MaxAttempts

		if canRetry {
			// Reset job to PENDING for retry
			log.Printf("Resetting stuck job %s to PENDING for retry (attempt %d/%d)", job.ID, job.Attempt+1, job.MaxAttempts)
			errorCode := "WORKER_CRASHED"
			errorMessage := "Job exceeded maximum runtime (likely worker crash), resetting for retry"

			if err := j.store.Jobs.MarkFailedForRetry(ctx, job.ID, errorCode, errorMessage); err != nil {
				log.Printf("Failed to reset job %s for retry: %v", job.ID, err)
				continue
			}

			log.Printf("Reset stuck job %s to PENDING for retry", job.ID)
		} else {
			// No more retries available, mark as permanently failed
			log.Printf("Stuck job %s has exhausted retries (%d/%d), marking as FAILED", job.ID, job.Attempt, job.MaxAttempts)
			errorCode := "STUCK_JOB_TIMEOUT"
			errorMessage := "Job exceeded maximum runtime and exhausted all retry attempts"

			if err := j.store.Jobs.MarkFailed(ctx, job.ID, errorCode, errorMessage); err != nil {
				log.Printf("Failed to mark job %s as failed: %v", job.ID, err)
				continue
			}

			// Handle cluster status based on job type (only when permanently failing)
			if job.JobType == types.JobTypeDestroy || job.JobType == types.JobTypeJanitorDestroy {
				// For destroy jobs, mark cluster as DESTROY_FAILED since we cannot verify completion
				// The destroy job timed out or got stuck, so we don't know if AWS resources were deleted
				// An admin can manually verify and mark as DESTROYED, or reconciliation can detect drift
				log.Printf("Marking cluster %s as DESTROY_FAILED (stuck destroy job - verification required)", job.ClusterID)
				if err := j.store.Clusters.UpdateStatus(ctx, nil, job.ClusterID, types.ClusterStatusDestroyFailed); err != nil {
					log.Printf("Failed to mark cluster %s as destroy failed: %v", job.ClusterID, err)
				}
			} else {
				// For other job types, mark cluster as FAILED
				if err := j.store.Clusters.UpdateStatus(ctx, nil, job.ClusterID, types.ClusterStatusFailed); err != nil {
					log.Printf("Failed to update cluster %s status to FAILED: %v", job.ClusterID, err)
				}
			}

			log.Printf("Marked stuck job %s as permanently failed", job.ID)
		}
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

// cleanupDestroyedClusters removes DESTROYED cluster records older than retention period
func (j *Janitor) cleanupDestroyedClusters(ctx context.Context) error {
	// Calculate cutoff time based on retention days
	cutoff := time.Now().AddDate(0, 0, -j.config.DestroyedClusterRetentionDays)

	count, err := j.store.Clusters.DeleteDestroyedClusters(ctx, cutoff)
	if err != nil {
		return err
	}

	if count > 0 {
		log.Printf("Deleted %d DESTROYED cluster records older than %d days", count, j.config.DestroyedClusterRetentionDays)
	}

	return nil
}

// cleanupFailedClusterDirs removes work directories for FAILED clusters older than retention period
func (j *Janitor) cleanupFailedClusterDirs(ctx context.Context) error {
	// Get all FAILED clusters
	failedStatus := types.ClusterStatusFailed
	filters := store.ListFilters{
		Status: &failedStatus,
		Limit:  1000,
		Offset: 0,
	}

	clusters, _, err := j.store.Clusters.List(ctx, filters)
	if err != nil {
		return err
	}

	if len(clusters) == 0 {
		return nil
	}

	// Calculate cutoff time
	cutoff := time.Now().AddDate(0, 0, -j.config.FailedClusterDirRetentionDays)

	deletedCount := 0
	for _, cluster := range clusters {
		// Only cleanup old FAILED clusters
		if cluster.UpdatedAt.Before(cutoff) {
			clusterDir := filepath.Join(j.workDir, cluster.ID)

			// Check if directory exists
			if _, err := os.Stat(clusterDir); err == nil {
				// Remove directory
				if err := os.RemoveAll(clusterDir); err != nil {
					log.Printf("Failed to remove directory for FAILED cluster %s: %v", cluster.Name, err)
					continue
				}
				deletedCount++
				log.Printf("Removed work directory for FAILED cluster %s (failed %d days ago)", cluster.Name, int(time.Since(cluster.UpdatedAt).Hours()/24))
			}
		}
	}

	if deletedCount > 0 {
		log.Printf("Cleaned up %d work directories for FAILED clusters older than %d days", deletedCount, j.config.FailedClusterDirRetentionDays)
	}

	return nil
}

// cleanupOrphanedDirs removes work directories that don't have matching cluster records
func (j *Janitor) cleanupOrphanedDirs(ctx context.Context) error {
	// Get all cluster IDs from database
	clusters, err := j.store.Clusters.ListAll(ctx)
	if err != nil {
		return err
	}

	// Build set of valid cluster IDs
	validClusterIDs := make(map[string]bool)
	for _, cluster := range clusters {
		validClusterIDs[cluster.ID] = true
	}

	// List all directories in workDir
	entries, err := os.ReadDir(j.workDir)
	if err != nil {
		// If workDir doesn't exist, nothing to clean up
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	deletedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()

		// Skip if this cluster ID exists in database
		if validClusterIDs[dirName] {
			continue
		}

		// This is an orphaned directory - remove it
		orphanedPath := filepath.Join(j.workDir, dirName)
		if err := os.RemoveAll(orphanedPath); err != nil {
			log.Printf("Failed to remove orphaned directory %s: %v", dirName, err)
			continue
		}

		deletedCount++
		log.Printf("Removed orphaned work directory: %s", dirName)
	}

	if deletedCount > 0 {
		log.Printf("Cleaned up %d orphaned work directories", deletedCount)
	}

	return nil
}

// enforceWorkHours enforces work hours by hibernating/resuming clusters
func (j *Janitor) enforceWorkHours(ctx context.Context) error {
	// Get clusters with work hours enabled
	clusters, err := j.store.Clusters.GetClustersForWorkHoursEnforcement(ctx)
	if err != nil {
		return err
	}

	if len(clusters) == 0 {
		return nil
	}

	log.Printf("Checking work hours for %d clusters", len(clusters))

	actionsCount := 0
	for _, cluster := range clusters {
		// Get the user to access their timezone and default work hours
		user, err := j.store.Users.GetByID(ctx, cluster.OwnerID)
		if err != nil {
			log.Printf("Failed to get user for cluster %s: %v", cluster.Name, err)
			continue
		}

		// Determine effective work hours (cluster override or user default)
		var workHoursEnabled bool
		var workHoursStart, workHoursEnd time.Time
		var workDays int16

		if cluster.WorkHoursEnabled != nil {
			// Cluster has explicit override
			workHoursEnabled = *cluster.WorkHoursEnabled
			if workHoursEnabled {
				if cluster.WorkHoursStart != nil && cluster.WorkHoursEnd != nil && cluster.WorkDays != nil {
					workHoursStart = *cluster.WorkHoursStart
					workHoursEnd = *cluster.WorkHoursEnd
					workDays = *cluster.WorkDays
				} else {
					log.Printf("Cluster %s has work_hours_enabled=true but missing work hours config, skipping", cluster.Name)
					continue
				}
			} else {
				// Cluster explicitly disabled work hours
				continue
			}
		} else {
			// Use user's default work hours
			workHoursEnabled = user.WorkHoursEnabled
			if !workHoursEnabled {
				continue
			}
			workHoursStart = user.WorkHoursStart
			workHoursEnd = user.WorkHoursEnd
			workDays = user.WorkDays
		}

		// Load user's timezone
		location, err := time.LoadLocation(user.Timezone)
		if err != nil {
			log.Printf("Failed to load timezone %s for user %s: %v", user.Timezone, user.Email, err)
			continue
		}

		// Get current time in user's timezone
		nowInTZ := time.Now().In(location)

		// Check if within work hours
		withinWorkHours := isWithinWorkHours(nowInTZ, workHoursStart, workHoursEnd, workDays)

		// Log grace period check
		gracePeriodInfo := "none"
		if cluster.LastWorkHoursCheck != nil {
			gracePeriodInfo = cluster.LastWorkHoursCheck.In(location).Format("2006-01-02 15:04 MST")
		}
		log.Printf("[Work Hours Check] Cluster: %s, Status: %s, Within hours: %v, Current time: %s, Grace period until: %s",
			cluster.Name, cluster.Status, withinWorkHours, nowInTZ.Format("2006-01-02 15:04 MST"), gracePeriodInfo)

		// Determine action needed
		var action string
		var jobType types.JobType
		var newStatus types.ClusterStatus

		if cluster.Status == types.ClusterStatusReady && !withinWorkHours {
			// Don't hibernate if post-deployment is actively pending or in progress
			// Allow hibernation if: NULL (legacy), 'skipped', 'completed', or 'failed'
			if cluster.PostDeployStatus != nil &&
				(*cluster.PostDeployStatus == "pending" || *cluster.PostDeployStatus == "in_progress") {
				log.Printf("[Work Hours Action] SKIPPING HIBERNATION for %s: post-deployment %s", cluster.Name, *cluster.PostDeployStatus)
				// Update check timestamp so we don't spam logs
				if err := j.store.Clusters.UpdateLastWorkHoursCheck(ctx, cluster.ID); err != nil {
					log.Printf("Failed to update last_work_hours_check for cluster %s: %v", cluster.Name, err)
				}
				continue
			}
			action = "hibernate"
			jobType = types.JobTypeHibernate
			newStatus = types.ClusterStatusHibernating
			log.Printf("[Work Hours Action] WILL CREATE HIBERNATE JOB for %s (READY cluster outside work hours)", cluster.Name)
		} else if cluster.Status == types.ClusterStatusHibernated && withinWorkHours {
			action = "resume"
			jobType = types.JobTypeResume
			newStatus = types.ClusterStatusResuming
			log.Printf("[Work Hours Action] WILL CREATE RESUME JOB for %s (HIBERNATED cluster within work hours)", cluster.Name)
		} else {
			// No action needed, just update the check timestamp
			log.Printf("[Work Hours Action] NO ACTION NEEDED for %s (Status: %s, Within hours: %v)", cluster.Name, cluster.Status, withinWorkHours)
			if err := j.store.Clusters.UpdateLastWorkHoursCheck(ctx, cluster.ID); err != nil {
				log.Printf("Failed to update last_work_hours_check for cluster %s: %v", cluster.Name, err)
			}
			continue
		}

		// Skip hibernation for non-AWS platforms (only AWS supports hibernate/resume)
		if cluster.Platform != types.PlatformAWS {
			log.Printf("Skipping %s for cluster %s: platform %s does not support hibernation (AWS only)", action, cluster.Name, cluster.Platform)
			// Update the check timestamp so we don't log this repeatedly
			if err := j.store.Clusters.UpdateLastWorkHoursCheck(ctx, cluster.ID); err != nil {
				log.Printf("Failed to update last_work_hours_check for cluster %s: %v", cluster.Name, err)
			}
			continue
		}

		// Check for ANY active jobs on this cluster
		// We don't want to hibernate/resume while other jobs are running (POST_CONFIGURE, etc.)
		allJobs, err := j.store.Jobs.ListByClusterID(ctx, cluster.ID)
		if err != nil {
			log.Printf("[Work Hours Action] CRITICAL: Failed to check for active jobs for cluster %s: %v", cluster.Name, err)
			continue
		}

		// DEBUG: Log all jobs found for this cluster
		log.Printf("[Work Hours Action] Cluster %s: found %d total jobs", cluster.Name, len(allJobs))
		for _, job := range allJobs {
			log.Printf("[Work Hours Action]   - Job %s: type=%s, status=%s, created=%v",
				job.ID[:8], job.JobType, job.Status, job.CreatedAt.Format("15:04:05"))
		}

		hasActiveJob := false
		var activeJobType types.JobType
		var activeJobID string
		for _, job := range allJobs {
			if job.Status == types.JobStatusPending || job.Status == types.JobStatusRunning || job.Status == types.JobStatusRetrying {
				hasActiveJob = true
				activeJobType = job.JobType
				activeJobID = job.ID
				break
			}
		}

		if hasActiveJob {
			log.Printf("[Work Hours Action] Cluster %s has active %s job (ID: %s), skipping %s",
				cluster.Name, activeJobType, activeJobID[:8], action)
			// Update check timestamp so we don't spam logs
			if err := j.store.Clusters.UpdateLastWorkHoursCheck(ctx, cluster.ID); err != nil {
				log.Printf("Failed to update last_work_hours_check for cluster %s: %v", cluster.Name, err)
			}
			continue
		}

		// Log that we're about to create a HIBERNATE/RESUME job
		log.Printf("[Work Hours Action] Creating %s job for cluster %s (no active jobs found)", action, cluster.Name)

		// Create the job
		job := &types.Job{
			ID:          uuid.New().String(),
			ClusterID:   cluster.ID,
			JobType:     jobType,
			Status:      types.JobStatusPending,
			Metadata:    types.JobMetadata{"reason": "WORK_HOURS_ENFORCEMENT", "triggered_by": "janitor"},
			MaxAttempts: 3,
			Attempt:     1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := j.store.Jobs.Create(ctx, job); err != nil {
			log.Printf("Failed to create %s job for cluster %s: %v", action, cluster.Name, err)
			continue
		}

		// Update cluster status
		if err := j.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, newStatus); err != nil {
			log.Printf("Failed to update cluster %s status to %s: %v", cluster.Name, newStatus, err)
			continue
		}

		// Update last check timestamp
		if err := j.store.Clusters.UpdateLastWorkHoursCheck(ctx, cluster.ID); err != nil {
			log.Printf("Failed to update last_work_hours_check for cluster %s: %v", cluster.Name, err)
		}

		actionsCount++
		log.Printf("[Work Hours Job Created] %s job %s for cluster %s | Time: %s | Within hours: %v | Grace period was: %s",
			strings.ToUpper(action), job.ID, cluster.Name, nowInTZ.Format("2006-01-02 15:04 MST"), withinWorkHours, gracePeriodInfo)
	}

	if actionsCount > 0 {
		log.Printf("Work hours enforcement: created %d jobs", actionsCount)
	}

	return nil
}

// isWithinWorkHours checks if the given time is within work hours
func isWithinWorkHours(now time.Time, start, end time.Time, workDaysMask int16) bool {
	// Check if today is a work day
	if !types.IsWorkDay(workDaysMask, now.Weekday()) {
		return false
	}

	// Extract current time in minutes since midnight
	currentMinutes := now.Hour()*60 + now.Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()

	// Handle normal case (start < end, e.g., 09:00 - 17:00)
	if startMinutes < endMinutes {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}

	// Handle wraparound case (start > end, e.g., 22:00 - 06:00 night shift)
	// Current time is within work hours if it's >= start OR < end
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}

// updateDeploymentMetrics calculates and updates deployment time statistics for all profiles.
// This runs every 5 minutes (as part of the janitor cycle) to keep metrics fresh based on
// the last 30 successful CREATE jobs per profile.
func (j *Janitor) updateDeploymentMetrics(ctx context.Context) error {
	count, err := j.store.ProfileDeploymentMetrics.UpdateAllMetrics(ctx)
	if err != nil {
		return err
	}

	if count > 0 {
		log.Printf("Updated deployment metrics for %d profiles", count)
	}

	return nil
}
