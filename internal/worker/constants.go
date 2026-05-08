package worker

import "time"

// Timeout constants for various operations
const (
	// Base directory paths
	OcpctlBaseDir = "/opt/ocpctl" // Base directory for ocpctl installation

	// Worker lifecycle timeouts
	WorkerShutdownTimeout = 55 * time.Minute // Maximum time to wait for jobs to complete during shutdown (aligns with systemd TimeoutStopSec=3600)
	IMDSRequestTimeout    = 2 * time.Second  // Timeout for EC2 instance metadata requests

	// Job processing timeouts
	DestroyOperationTimeout      = 30 * time.Minute // Timeout for openshift-install destroy cluster (manifest cleanup handles AWS resources afterward)
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

	// Work hours enforcement
	WorkHoursGracePeriod = 2 * time.Hour // Grace period after cluster becomes READY before work hours hibernation can occur
)
