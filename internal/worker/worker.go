package worker

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/metrics"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Config holds worker configuration
type Config struct {
	WorkerID       string
	PollInterval   time.Duration
	LockTimeout    time.Duration
	WorkDir        string
	S3BucketName   string // S3 bucket for storing cluster artifacts
	MaxConcurrent  int
	RetryBackoff   time.Duration
	MaxRetries     int
}

// DefaultConfig returns default worker configuration
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	workerID := fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8])

	// Try to get EC2 instance ID if running on EC2
	if instanceID := getEC2InstanceID(); instanceID != "" {
		workerID = fmt.Sprintf("i-%s", instanceID)
	}

	// Get S3 bucket from environment or use default
	s3Bucket := os.Getenv("S3_ARTIFACT_BUCKET")
	if s3Bucket == "" {
		s3Bucket = "ocpctl-binaries" // Default to same bucket as worker binaries
	}

	return &Config{
		WorkerID:      workerID,
		PollInterval:  10 * time.Second,
		LockTimeout:   30 * time.Minute,
		WorkDir:       "/tmp/ocpctl",
		S3BucketName:  s3Bucket,
		MaxConcurrent: 3,
		RetryBackoff:  30 * time.Second,
		MaxRetries:    3,
	}
}

// getEC2InstanceID retrieves the EC2 instance ID from metadata service
func getEC2InstanceID() string {
	// Use IMDSv2 (more secure)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get token first (IMDSv2)
	tokenReq, err := http.NewRequestWithContext(ctx, "PUT",
		"http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		return ""
	}
	tokenReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")

	client := &http.Client{Timeout: 2 * time.Second}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return ""
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != 200 {
		return ""
	}

	tokenBytes, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return ""
	}
	token := string(tokenBytes)

	// Get instance ID using token
	idReq, err := http.NewRequestWithContext(ctx, "GET",
		"http://169.254.169.254/latest/meta-data/instance-id", nil)
	if err != nil {
		return ""
	}
	idReq.Header.Set("X-aws-ec2-metadata-token", token)

	idResp, err := client.Do(idReq)
	if err != nil {
		return ""
	}
	defer idResp.Body.Close()

	if idResp.StatusCode != 200 {
		return ""
	}

	instanceIDBytes, err := io.ReadAll(idResp.Body)
	if err != nil {
		return ""
	}

	return string(instanceIDBytes)
}

// getEC2ASGName retrieves the Auto Scaling Group name from EC2 tags
func getEC2ASGName() string {
	// Use IMDSv2
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get token first (IMDSv2)
	tokenReq, err := http.NewRequestWithContext(ctx, "PUT",
		"http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		return ""
	}
	tokenReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")

	client := &http.Client{Timeout: 2 * time.Second}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return ""
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != 200 {
		return ""
	}

	tokenBytes, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return ""
	}
	token := string(tokenBytes)

	// Get instance tags to find ASG name
	tagsReq, err := http.NewRequestWithContext(ctx, "GET",
		"http://169.254.169.254/latest/meta-data/tags/instance/aws:autoscaling:groupName", nil)
	if err != nil {
		return ""
	}
	tagsReq.Header.Set("X-aws-ec2-metadata-token", token)

	tagsResp, err := client.Do(tagsReq)
	if err != nil {
		return ""
	}
	defer tagsResp.Body.Close()

	if tagsResp.StatusCode != 200 {
		return ""
	}

	asgNameBytes, err := io.ReadAll(tagsResp.Body)
	if err != nil {
		return ""
	}

	return string(asgNameBytes)
}

// Worker processes background jobs
type Worker struct {
	config    *Config
	store     *store.Store
	processor *JobProcessor
	metrics   *metrics.Publisher
	asgName   string
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewWorker creates a new worker instance
func NewWorker(config *Config, st *store.Store, profileRegistry *profile.Registry) *Worker {
	if config == nil {
		config = DefaultConfig()
	}

	// Initialize metrics publisher (non-fatal if it fails)
	metricsPublisher, err := metrics.NewPublisher(context.Background())
	if err != nil {
		log.Printf("Warning: Failed to initialize CloudWatch metrics: %v", err)
	}

	// Get ASG name if running in an Auto Scaling Group
	asgName := getEC2ASGName()
	if asgName != "" {
		log.Printf("Running in Auto Scaling Group: %s", asgName)
	}

	return &Worker{
		config:    config,
		store:     st,
		processor: NewJobProcessor(config, st, profileRegistry),
		metrics:   metricsPublisher,
		asgName:   asgName,
		running:   false,
	}
}

// Start starts the worker loop
func (w *Worker) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true

	log.Printf("Worker %s starting (poll=%s, max_concurrent=%d)",
		w.config.WorkerID, w.config.PollInterval, w.config.MaxConcurrent)

	// Publish worker active metric
	if w.metrics != nil {
		dims := map[string]string{
			"WorkerID": w.config.WorkerID,
		}
		if err := w.metrics.PublishGauge(ctx, metrics.MetricWorkerActive, 1, dims); err != nil {
			log.Printf("Warning: Failed to publish worker active metric: %v", err)
		}
	}

	// Create work directory
	if err := os.MkdirAll(w.config.WorkDir, 0755); err != nil {
		return fmt.Errorf("create work directory: %w", err)
	}

	// Start polling loop
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			log.Printf("Worker %s shutting down", w.config.WorkerID)
			// Publish worker inactive metric
			if w.metrics != nil {
				dims := map[string]string{
					"WorkerID": w.config.WorkerID,
				}
				_ = w.metrics.PublishGauge(ctx, metrics.MetricWorkerActive, 0, dims)
			}
			return w.ctx.Err()

		case <-ticker.C:
			w.poll()
		}
	}
}

// Stop stops the worker gracefully
func (w *Worker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.running = false
}

// poll fetches and processes pending jobs
func (w *Worker) poll() {
	ctx := context.Background()

	// Get total count of all pending jobs in the queue
	// This is used for auto-scaling metrics
	totalPending, err := w.store.Jobs.CountPending(ctx)
	if err != nil {
		log.Printf("Error counting pending jobs: %v", err)
	} else if w.metrics != nil {
		// Publish pending jobs metric for auto-scaling
		dims := map[string]string{}
		// Include ASG name dimension if running in an Auto Scaling Group
		if w.asgName != "" {
			dims["AutoScalingGroupName"] = w.asgName
		}
		if err := w.metrics.PublishGauge(ctx, metrics.MetricPendingJobs, float64(totalPending), dims); err != nil {
			log.Printf("Warning: Failed to publish pending jobs metric: %v", err)
		}
	}

	// Get pending jobs for this worker to process
	jobs, err := w.store.Jobs.GetPending(ctx, w.config.MaxConcurrent)
	if err != nil {
		log.Printf("Error fetching pending jobs: %v", err)
		return
	}

	if len(jobs) == 0 {
		return
	}

	log.Printf("Found %d pending jobs", len(jobs))

	// Process jobs concurrently
	for _, job := range jobs {
		go w.processJob(job)
	}
}

// processJob processes a single job
func (w *Worker) processJob(job *types.Job) {
	ctx := context.Background()

	// Try to acquire lock
	lock, err := w.acquireLock(ctx, job)
	if err != nil {
		log.Printf("Failed to acquire lock for job %s: %v", job.ID, err)
		return
	}
	if lock == nil {
		// Lock already held by another worker
		return
	}

	// Ensure lock is released
	defer w.releaseLock(ctx, job.ClusterID, job.ID)

	// Check if cluster still exists and is in a valid state for this job
	cluster, err := w.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		log.Printf("Failed to get cluster %s for job %s: %v", job.ClusterID, job.ID, err)
		// Mark job as failed if cluster doesn't exist
		errorMsg := fmt.Sprintf("Cluster not found: %v", err)
		if markErr := w.store.Jobs.MarkFailed(ctx, job.ID, "CLUSTER_NOT_FOUND", errorMsg); markErr != nil {
			log.Printf("Failed to mark job %s as failed: %v", job.ID, markErr)
		}
		return
	}

	// Auto-cancel jobs for DESTROYED clusters (except DESTROY jobs themselves)
	if cluster.Status == types.ClusterStatusDestroyed && job.JobType != types.JobTypeDestroy {
		log.Printf("Auto-cancelling job %s (type=%s): cluster %s is already DESTROYED",
			job.ID, job.JobType, cluster.Name)
		errorMsg := fmt.Sprintf("Cluster %s was destroyed before job could execute", cluster.Name)
		if markErr := w.store.Jobs.MarkFailed(ctx, job.ID, "CLUSTER_DESTROYED", errorMsg); markErr != nil {
			log.Printf("Failed to mark job %s as failed: %v", job.ID, markErr)
		}
		return
	}

	// Update job status to RUNNING
	if err := w.store.Jobs.MarkStarted(ctx, job.ID); err != nil {
		log.Printf("Failed to mark job %s as started: %v", job.ID, err)
		return
	}

	log.Printf("Processing job %s (type=%s, cluster=%s)", job.ID, job.JobType, job.ClusterID)

	// Process the job
	err = w.processor.Process(ctx, job)

	// Update job status based on result
	if err != nil {
		log.Printf("Job %s failed: %v", job.ID, err)
		w.handleJobFailure(ctx, job, err)
	} else {
		log.Printf("Job %s completed successfully", job.ID)
		w.handleJobSuccess(ctx, job)
	}
}

// acquireLock attempts to acquire a lock for the cluster
func (w *Worker) acquireLock(ctx context.Context, job *types.Job) (*types.JobLock, error) {
	expiresAt := time.Now().Add(w.config.LockTimeout)

	lock := &types.JobLock{
		ClusterID: job.ClusterID,
		JobID:     job.ID,
		LockedBy:  w.config.WorkerID,
		LockedAt:  time.Now(),
		ExpiresAt: expiresAt,
	}

	acquired, err := w.store.JobLocks.TryAcquire(ctx, lock)
	if err != nil {
		return nil, err
	}

	if !acquired {
		return nil, nil
	}

	return lock, nil
}

// releaseLock releases the cluster lock
func (w *Worker) releaseLock(ctx context.Context, clusterID, jobID string) {
	if err := w.store.JobLocks.Release(ctx, clusterID, jobID); err != nil {
		log.Printf("Failed to release lock for cluster %s: %v", clusterID, err)
	}
}

// handleJobSuccess marks job as succeeded
func (w *Worker) handleJobSuccess(ctx context.Context, job *types.Job) {
	if err := w.store.Jobs.MarkSucceeded(ctx, job.ID); err != nil {
		log.Printf("Failed to mark job %s as succeeded: %v", job.ID, err)
	}
}

// handleJobFailure handles job failure with retry logic
func (w *Worker) handleJobFailure(ctx context.Context, job *types.Job, jobErr error) {
	// Check if this is a "not ready" error - defer without incrementing attempts
	if types.IsNotReadyError(jobErr) {
		log.Printf("Job %s deferred: %v (will retry when ready)", job.ID, jobErr)

		// Reset to PENDING without incrementing attempts
		if err := w.store.Jobs.UpdateStatus(ctx, job.ID, types.JobStatusPending); err != nil {
			log.Printf("Failed to reset job %s to pending: %v", job.ID, err)
		}
		return
	}

	// For all other errors, increment attempt counter
	if err := w.store.Jobs.IncrementAttempt(ctx, job.ID); err != nil {
		log.Printf("Failed to increment attempt for job %s: %v", job.ID, err)
		return
	}

	// Check if max attempts reached
	if job.Attempt+1 >= job.MaxAttempts {
		log.Printf("Job %s reached max attempts (%d), marking as failed", job.ID, job.MaxAttempts)

		// Mark job as permanently failed
		errorCode := "MAX_RETRIES_EXCEEDED"
		errorMessage := fmt.Sprintf("Job failed after %d attempts: %v", job.MaxAttempts, jobErr)
		if err := w.store.Jobs.MarkFailed(ctx, job.ID, errorCode, errorMessage); err != nil {
			log.Printf("Failed to mark job %s as failed: %v", job.ID, err)
		}

		// Update cluster status to FAILED
		if err := w.store.Clusters.UpdateStatus(ctx, nil, job.ClusterID, types.ClusterStatusFailed); err != nil {
			log.Printf("Failed to update cluster %s status to FAILED: %v", job.ClusterID, err)
		}
	} else {
		// Schedule retry
		log.Printf("Job %s will be retried (attempt %d/%d)", job.ID, job.Attempt+1, job.MaxAttempts)

		// For CREATE jobs, clean up partial infrastructure before retry
		if job.JobType == types.JobTypeCreate {
			w.cleanupPartialDeployment(ctx, job)
		}

		// Update job status back to PENDING for retry
		if err := w.store.Jobs.UpdateStatus(ctx, job.ID, types.JobStatusPending); err != nil {
			log.Printf("Failed to reset job %s to pending: %v", job.ID, err)
		}

		// TODO: Implement exponential backoff delay
	}
}

// cleanupPartialDeployment cleans up partial infrastructure from a failed CREATE job
func (w *Worker) cleanupPartialDeployment(ctx context.Context, job *types.Job) {
	// Get cluster metadata for DNS cleanup
	cluster, err := w.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		log.Printf("Warning: failed to get cluster for DNS cleanup: %v", err)
	} else {
		// Clean up DNS records before openshift-install destroy
		// This ensures DNS cleanup happens even if workDir doesn't exist
		log.Printf("Cleaning up DNS records for cluster %s.%s", cluster.Name, cluster.BaseDomain)
		dnsCleaner := NewDNSCleaner(cluster.Region)
		if err := dnsCleaner.CleanupClusterDNS(ctx, cluster.Name, cluster.BaseDomain); err != nil {
			log.Printf("Warning: DNS cleanup failed: %v", err)
		} else {
			log.Printf("Successfully cleaned up DNS records")
		}
	}

	workDir := fmt.Sprintf("%s/%s", w.config.WorkDir, job.ClusterID)

	// Check if work directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		log.Printf("Work directory %s does not exist, skipping openshift-install destroy", workDir)
		return
	}

	log.Printf("Cleaning up partial deployment for job %s (cluster %s)", job.ID, job.ClusterID)

	// Get cluster version for version-specific installer
	if cluster != nil {
		// Create version-specific installer
		inst, err := installer.NewInstallerForVersion(cluster.Version)
		if err != nil {
			log.Printf("Warning: failed to create installer for version %s: %v", cluster.Version, err)
			log.Printf("Skipping openshift-install destroy, proceeding with directory cleanup")
		} else {
			// Run openshift-install destroy to clean up partial infrastructure
			output, err := inst.DestroyCluster(ctx, workDir)

			if err != nil {
				// Log the error but don't fail - allow retry to proceed
				log.Printf("Warning: cleanup failed for job %s: %v\nOutput: %s", job.ID, err, output)
				log.Printf("Proceeding with retry despite cleanup failure")
			} else {
				log.Printf("Successfully cleaned up partial deployment for job %s", job.ID)
			}
		}
	} else {
		log.Printf("Cannot create installer without cluster metadata, skipping destroy")
	}

	// Remove work directory to ensure clean slate for retry
	if err := os.RemoveAll(workDir); err != nil {
		log.Printf("Warning: failed to remove work directory %s: %v", workDir, err)
	}

	// Clean up temporary files created by openshift-install
	w.cleanupTempFiles()
}

// cleanupTempFiles removes temporary files created by openshift-install
func (w *Worker) cleanupTempFiles() {
	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		tmpDir = "/tmp"
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		log.Printf("Warning: failed to read temp directory %s: %v", tmpDir, err)
		return
	}

	now := time.Now()
	cleaned := 0
	for _, entry := range entries {
		// Only clean openshift-related temp files
		name := entry.Name()
		if !filepath.HasPrefix(name, "openshift-") {
			continue
		}

		fullPath := filepath.Join(tmpDir, name)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		// Remove files/dirs older than 1 hour
		if now.Sub(info.ModTime()) > 1*time.Hour {
			if err := os.RemoveAll(fullPath); err != nil {
				log.Printf("Warning: failed to remove temp file %s: %v", fullPath, err)
			} else {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		log.Printf("Cleaned up %d old temp files from %s", cleaned, tmpDir)
	}
}
