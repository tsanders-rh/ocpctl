package metrics

import (
	"context"
	"log"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Collector periodically collects metrics from database
type Collector struct {
	store          *store.Store
	updateInterval time.Duration
	stopChan       chan struct{}
}

// NewCollector creates a new metrics collector
func NewCollector(s *store.Store, interval time.Duration) *Collector {
	return &Collector{
		store:          s,
		updateInterval: interval,
		stopChan:       make(chan struct{}),
	}
}

// Start begins collecting metrics in the background
func (c *Collector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.updateInterval)
	defer ticker.Stop()

	// Collect initial metrics immediately
	c.collectMetrics(ctx)

	for {
		select {
		case <-ticker.C:
			c.collectMetrics(ctx)
		case <-c.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop stops the metrics collector
func (c *Collector) Stop() {
	close(c.stopChan)
}

// collectMetrics gathers all metrics from database
func (c *Collector) collectMetrics(ctx context.Context) {
	c.collectClusterMetrics(ctx)
	c.collectJobMetrics(ctx)
	c.collectDatabaseMetrics(ctx)
}

// collectClusterMetrics gathers cluster-related metrics
func (c *Collector) collectClusterMetrics(ctx context.Context) {
	// Query clusters grouped by status
	query := `
		SELECT status, COUNT(*)
		FROM clusters
		GROUP BY status
	`

	rows, err := c.store.Pool().Query(ctx, query)
	if err != nil {
		log.Printf("Error collecting cluster metrics: %v", err)
		return
	}
	defer rows.Close()

	// Reset all status gauges first
	for _, status := range []types.ClusterStatus{
		types.ClusterStatusPending,
		types.ClusterStatusCreating,
		types.ClusterStatusReady,
		types.ClusterStatusFailed,
		types.ClusterStatusDestroying,
		types.ClusterStatusDestroyed,
		types.ClusterStatusHibernated,
	} {
		ClustersTotal.WithLabelValues(string(status)).Set(0)
	}

	// Update with actual counts
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			log.Printf("Error scanning cluster status: %v", err)
			continue
		}
		ClustersTotal.WithLabelValues(status).Set(float64(count))
	}

	// Query clusters by profile
	query = `
		SELECT profile, COUNT(*)
		FROM clusters
		WHERE status != $1
		GROUP BY profile
	`

	rows, err = c.store.Pool().Query(ctx, query, types.ClusterStatusDestroyed)
	if err != nil {
		log.Printf("Error collecting cluster by profile metrics: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var profile string
		var count int
		if err := rows.Scan(&profile, &count); err != nil {
			log.Printf("Error scanning cluster profile: %v", err)
			continue
		}
		ClustersByProfile.WithLabelValues(profile).Set(float64(count))
	}

	// Query clusters by region
	query = `
		SELECT platform, region, COUNT(*)
		FROM clusters
		WHERE status != $1
		GROUP BY platform, region
	`

	rows, err = c.store.Pool().Query(ctx, query, types.ClusterStatusDestroyed)
	if err != nil {
		log.Printf("Error collecting cluster by region metrics: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var platform, region string
		var count int
		if err := rows.Scan(&platform, &region, &count); err != nil {
			log.Printf("Error scanning cluster region: %v", err)
			continue
		}
		ClustersByRegion.WithLabelValues(platform, region).Set(float64(count))
	}
}

// collectJobMetrics gathers job queue metrics
func (c *Collector) collectJobMetrics(ctx context.Context) {
	// Query jobs in queue by type
	query := `
		SELECT job_type, COUNT(*)
		FROM jobs
		WHERE status = $1
		GROUP BY job_type
	`

	rows, err := c.store.Pool().Query(ctx, query, types.JobStatusPending)
	if err != nil {
		log.Printf("Error collecting job queue metrics: %v", err)
		return
	}
	defer rows.Close()

	// Reset all job type gauges first
	for _, jobType := range []types.JobType{
		types.JobTypeCreate,
		types.JobTypeDestroy,
		types.JobTypeHibernate,
		types.JobTypeResume,
		types.JobTypePostConfigure,
		types.JobTypeJanitorDestroy,
	} {
		JobsQueuedTotal.WithLabelValues(string(jobType)).Set(0)
	}

	// Update with actual counts
	for rows.Next() {
		var jobType string
		var count int
		if err := rows.Scan(&jobType, &count); err != nil {
			log.Printf("Error scanning job type: %v", err)
			continue
		}
		JobsQueuedTotal.WithLabelValues(jobType).Set(float64(count))
	}

	// Query jobs currently processing by type
	query = `
		SELECT job_type, COUNT(*)
		FROM jobs
		WHERE status = $1
		GROUP BY job_type
	`

	rows, err = c.store.Pool().Query(ctx, query, types.JobStatusRunning)
	if err != nil {
		log.Printf("Error collecting processing job metrics: %v", err)
		return
	}
	defer rows.Close()

	// Reset all processing gauges
	for _, jobType := range []types.JobType{
		types.JobTypeCreate,
		types.JobTypeDestroy,
		types.JobTypeHibernate,
		types.JobTypeResume,
		types.JobTypePostConfigure,
		types.JobTypeJanitorDestroy,
	} {
		JobsProcessingTotal.WithLabelValues(string(jobType)).Set(0)
	}

	// Update with actual counts
	for rows.Next() {
		var jobType string
		var count int
		if err := rows.Scan(&jobType, &count); err != nil {
			log.Printf("Error scanning processing job type: %v", err)
			continue
		}
		JobsProcessingTotal.WithLabelValues(jobType).Set(float64(count))
	}
}

// collectDatabaseMetrics gathers database connection pool metrics
func (c *Collector) collectDatabaseMetrics(ctx context.Context) {
	stats := c.store.Pool().Stat()
	DatabaseConnectionsOpen.Set(float64(stats.TotalConns()))
}
