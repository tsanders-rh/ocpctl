package api

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

// MetricsHandler handles metrics API requests
type MetricsHandler struct {
	store *store.Store
}

// NewMetricsHandler creates a new metrics handler
func NewMetricsHandler(store *store.Store) *MetricsHandler {
	return &MetricsHandler{store: store}
}

// MetricsSnapshot represents current system metrics in JSON format
type MetricsSnapshot struct {
	API       APIMetricsSnapshot       `json:"api"`
	Workers   WorkerMetricsSnapshot    `json:"workers"`
	Jobs      JobMetricsSnapshot       `json:"jobs"`
	Clusters  ClusterMetricsSnapshot   `json:"clusters"`
	Autoscale AutoscaleMetricsSnapshot `json:"autoscale"`
	Database  DatabaseMetricsSnapshot  `json:"database"`
}

type APIMetricsSnapshot struct {
	RequestsPerSecond float64         `json:"requests_per_second"`
	ActiveConnections int64           `json:"active_connections"`
	ErrorRate         float64         `json:"error_rate"`
	RequestsByStatus  map[string]int  `json:"requests_by_status"`
}

type WorkerMetricsSnapshot struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Idle   int `json:"idle"`
}

type JobMetricsSnapshot struct {
	QueuedByType    map[string]int `json:"queued_by_type"`
	ProcessingTotal int            `json:"processing_total"`
	TotalQueued     int            `json:"total_queued"`
}

type ClusterMetricsSnapshot struct {
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"by_status"`
	ByProfile map[string]int `json:"by_profile"`
}

type AutoscaleMetricsSnapshot struct {
	CurrentWorkers int `json:"current_workers"`
	DesiredWorkers int `json:"desired_workers"`
}

type DatabaseMetricsSnapshot struct {
	OpenConnections int `json:"open_connections"`
	MaxConnections  int `json:"max_connections"`
}

// GetCurrentMetrics returns current system metrics
// @Summary Get current system metrics
// @Description Returns current system metrics including API stats, job queue, clusters, and workers
// @Tags metrics
// @Produce json
// @Success 200 {object} MetricsSnapshot
// @Failure 500 {object} ErrorResponse
// @Router /metrics/current [get]
// @Security BearerAuth
func (h *MetricsHandler) GetCurrentMetrics(c echo.Context) error {
	ctx := c.Request().Context()

	snapshot := MetricsSnapshot{
		API:       h.getAPIMetrics(),
		Workers:   h.getWorkerMetrics(ctx),
		Jobs:      h.getJobMetrics(ctx),
		Clusters:  h.getClusterMetrics(ctx),
		Autoscale: h.getAutoscaleMetrics(ctx),
		Database:  h.getDatabaseMetrics(),
	}

	return c.JSON(http.StatusOK, snapshot)
}

func (h *MetricsHandler) getAPIMetrics() APIMetricsSnapshot {
	return APIMetricsSnapshot{
		RequestsPerSecond: 0, // Calculated on frontend from historical data
		ActiveConnections: 0, // Prometheus gauges don't expose current value easily
		ErrorRate:         0,
		RequestsByStatus:  make(map[string]int),
	}
}

func (h *MetricsHandler) getWorkerMetrics(ctx context.Context) WorkerMetricsSnapshot {
	// Count running jobs to estimate active workers
	var runningCount int
	h.store.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM jobs WHERE status = 'RUNNING'
	`).Scan(&runningCount)

	active := runningCount
	if active > 0 {
		active = 1 // At least one worker is active
	}

	return WorkerMetricsSnapshot{
		Total:  1, // Static worker + autoscale
		Active: active,
		Idle:   1 - active,
	}
}

func (h *MetricsHandler) getJobMetrics(ctx context.Context) JobMetricsSnapshot {
	queuedByType := make(map[string]int)
	totalQueued := 0
	processingTotal := 0

	// Query pending jobs
	query := `
		SELECT job_type, COUNT(*)
		FROM jobs
		WHERE status = 'PENDING'
		GROUP BY job_type
	`
	rows, err := h.store.Pool().Query(ctx, query)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var jobType string
			var count int
			if err := rows.Scan(&jobType, &count); err == nil {
				queuedByType[jobType] = count
				totalQueued += count
			}
		}
	}

	// Count processing jobs
	h.store.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM jobs WHERE status = 'RUNNING'
	`).Scan(&processingTotal)

	return JobMetricsSnapshot{
		QueuedByType:    queuedByType,
		ProcessingTotal: processingTotal,
		TotalQueued:     totalQueued,
	}
}

func (h *MetricsHandler) getClusterMetrics(ctx context.Context) ClusterMetricsSnapshot {
	byStatus := make(map[string]int)
	byProfile := make(map[string]int)
	total := 0

	// Get clusters by status
	query := `
		SELECT status, COUNT(*)
		FROM clusters
		GROUP BY status
	`
	rows, err := h.store.Pool().Query(ctx, query)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int
			if err := rows.Scan(&status, &count); err == nil {
				byStatus[status] = count
				total += count
			}
		}
	}

	// Get clusters by profile
	query = `
		SELECT profile, COUNT(*)
		FROM clusters
		WHERE status != 'DESTROYED'
		GROUP BY profile
	`
	rows, err = h.store.Pool().Query(ctx, query)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var profile string
			var count int
			if err := rows.Scan(&profile, &count); err == nil {
				byProfile[profile] = count
			}
		}
	}

	return ClusterMetricsSnapshot{
		Total:     total,
		ByStatus:  byStatus,
		ByProfile: byProfile,
	}
}

func (h *MetricsHandler) getAutoscaleMetrics(ctx context.Context) AutoscaleMetricsSnapshot {
	// Simple logic: desired = ceil(queue_depth / 3)
	var queueDepth int
	h.store.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM jobs WHERE status = 'PENDING'
	`).Scan(&queueDepth)

	desired := 1 // Always at least 1
	if queueDepth > 3 {
		desired = (queueDepth + 2) / 3 // Ceiling division
	}

	return AutoscaleMetricsSnapshot{
		CurrentWorkers: 1, // Would query AWS autoscale group in production
		DesiredWorkers: desired,
	}
}

func (h *MetricsHandler) getDatabaseMetrics() DatabaseMetricsSnapshot {
	stats := h.store.Stats()

	return DatabaseMetricsSnapshot{
		OpenConnections: int(stats.TotalConns()),
		MaxConnections:  int(stats.MaxConns()),
	}
}
