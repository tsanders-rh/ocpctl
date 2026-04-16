package worker

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
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
		LockTimeout:   90 * time.Minute, // Must be longer than longest operation (eksctl: 60min)
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
	ctx, cancel := context.WithTimeout(context.Background(), IMDSRequestTimeout)
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
	ctx, cancel := context.WithTimeout(context.Background(), IMDSRequestTimeout)
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

// ActiveJobInfo tracks information about a currently running job
type ActiveJobInfo struct {
	JobID       string
	JobType     string
	ClusterID   string
	ClusterName string
	StartedAt   time.Time
}

// Worker processes background jobs
type Worker struct {
	config     *Config
	store      *store.Store
	processor  *JobProcessor
	metrics    *metrics.Publisher
	asgName    string
	running    bool
	ctx        context.Context
	cancel     context.CancelFunc
	jobWg      sync.WaitGroup               // Tracks running job goroutines for graceful shutdown
	activeJobs map[string]*ActiveJobInfo    // Tracks currently running jobs
	jobsMu     sync.RWMutex                 // Protects activeJobs map
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
		config:     config,
		store:      st,
		processor:  NewJobProcessor(config, st, profileRegistry),
		metrics:    metricsPublisher,
		asgName:    asgName,
		running:    false,
		activeJobs: make(map[string]*ActiveJobInfo),
	}
}

// Start starts the worker loop
func (w *Worker) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true

	log.Printf("Worker %s starting (poll=%s, max_concurrent=%d)",
		w.config.WorkerID, w.config.PollInterval, w.config.MaxConcurrent)

	// Validate required binaries are installed before processing jobs
	if err := w.validateInstallerBinaries(); err != nil {
		return fmt.Errorf("preflight check failed: %w", err)
	}
	log.Printf("✓ All required installer binaries are available")

	// Publish worker active metric
	if w.metrics != nil {
		dims := map[string]string{
			"WorkerID": w.config.WorkerID,
		}
		if err := w.metrics.PublishGauge(ctx, metrics.MetricWorkerActive, 1, dims); err != nil {
			log.Printf("Warning: Failed to publish worker active metric: %v", err)
		}
	}

	// Create work directory with restrictive permissions (0700)
	// This directory will contain cluster-specific subdirectories with sensitive files
	if err := os.MkdirAll(w.config.WorkDir, 0700); err != nil {
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
	log.Printf("Stopping worker, waiting for running jobs to complete...")

	// Cancel context to signal all jobs to stop
	if w.cancel != nil {
		w.cancel()
	}
	w.running = false

	// Wait for all running jobs to complete (with timeout)
	// Use a channel to implement timeout on WaitGroup
	done := make(chan struct{})
	go func() {
		w.jobWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("All running jobs completed, worker stopped gracefully")
	case <-time.After(WorkerShutdownTimeout):
		log.Printf("WARNING: Timeout waiting for jobs to complete, some jobs may still be running")
	}
}

// registerJob adds a job to the active jobs list
func (w *Worker) registerJob(jobID, jobType, clusterID, clusterName string) {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()

	w.activeJobs[jobID] = &ActiveJobInfo{
		JobID:       jobID,
		JobType:     jobType,
		ClusterID:   clusterID,
		ClusterName: clusterName,
		StartedAt:   time.Now(),
	}
}

// unregisterJob removes a job from the active jobs list
func (w *Worker) unregisterJob(jobID string) {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()

	delete(w.activeJobs, jobID)
}

// GetActiveJobs returns a snapshot of currently running jobs
func (w *Worker) GetActiveJobs() []*ActiveJobInfo {
	w.jobsMu.RLock()
	defer w.jobsMu.RUnlock()

	jobs := make([]*ActiveJobInfo, 0, len(w.activeJobs))
	for _, job := range w.activeJobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// poll fetches and processes pending jobs
func (w *Worker) poll() {
	// Use worker's context (not Background) to enable graceful shutdown
	ctx := w.ctx

	// Clean up expired locks before processing jobs
	// This prevents stale locks from blocking job processing
	expiredLocks, err := w.store.JobLocks.CleanupExpiredLocks(ctx)
	if err != nil {
		log.Printf("Warning: Failed to cleanup expired locks: %v", err)
	} else if len(expiredLocks) > 0 {
		log.Printf("Cleaned up %d expired locks", len(expiredLocks))
	}

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

	// Extract unique cluster IDs from jobs
	clusterIDs := make([]string, 0, len(jobs))
	clusterIDSet := make(map[string]bool)
	for _, job := range jobs {
		if !clusterIDSet[job.ClusterID] {
			clusterIDs = append(clusterIDs, job.ClusterID)
			clusterIDSet[job.ClusterID] = true
		}
	}

	// Batch fetch all clusters in a SINGLE query (prevents N+1 pattern)
	clusters, err := w.store.Clusters.GetByIDs(ctx, clusterIDs)
	if err != nil {
		log.Printf("Error batch fetching clusters: %v", err)
		return
	}

	// Create lookup map for O(1) access
	clusterMap := make(map[string]*types.Cluster)
	for _, cluster := range clusters {
		clusterMap[cluster.ID] = cluster
	}

	// Process jobs concurrently with pre-fetched cluster data
	// Pass worker context so jobs can be cancelled on shutdown
	// Use WaitGroup to track running jobs for graceful shutdown
	for _, job := range jobs {
		cluster := clusterMap[job.ClusterID]
		if cluster == nil {
			log.Printf("Cluster %s not found for job %s, skipping", job.ClusterID, job.ID)
			// Mark job as failed since cluster doesn't exist
			errorMsg := "Cluster not found"
			if markErr := w.store.Jobs.MarkFailed(ctx, job.ID, "CLUSTER_NOT_FOUND", errorMsg); markErr != nil {
				log.Printf("Failed to mark job %s as failed: %v", job.ID, markErr)
			}
			continue
		}

		// Track this job goroutine
		w.jobWg.Add(1)
		go w.processJob(w.ctx, job, cluster)
	}
}

// processJob processes a single job
// Accepts parent context to enable cancellation during shutdown
// Cluster is passed as parameter (pre-fetched) to avoid N+1 query pattern
func (w *Worker) processJob(ctx context.Context, job *types.Job, cluster *types.Cluster) {
	// Signal WaitGroup when job completes (for graceful shutdown)
	defer w.jobWg.Done()

	// Unregister job when it completes (success or failure)
	defer w.unregisterJob(job.ID)

	// Check if context is already cancelled (worker shutting down)
	select {
	case <-ctx.Done():
		log.Printf("Job %s cancelled before processing (worker shutdown)", job.ID)
		return
	default:
	}

	// Create child context with timeout for this specific job
	// This allows both timeout-based cancellation AND parent cancellation
	ctx, cancel := context.WithTimeout(ctx, w.config.LockTimeout)
	defer cancel()

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

	// Register job as active (for health endpoint and monitoring)
	w.registerJob(job.ID, string(job.JobType), job.ClusterID, cluster.Name)

	// Delete old deployment logs from previous attempts
	// This ensures each retry starts with a clean slate and sequence numbers start from 0
	if err := w.store.DeploymentLogs.DeleteByJobID(ctx, job.ID); err != nil {
		log.Printf("Warning: failed to delete old deployment logs for job %s: %v", job.ID, err)
		// Continue processing - this is not fatal
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
	// Try to release lock with the current context first
	err := w.store.JobLocks.Release(ctx, clusterID, jobID)
	if err == nil {
		return
	}

	// If release failed (possibly due to context timeout), retry with background context
	log.Printf("Lock release failed (attempt 1), retrying with background context: %v", err)

	retryCtx, cancel := context.WithTimeout(context.Background(), LockReleaseRetryTimeout)
	defer cancel()

	if retryErr := w.store.JobLocks.Release(retryCtx, clusterID, jobID); retryErr != nil {
		// CRITICAL: Lock release failed after retry - cluster will be locked indefinitely
		log.Printf("CRITICAL: Failed to release lock for cluster %s (job %s) after retry: %v",
			clusterID, jobID, retryErr)
		log.Printf("CRITICAL: Manual intervention required - cluster %s may be locked. Run: DELETE FROM job_locks WHERE cluster_id = '%s'",
			clusterID, clusterID)
		// TODO: Send alert to operations team when alerting system is available
	}
}

// handleJobSuccess marks job as succeeded and saves metadata
func (w *Worker) handleJobSuccess(ctx context.Context, job *types.Job) {
	if err := w.store.Jobs.MarkSucceeded(ctx, job.ID, job.Metadata); err != nil {
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

	// Check if max attempts reached BEFORE incrementing
	// job.Attempt is 1-indexed, so if attempt 3 just failed and max is 3, we're done
	if job.Attempt >= job.MaxAttempts {
		log.Printf("Job %s reached max attempts (%d/%d), marking as failed", job.ID, job.Attempt, job.MaxAttempts)

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
		return
	}

	// Job will be retried - increment attempt counter
	if err := w.store.Jobs.IncrementAttempt(ctx, job.ID); err != nil {
		log.Printf("Failed to increment attempt for job %s: %v", job.ID, err)
		return
	}

	// Record retry attempt with error details for debugging
	errorCode := "RETRY"
	if err := w.store.JobRetryHistory.RecordRetry(ctx, job.ID, job.Attempt+1, errorCode, jobErr.Error()); err != nil {
		log.Printf("Warning: Failed to record retry history for job %s: %v", job.ID, err)
		// Don't fail - this is just for tracking
	}

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

// cleanupPartialDeployment cleans up partial infrastructure from a failed CREATE job
func (w *Worker) cleanupPartialDeployment(ctx context.Context, job *types.Job) {
	// Get cluster metadata
	cluster, err := w.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		log.Printf("Warning: failed to get cluster for cleanup: %v", err)
		return
	}

	log.Printf("Cleaning up partial deployment for job %s (cluster %s, type %s)", job.ID, job.ClusterID, cluster.ClusterType)

	// Clean up DNS records for OpenShift clusters (have base domain)
	if cluster.BaseDomain != nil && *cluster.BaseDomain != "" {
		log.Printf("Cleaning up DNS records for cluster %s.%s", cluster.Name, *cluster.BaseDomain)
		dnsCleaner := NewDNSCleaner(cluster.Region)
		if err := dnsCleaner.CleanupClusterDNS(ctx, cluster.Name, *cluster.BaseDomain); err != nil {
			log.Printf("Warning: DNS cleanup failed: %v", err)
		} else {
			log.Printf("Successfully cleaned up DNS records")
		}
	}

	workDir := fmt.Sprintf("%s/%s", w.config.WorkDir, job.ClusterID)

	// Route to appropriate cleanup based on cluster type
	switch cluster.ClusterType {
	case types.ClusterTypeOpenShift:
		w.cleanupOpenShiftDeployment(ctx, job, cluster, workDir)
	case types.ClusterTypeEKS:
		w.cleanupEKSDeployment(ctx, job, cluster, workDir)
	case types.ClusterTypeIKS:
		w.cleanupIKSDeployment(ctx, job, cluster, workDir)
	default:
		log.Printf("Unknown cluster type %s, skipping cleanup", cluster.ClusterType)
	}

	// Remove work directory to ensure clean slate for retry
	if err := os.RemoveAll(workDir); err != nil {
		log.Printf("Warning: failed to remove work directory %s: %v", workDir, err)
	}
}

// cleanupOpenShiftDeployment cleans up partial OpenShift deployment
func (w *Worker) cleanupOpenShiftDeployment(ctx context.Context, job *types.Job, cluster *types.Cluster, workDir string) {
	// Check if work directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		log.Printf("Work directory %s does not exist, skipping openshift-install destroy", workDir)
		return
	}

	// Create version-specific installer
	inst, err := installer.NewInstallerForVersion(cluster.Version)
	if err != nil {
		log.Printf("Warning: failed to create installer for version %s: %v", cluster.Version, err)
		return
	}

	// Run openshift-install destroy to clean up partial infrastructure
	output, err := inst.DestroyCluster(ctx, workDir)
	if err != nil {
		log.Printf("Warning: openshift-install destroy failed for job %s: %v\nOutput: %s", job.ID, err, output)
	} else {
		log.Printf("Successfully cleaned up OpenShift deployment for job %s", job.ID)
	}
}

// cleanupEKSDeployment cleans up partial EKS deployment
func (w *Worker) cleanupEKSDeployment(ctx context.Context, job *types.Job, cluster *types.Cluster, workDir string) {
	log.Printf("Cleaning up partial EKS deployment for cluster %s in region %s", cluster.Name, cluster.Region)

	eksInstaller := installer.NewEKSInstaller()
	output, err := eksInstaller.DestroyCluster(ctx, cluster.Name, cluster.Region)
	if err != nil {
		log.Printf("Warning: eksctl delete failed for job %s: %v\nOutput: %s", job.ID, err, output)
	} else {
		log.Printf("Successfully cleaned up EKS deployment for job %s", job.ID)
	}
}

// cleanupIKSDeployment cleans up partial IKS deployment
func (w *Worker) cleanupIKSDeployment(ctx context.Context, job *types.Job, cluster *types.Cluster, workDir string) {
	log.Printf("Cleaning up partial IKS deployment for cluster %s", cluster.Name)

	iksInstaller := installer.NewIKSInstaller()

	// Login to IBM Cloud
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		log.Printf("Warning: IBMCLOUD_API_KEY not set, cannot cleanup IKS cluster")
		return
	}

	// Use empty resource group - Login will query for available resource groups
	if err := iksInstaller.Login(ctx, apiKey, cluster.Region, ""); err != nil {
		log.Printf("Warning: IBM Cloud login failed: %v", err)
		return
	}

	output, err := iksInstaller.DestroyCluster(ctx, cluster.Name)
	if err != nil {
		log.Printf("Warning: IKS cluster destroy failed for job %s: %v\nOutput: %s", job.ID, err, output)
	} else {
		log.Printf("Successfully cleaned up IKS deployment for job %s", job.ID)
	}
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

// validateInstallerBinaries checks that all required installer binaries are present
func (w *Worker) validateInstallerBinaries() error {
	// List of required binaries
	requiredBinaries := []struct {
		name     string
		path     string
		optional bool
	}{
		{"eksctl", "/usr/local/bin/eksctl", false},
		{"ibmcloud", "/usr/local/bin/ibmcloud", true}, // Optional - only needed for IKS
	}

	// Check each binary
	var missing []string
	for _, binary := range requiredBinaries {
		if _, err := os.Stat(binary.path); os.IsNotExist(err) {
			if !binary.optional {
				missing = append(missing, binary.name)
				log.Printf("✗ Required binary not found: %s (expected at %s)", binary.name, binary.path)
			} else {
				log.Printf("⚠ Optional binary not found: %s (expected at %s)", binary.name, binary.path)
			}
		} else {
			log.Printf("✓ Found %s at %s", binary.name, binary.path)
		}
	}

	// Check for versioned OpenShift installer binaries (openshift-install-4.*)
	openshiftBinaries, err := filepath.Glob("/usr/local/bin/openshift-install-*")
	if err != nil {
		return fmt.Errorf("failed to check for openshift-install binaries: %w", err)
	}
	if len(openshiftBinaries) == 0 {
		missing = append(missing, "openshift-install")
		log.Printf("✗ No versioned openshift-install binaries found (expected openshift-install-4.* in /usr/local/bin/)")
	} else {
		log.Printf("✓ Found %d versioned openshift-install binaries:", len(openshiftBinaries))
		for _, binary := range openshiftBinaries {
			log.Printf("  - %s", filepath.Base(binary))
		}
	}

	// Check for versioned ccoctl binaries (ccoctl-4.*)
	ccoctlBinaries, err := filepath.Glob("/usr/local/bin/ccoctl-*")
	if err != nil {
		return fmt.Errorf("failed to check for ccoctl binaries: %w", err)
	}
	if len(ccoctlBinaries) == 0 {
		missing = append(missing, "ccoctl")
		log.Printf("✗ No versioned ccoctl binaries found (expected ccoctl-4.* in /usr/local/bin/)")
	} else {
		log.Printf("✓ Found %d versioned ccoctl binaries:", len(ccoctlBinaries))
		for _, binary := range ccoctlBinaries {
			log.Printf("  - %s", filepath.Base(binary))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required installer binaries: %v", missing)
	}

	return nil
}
