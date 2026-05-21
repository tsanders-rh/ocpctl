package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ============================================================================
// API Metrics
// ============================================================================

var (
	// HTTPRequestsTotal tracks all HTTP requests
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	// HTTPRequestDuration tracks request latency
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ocpctl_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
		},
		[]string{"method", "endpoint"},
	)

	// HTTPRequestsInFlight tracks concurrent requests
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ocpctl_http_requests_in_flight",
			Help: "Current number of HTTP requests being processed",
		},
	)

	// AuthRequestsTotal tracks authentication attempts
	AuthRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_auth_requests_total",
			Help: "Total authentication requests by method and result",
		},
		[]string{"method", "result"}, // method: api_key|iam, result: success|failure
	)

	// RateLimitHitsTotal tracks rate limit violations
	RateLimitHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_rate_limit_hits_total",
			Help: "Total number of rate limit hits",
		},
		[]string{"endpoint"},
	)
)

// ============================================================================
// Worker Metrics
// ============================================================================

var (
	// WorkersTotal tracks total number of workers
	WorkersTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_workers_total",
			Help: "Total number of workers by type",
		},
		[]string{"type"}, // static|autoscale
	)

	// WorkersActive tracks workers currently processing jobs
	WorkersActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ocpctl_workers_active",
			Help: "Number of workers currently processing jobs",
		},
	)

	// WorkerUptimeSeconds tracks worker uptime
	WorkerUptimeSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_worker_uptime_seconds",
			Help: "Worker uptime in seconds",
		},
		[]string{"worker_id"},
	)
)

// ============================================================================
// Job Metrics
// ============================================================================

var (
	// JobsQueuedTotal tracks jobs in queue by type
	JobsQueuedTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_jobs_queued_total",
			Help: "Number of jobs in queue by type",
		},
		[]string{"type"}, // CREATE, DESTROY, HIBERNATE, RESUME, POST_CONFIGURE
	)

	// JobsProcessingTotal tracks jobs currently being processed
	JobsProcessingTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_jobs_processing_total",
			Help: "Number of jobs currently being processed",
		},
		[]string{"type"},
	)

	// JobsCompletedTotal tracks completed jobs
	JobsCompletedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_jobs_completed_total",
			Help: "Total number of completed jobs",
		},
		[]string{"type", "status"}, // status: success|failed
	)

	// JobDurationSeconds tracks job processing time
	JobDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ocpctl_job_duration_seconds",
			Help:    "Job processing duration in seconds",
			Buckets: []float64{60, 300, 600, 900, 1800, 3600, 5400, 7200}, // 1min to 2hrs
		},
		[]string{"type"},
	)

	// JobWaitTimeSeconds tracks time jobs spend in queue
	JobWaitTimeSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ocpctl_job_wait_time_seconds",
			Help:    "Time jobs spend waiting in queue",
			Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800}, // 1sec to 30min
		},
		[]string{"type"},
	)

	// JobLocksAcquired tracks lock acquisition attempts
	JobLocksAcquired = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_job_locks_acquired_total",
			Help: "Total number of job lock acquisitions",
		},
		[]string{"result"}, // success|failure
	)
)

// ============================================================================
// Cluster Metrics
// ============================================================================

var (
	// ClustersTotal tracks total clusters by status
	ClustersTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_clusters_total",
			Help: "Total number of clusters by status",
		},
		[]string{"status"}, // PENDING, CREATING, READY, FAILED, DESTROYING, DESTROYED, HIBERNATED
	)

	// ClustersByProfile tracks clusters by profile
	ClustersByProfile = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_clusters_by_profile",
			Help: "Number of clusters by profile",
		},
		[]string{"profile"},
	)

	// ClustersByRegion tracks clusters by region
	ClustersByRegion = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_clusters_by_region",
			Help: "Number of clusters by region",
		},
		[]string{"platform", "region"},
	)

	// ClusterProvisionDurationSeconds tracks cluster provisioning time
	ClusterProvisionDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ocpctl_cluster_provision_duration_seconds",
			Help:    "Cluster provisioning duration in seconds",
			Buckets: []float64{600, 1200, 1800, 2400, 3000, 3600, 4200, 4800}, // 10min to 80min
		},
		[]string{"profile"},
	)

	// ClusterCreatedTotal tracks cluster creation attempts
	ClusterCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_cluster_created_total",
			Help: "Total number of cluster creation attempts",
		},
		[]string{"profile", "result"}, // result: success|failed
	)

	// ClusterCostHourly tracks estimated hourly cost
	ClusterCostHourly = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_cluster_cost_hourly_usd",
			Help: "Estimated hourly cost in USD",
		},
		[]string{"profile", "status"},
	)
)

// ============================================================================
// Autoscale Metrics
// ============================================================================

var (
	// AutoscaleDesiredWorkers tracks desired worker count
	AutoscaleDesiredWorkers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ocpctl_autoscale_desired_workers",
			Help: "Desired number of autoscale workers",
		},
	)

	// AutoscaleCurrentWorkers tracks current worker count
	AutoscaleCurrentWorkers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ocpctl_autoscale_current_workers",
			Help: "Current number of autoscale workers",
		},
	)

	// AutoscaleEventsTotal tracks scaling events
	AutoscaleEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_autoscale_events_total",
			Help: "Total number of autoscale events",
		},
		[]string{"direction"}, // up|down
	)
)

// ============================================================================
// Database Metrics
// ============================================================================

var (
	// DatabaseConnectionsOpen tracks open database connections
	DatabaseConnectionsOpen = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ocpctl_database_connections_open",
			Help: "Number of open database connections",
		},
	)

	// DatabaseQueriesTotal tracks database queries
	DatabaseQueriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ocpctl_database_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"operation"}, // select|insert|update|delete
	)

	// DatabaseQueryDuration tracks query latency
	DatabaseQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ocpctl_database_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)
)

// ============================================================================
// System Metrics
// ============================================================================

var (
	// BuildInfo provides version information
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_build_info",
			Help: "Build version information",
		},
		[]string{"version", "commit", "build_time"},
	)

	// UptimeSeconds tracks service uptime
	UptimeSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ocpctl_uptime_seconds",
			Help: "Service uptime in seconds",
		},
		[]string{"service"}, // api|worker
	)
)
