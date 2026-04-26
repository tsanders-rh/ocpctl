package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/time/rate"
)

// rateLimiterEntry holds a rate limiter and its last access time
type rateLimiterEntry struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// RateLimiterStore holds rate limiters per IP with last access tracking
type RateLimiterStore struct {
	limiters map[string]*rateLimiterEntry
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	cleanup  time.Duration
	maxAge   time.Duration // How long to keep inactive limiters
}

// NewRateLimiterStore creates a new rate limiter store
func NewRateLimiterStore(requestsPerMinute int, burst int) *RateLimiterStore {
	store := &RateLimiterStore{
		limiters: make(map[string]*rateLimiterEntry),
		rate:     rate.Limit(float64(requestsPerMinute) / 60.0), // Convert to per-second
		burst:    burst,
		cleanup:  5 * time.Minute,  // Run cleanup every 5 minutes
		maxAge:   30 * time.Minute, // Remove limiters inactive for 30 minutes
	}

	// Cleanup old limiters periodically
	go store.cleanupRoutine()

	return store
}

// getLimiter returns a rate limiter for the given IP and updates last access time
func (s *RateLimiterStore) getLimiter(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.limiters[ip]
	if !exists {
		// Create new limiter entry
		entry = &rateLimiterEntry{
			limiter:    rate.NewLimiter(s.rate, s.burst),
			lastAccess: time.Now(),
		}
		s.limiters[ip] = entry
	} else {
		// Update last access time
		entry.lastAccess = time.Now()
	}

	return entry.limiter
}

// cleanupRoutine periodically removes inactive limiters
// Only removes limiters that haven't been accessed in maxAge duration
func (s *RateLimiterStore) cleanupRoutine() {
	ticker := time.NewTicker(s.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		removed := 0

		// Remove entries not accessed within maxAge
		for ip, entry := range s.limiters {
			if now.Sub(entry.lastAccess) > s.maxAge {
				delete(s.limiters, ip)
				removed++
			}
		}

		s.mu.Unlock()

		// Log cleanup stats if any limiters were removed
		if removed > 0 {
			// Don't use log.Printf here to avoid spamming logs
			// Could add metrics instead
		}
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
