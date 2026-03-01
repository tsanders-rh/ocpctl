package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/time/rate"
)

// RateLimiterStore holds rate limiters per IP
type RateLimiterStore struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	cleanup  time.Duration
}

// NewRateLimiterStore creates a new rate limiter store
func NewRateLimiterStore(requestsPerMinute int, burst int) *RateLimiterStore {
	store := &RateLimiterStore{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(float64(requestsPerMinute) / 60.0), // Convert to per-second
		burst:    burst,
		cleanup:  5 * time.Minute,
	}

	// Cleanup old limiters periodically
	go store.cleanupRoutine()

	return store
}

// getLimiter returns a rate limiter for the given IP
func (s *RateLimiterStore) getLimiter(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	limiter, exists := s.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(s.rate, s.burst)
		s.limiters[ip] = limiter
	}

	return limiter
}

// cleanupRoutine periodically removes inactive limiters
func (s *RateLimiterStore) cleanupRoutine() {
	ticker := time.NewTicker(s.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		// Simple cleanup: reset the map periodically
		// In production, you'd want to track last access time
		s.limiters = make(map[string]*rate.Limiter)
		s.mu.Unlock()
	}
}

// RateLimit returns a rate limiting middleware
func RateLimit(requestsPerMinute int, burst int) echo.MiddlewareFunc {
	store := NewRateLimiterStore(requestsPerMinute, burst)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get client IP
			ip := c.RealIP()
			if ip == "" {
				ip = c.Request().RemoteAddr
			}

			// Get rate limiter for this IP
			limiter := store.getLimiter(ip)

			// Check if request is allowed
			if !limiter.Allow() {
				return echo.NewHTTPError(
					http.StatusTooManyRequests,
					"rate limit exceeded, please try again later",
				)
			}

			return next(c)
		}
	}
}

// StrictRateLimit returns a stricter rate limit for sensitive endpoints
func StrictRateLimit(requestsPerMinute int) echo.MiddlewareFunc {
	return RateLimit(requestsPerMinute, 1) // Very low burst
}
