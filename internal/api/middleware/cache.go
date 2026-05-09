package middleware

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
)

// CacheControl adds Cache-Control headers to responses
// ttl is the time-to-live in seconds (0 = no cache, -1 = no-store)
func CacheControl(ttl int) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Process the request first
			err := next(c)

			// Only add cache headers for successful responses
			if err == nil && c.Response().Status < 300 {
				if ttl < 0 {
					// Private, no-store for sensitive data
					c.Response().Header().Set("Cache-Control", "private, no-store, must-revalidate")
				} else if ttl == 0 {
					// No cache but allow storage
					c.Response().Header().Set("Cache-Control", "no-cache, private")
				} else {
					// Public cache with specific TTL
					c.Response().Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", ttl))
				}
			}

			return err
		}
	}
}

// CachePublic is a convenience wrapper for publicly cacheable responses
func CachePublic(duration time.Duration) echo.MiddlewareFunc {
	return CacheControl(int(duration.Seconds()))
}

// CachePrivate is a convenience wrapper for private cache-control
func CachePrivate(duration time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			if err == nil && c.Response().Status < 300 {
				c.Response().Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(duration.Seconds())))
			}
			return err
		}
	}
}

// NoCache disables caching
func NoCache() echo.MiddlewareFunc {
	return CacheControl(-1)
}
