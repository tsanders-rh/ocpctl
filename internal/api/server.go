package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	apimiddleware "github.com/tsanders-rh/ocpctl/internal/api/middleware"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

// ServerConfig holds configuration for the API server
type ServerConfig struct {
	Port              int
	ShutdownTimeout   time.Duration
	EnableCORS        bool
	EnableAuth        bool
	JWTSecret         string
	AllowedOrigins    []string
	MaxBodySize       string
	RateLimitRequests int
	RateLimitDuration time.Duration
}

// DefaultServerConfig returns default server configuration
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Port:              8080,
		ShutdownTimeout:   10 * time.Second,
		EnableCORS:        true,
		EnableAuth:        false, // Disabled for Phase 1
		AllowedOrigins:    []string{"*"},
		MaxBodySize:       "1M",
		RateLimitRequests: 100,
		RateLimitDuration: 1 * time.Minute,
	}
}

// Server represents the HTTP API server
type Server struct {
	echo     *echo.Echo
	config   *ServerConfig
	store    *store.Store
	registry *profile.Registry
	policy   *policy.Engine
}

// NewServer creates a new API server
func NewServer(
	config *ServerConfig,
	store *store.Store,
	registry *profile.Registry,
	policyEngine *policy.Engine,
) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Disable Echo's default logger, we'll use our own
	e.Logger.SetOutput(nil)

	// Set custom validator
	e.Validator = NewValidator()

	s := &Server{
		echo:     e,
		config:   config,
		store:    store,
		registry: registry,
		policy:   policyEngine,
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// setupMiddleware configures middleware stack
func (s *Server) setupMiddleware() {
	// Recover from panics
	s.echo.Use(middleware.Recover())

	// Request ID for tracing
	s.echo.Use(middleware.RequestID())

	// Logging middleware
	s.echo.Use(apimiddleware.Logger())

	// CORS if enabled
	if s.config.EnableCORS {
		s.echo.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: s.config.AllowedOrigins,
			AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch},
			AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
		}))
	}

	// Body limit
	s.echo.Use(middleware.BodyLimit(s.config.MaxBodySize))

	// Timeout middleware
	s.echo.Use(middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: 30 * time.Second,
	}))
}

// setupRoutes configures API routes
func (s *Server) setupRoutes() {
	// Health check
	s.echo.GET("/health", s.healthCheck)
	s.echo.GET("/ready", s.readyCheck)

	// API v1 routes
	v1 := s.echo.Group("/api/v1")

	// Cluster handlers
	clusterHandler := NewClusterHandler(s.store, s.policy)
	v1.POST("/clusters", clusterHandler.Create)
	v1.GET("/clusters", clusterHandler.List)
	v1.GET("/clusters/:id", clusterHandler.Get)
	v1.DELETE("/clusters/:id", clusterHandler.Delete)
	v1.PATCH("/clusters/:id/extend", clusterHandler.Extend)

	// Profile handlers
	profileHandler := NewProfileHandler(s.registry)
	v1.GET("/profiles", profileHandler.List)
	v1.GET("/profiles/:name", profileHandler.Get)

	// Job handlers
	jobHandler := NewJobHandler(s.store)
	v1.GET("/jobs", jobHandler.List)
	v1.GET("/jobs/:id", jobHandler.Get)
}

// healthCheck returns basic health status
func (s *Server) healthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// readyCheck checks if server is ready to handle requests
func (s *Server) readyCheck(c echo.Context) error {
	// Check database connection
	ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"error":  "database unavailable",
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "ready",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.config.Port)
	fmt.Printf("Starting API server on %s\n", addr)
	return s.echo.Start(addr)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

// Echo returns the underlying Echo instance for testing
func (s *Server) Echo() *echo.Echo {
	return s.echo
}
