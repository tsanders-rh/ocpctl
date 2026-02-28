package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Config holds worker configuration
type Config struct {
	WorkerID       string
	PollInterval   time.Duration
	LockTimeout    time.Duration
	WorkDir        string
	MaxConcurrent  int
	RetryBackoff   time.Duration
	MaxRetries     int
}

// DefaultConfig returns default worker configuration
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	return &Config{
		WorkerID:      fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8]),
		PollInterval:  10 * time.Second,
		LockTimeout:   30 * time.Minute,
		WorkDir:       "/tmp/ocpctl",
		MaxConcurrent: 3,
		RetryBackoff:  30 * time.Second,
		MaxRetries:    3,
	}
}

// Worker processes background jobs
type Worker struct {
	config    *Config
	store     *store.Store
	processor *JobProcessor
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewWorker creates a new worker instance
func NewWorker(config *Config, st *store.Store) *Worker {
	if config == nil {
		config = DefaultConfig()
	}

	return &Worker{
		config:    config,
		store:     st,
		processor: NewJobProcessor(config, st),
		running:   false,
	}
}

// Start starts the worker loop
func (w *Worker) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true

	log.Printf("Worker %s starting (poll=%s, max_concurrent=%d)",
		w.config.WorkerID, w.config.PollInterval, w.config.MaxConcurrent)

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

	// Get pending jobs
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

	// Update job status to RUNNING
	if err := w.store.Jobs.MarkStarted(ctx, nil, job.ID); err != nil {
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
	// Increment attempt counter
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

		// Update job status back to PENDING for retry
		if err := w.store.Jobs.UpdateStatus(ctx, nil, job.ID, types.JobStatusPending); err != nil {
			log.Printf("Failed to reset job %s to pending: %v", job.ID, err)
		}

		// TODO: Implement exponential backoff delay
	}
}
