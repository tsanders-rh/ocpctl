package middleware

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/metrics"
)

// PrometheusMetrics returns middleware that tracks HTTP request metrics
func PrometheusMetrics() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip metrics endpoint itself to avoid recursion
			if c.Path() == "/metrics" {
				return next(c)
			}

			start := time.Now()

			// Track in-flight requests
			metrics.HTTPRequestsInFlight.Inc()
			defer metrics.HTTPRequestsInFlight.Dec()

			// Process request
			err := next(c)

			// Calculate duration
			duration := time.Since(start).Seconds()

			// Get response status
			status := c.Response().Status
			if status == 0 {
				status = 200
			}

			// Extract method and path
			method := c.Request().Method
			path := c.Path()

			// Record metrics
			metrics.HTTPRequestsTotal.WithLabelValues(
				method,
				path,
				strconv.Itoa(status),
			).Inc()

			metrics.HTTPRequestDuration.WithLabelValues(
				method,
				path,
			).Observe(duration)

			return err
		}
	}
}
