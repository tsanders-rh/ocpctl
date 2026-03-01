package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	apimiddleware "github.com/tsanders-rh/ocpctl/internal/api/middleware"
	"github.com/tsanders-rh/ocpctl/internal/auth"
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
	EnableIAMAuth     bool
	JWTSecret         string
	JWTAccessTTL      time.Duration
	JWTRefreshTTL     time.Duration
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
		EnableAuth:        true,  // Enabled for Phase 2
		EnableIAMAuth:     false, // Disabled by default, enable with ENABLE_IAM_AUTH=true
		JWTSecret:         "change-me-in-production-min-32-chars",
		JWTAccessTTL:      15 * time.Minute,
		JWTRefreshTTL:     7 * 24 * time.Hour, // 7 days
		AllowedOrigins:    []string{"http://localhost:3000"}, // Next.js dev server
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
	auth     *auth.Auth
	iamAuth  *auth.IAMAuthenticator
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
	e.Logger.SetOutput(io.Discard)

	// Set custom validator
	e.Validator = NewValidator()

	// Create auth service
	authService := auth.NewAuth(
		config.JWTSecret,
		config.JWTAccessTTL,
		config.JWTRefreshTTL,
	)

	// Create IAM auth service
	iamAuthService, err := auth.NewIAMAuthenticator(
		store.IAMMappings,
		store.Users,
		config.EnableIAMAuth,
	)
	if err != nil {
		// Log warning but continue (IAM auth will be disabled)
		e.Logger.Warn("Failed to initialize IAM authenticator: ", err)
	}

	s := &Server{
		echo:     e,
		config:   config,
		store:    store,
		registry: registry,
		policy:   policyEngine,
		auth:     authService,
		iamAuth:  iamAuthService,
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
			AllowOrigins:     s.config.AllowedOrigins,
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch},
			AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
			AllowCredentials: true, // Required for cookies
			ExposeHeaders:    []string{echo.HeaderContentLength},
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
	// Health check (no auth required)
	s.echo.GET("/health", s.healthCheck)
	s.echo.GET("/ready", s.readyCheck)

	// API v1 routes
	v1 := s.echo.Group("/api/v1")

	// Auth routes (public)
	authHandler := NewAuthHandler(s.store, s.auth)
	authGroup := v1.Group("/auth")
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/logout", authHandler.Logout)
	authGroup.POST("/refresh", authHandler.Refresh)

	// Protected auth routes (require authentication)
	authProtected := authGroup.Group("", auth.RequireAuthDual(s.auth, s.iamAuth))
	authProtected.GET("/me", authHandler.GetMe)
	authProtected.PATCH("/me", authHandler.UpdateMe)
	authProtected.POST("/password", authHandler.ChangePassword)

	// User management routes (admin only)
	userHandler := NewUserHandler(s.store)
	usersGroup := v1.Group("/users", auth.RequireAuthDual(s.auth, s.iamAuth), auth.RequireAdmin())
	usersGroup.GET("", userHandler.List)
	usersGroup.POST("", userHandler.Create)
	usersGroup.GET("/:id", userHandler.Get)
	usersGroup.PATCH("/:id", userHandler.Update)
	usersGroup.DELETE("/:id", userHandler.Delete)

	// Cluster routes (all require authentication)
	clusterHandler := NewClusterHandler(s.store, s.policy)
	clustersGroup := v1.Group("/clusters", auth.RequireAuthDual(s.auth, s.iamAuth))
	clustersGroup.POST("", clusterHandler.Create)
	clustersGroup.GET("", clusterHandler.List)
	clustersGroup.GET("/:id", clusterHandler.Get)
	clustersGroup.DELETE("/:id", clusterHandler.Delete)
	clustersGroup.PATCH("/:id/extend", clusterHandler.Extend)
	clustersGroup.GET("/:id/outputs", clusterHandler.GetOutputs)
	clustersGroup.GET("/:id/kubeconfig", clusterHandler.DownloadKubeconfig)

	// Profile routes (public for now, authenticated users only in production)
	profileHandler := NewProfileHandler(s.registry)
	profilesGroup := v1.Group("/profiles")
	profilesGroup.GET("", profileHandler.List)
	profilesGroup.GET("/:name", profileHandler.Get)

	// Job routes (require authentication)
	jobHandler := NewJobHandler(s.store)
	jobsGroup := v1.Group("/jobs", auth.RequireAuthDual(s.auth, s.iamAuth))
	jobsGroup.GET("", jobHandler.List)
	jobsGroup.GET("/:id", jobHandler.Get)
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
