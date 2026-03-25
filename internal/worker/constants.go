package worker

import "time"

// Timeout constants for various operations
const (
	// Worker lifecycle timeouts
	WorkerShutdownTimeout = 30 * time.Second // Maximum time to wait for jobs to complete during shutdown
	IMDSRequestTimeout    = 2 * time.Second  // Timeout for EC2 instance metadata requests

	// Job processing timeouts
	DestroyOperationTimeout      = 45 * time.Minute // Timeout for cluster destroy operations
	DNSCleanupTimeout            = 5 * time.Minute  // Timeout for DNS record cleanup
	ClusterStatusCheckTimeout    = 10 * time.Minute // Timeout for waiting on cluster status changes
	AWSCommandTimeout            = 30 * time.Second // Timeout for individual AWS CLI commands
	LockReleaseRetryTimeout      = 10 * time.Second // Timeout for retry attempt on lock release
	LogFlushTimeout              = 5 * time.Second  // Timeout for flushing deployment logs
	DNSPropagationCheckInterval  = 10 * time.Second // Interval between DNS propagation checks
	PostConfigWaitTimeout        = 10 * time.Minute // Timeout for post-configuration operations
	PostConfigPollInterval       = 10 * time.Second // Interval between post-config status polls

	// Sleep/delay constants
	LogBatchFlushDelay      = 500 * time.Millisecond // Delay to allow final log batch to flush
	LogStreamPollInterval   = 100 * time.Millisecond // Interval for polling log stream
	LogStreamFlushInterval  = 500 * time.Millisecond // Interval for flushing log batches
	APIStabilizationDelay   = 2 * time.Second        // Delay to allow API server to stabilize
	NodeReadyCheckInterval  = 5 * time.Second        // Interval between node readiness checks
	CleanupRetryDelay       = 2 * time.Second        // Delay between cleanup retry attempts
	EKSCleanupWaitDelay     = 60 * time.Second       // Delay to allow EKS resources to clean up
)
