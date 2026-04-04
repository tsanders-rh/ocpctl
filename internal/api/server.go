package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	echoSwagger "github.com/swaggo/echo-swagger"

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
	IAMAllowedGroup   string // Optional IAM group name for restricting authentication
	JWTSecret         string
	JWTAccessTTL      time.Duration
	JWTRefreshTTL     time.Duration
	AllowedOrigins    []string
	MaxBodySize       string
	RateLimitRequests int
	RateLimitDuration time.Duration
	Environment       string // Environment name (development, production, etc.)
	// Version information
	Version   string
	Commit    string
	BuildTime string
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
) (*Server, error) {
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
		config.IAMAllowedGroup,
	)
	if err != nil {
		// In production with IAM auth enabled, this is a critical failure
		if config.Environment == "production" && config.EnableIAMAuth {
			return nil, fmt.Errorf("CRITICAL: IAM authentication required in production but initialization failed: %w", err)
		}
		// In development, log warning and continue (IAM auth will be disabled)
		e.Logger.Warn("Failed to initialize IAM authenticator (IAM auth will be unavailable): ", err)
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

	return s, nil
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

	// Rate limiting (global, moderate limits)
	s.echo.Use(apimiddleware.RateLimit(s.config.RateLimitRequests, 10))
}

// setupRoutes configures API routes
func (s *Server) setupRoutes() {
	// Health check (no auth required)
	s.echo.GET("/health", s.healthCheck)
	s.echo.GET("/ready", s.readyCheck)
	s.echo.GET("/version", s.versionCheck)

	// Swagger API documentation (no auth required)
	s.echo.GET("/swagger/*", echoSwagger.EchoWrapHandler(echoSwagger.InstanceName("swagger")))

	// API v1 routes
	v1 := s.echo.Group("/api/v1")

	// Auth routes (public)
	authHandler := NewAuthHandler(s.store, s.auth)
	authGroup := v1.Group("/auth")

	// Strict rate limiting for login to prevent brute force
	authGroup.POST("/login", authHandler.Login, apimiddleware.StrictRateLimit(5)) // 5 requests/minute
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

	// Orphaned resources routes (admin only)
	orphanedHandler := NewOrphanedResourceHandler(s.store, s.policy)
	adminGroup := v1.Group("/admin", auth.RequireAuthDual(s.auth, s.iamAuth), auth.RequireAdmin())
	adminGroup.GET("/orphaned-resources", orphanedHandler.List)
	adminGroup.GET("/orphaned-resources/stats", orphanedHandler.GetStats)
	adminGroup.PATCH("/orphaned-resources/:id/resolve", orphanedHandler.MarkResolved)
	adminGroup.PATCH("/orphaned-resources/:id/ignore", orphanedHandler.MarkIgnored)
	adminGroup.DELETE("/orphaned-resources/:id", orphanedHandler.Delete)

	// Cluster routes (all require authentication)
	clusterHandler := NewClusterHandler(s.store, s.policy, s.registry)

	// Cluster statistics (admin only)
	adminGroup.GET("/clusters/statistics", clusterHandler.GetStatistics)
	clustersGroup := v1.Group("/clusters", auth.RequireAuthDual(s.auth, s.iamAuth))

	// Stricter rate limit for cluster creation (resource intensive)
	clustersGroup.POST("", clusterHandler.Create, apimiddleware.StrictRateLimit(10)) // 10 requests/minute
	clustersGroup.GET("", clusterHandler.List)
	clustersGroup.GET("/:id", clusterHandler.Get)
	clustersGroup.DELETE("/:id", clusterHandler.Delete)
	clustersGroup.PATCH("/:id/extend", clusterHandler.Extend)
	clustersGroup.POST("/:id/refresh-outputs", clusterHandler.RefreshOutputs)
	clustersGroup.POST("/:id/hibernate", clusterHandler.Hibernate)
	clustersGroup.POST("/:id/resume", clusterHandler.Resume)
	clustersGroup.GET("/:id/outputs", clusterHandler.GetOutputs)
	clustersGroup.GET("/:id/kubeconfig", clusterHandler.DownloadKubeconfig)
	clustersGroup.GET("/:id/kubeconfig/download-url", clusterHandler.GetKubeconfigDownloadURL)

	// Deployment logs routes (require authentication, checked within handler)
	logHandler := NewLogHandler(s.store)
	clustersGroup.GET("/:id/logs", logHandler.GetClusterLogs)

	// Storage routes (require authentication, checked within handler)
	storageHandler := NewStorageHandler(s.store, s.policy)
	clustersGroup.POST("/:id/storage/link", storageHandler.LinkToCluster)
	clustersGroup.GET("/:id/storage", storageHandler.GetStorage)
	clustersGroup.DELETE("/:id/storage/link/:group_id", storageHandler.UnlinkStorage)

	// Configuration routes (require authentication)
	configHandler := NewConfigurationHandler(s.store)
	clustersGroup.GET("/:id/configurations", configHandler.ListClusterConfigurations)
	clustersGroup.POST("/:id/configure", configHandler.TriggerPostConfiguration)
	clustersGroup.PATCH("/:id/configurations/:config_id/retry", configHandler.RetryConfiguration)

	// Profile routes (require authentication)
	profileHandler := NewProfileHandler(s.registry, s.store)
	profilesGroup := v1.Group("/profiles", auth.RequireAuthDual(s.auth, s.iamAuth))
	profilesGroup.GET("", profileHandler.List)
	profilesGroup.GET("/:name", profileHandler.Get)

	// Post-config add-ons routes (require authentication)
	addonsHandler := NewAddonsHandler(s.store)
	postConfigGroup := v1.Group("/post-config", auth.RequireAuthDual(s.auth, s.iamAuth))
	postConfigGroup.GET("/addons", addonsHandler.List)

	// Post-config validation and templates
	postConfigHandler := NewPostConfigHandler()
	postConfigGroup.POST("/validate", postConfigHandler.Validate)

	// Template routes (require authentication)
	templateHandler := NewTemplateHandler(s.store)
	templatesGroup := v1.Group("/templates", auth.RequireAuthDual(s.auth, s.iamAuth))
	templatesGroup.POST("", templateHandler.Create)
	templatesGroup.GET("", templateHandler.List)
	templatesGroup.GET("/:id", templateHandler.Get)
	templatesGroup.PATCH("/:id", templateHandler.Update)
	templatesGroup.DELETE("/:id", templateHandler.Delete)

	// Job routes (require authentication)
	jobHandler := NewJobHandler(s.store)
	jobsGroup := v1.Group("/jobs", auth.RequireAuthDual(s.auth, s.iamAuth))
	jobsGroup.GET("", jobHandler.List)
	jobsGroup.GET("/:id", jobHandler.Get)

	// System/Infrastructure routes (admin only)
	systemHandler := NewSystemHandler(s.store, s.config.Version)
	adminGroup.GET("/system/infrastructure", systemHandler.GetInfrastructure)
}

// healthCheck returns basic health status
//
//	@Summary		Health check
//	@Description	Returns basic health status of the API server including auth availability
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Router			/health [get]
func (s *Server) healthCheck(c echo.Context) error {
	health := map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
		"auth": map[string]interface{}{
			"jwt_enabled": s.config.EnableAuth,
			"iam_enabled": s.config.EnableIAMAuth,
			"iam_available": s.iamAuth != nil,
		},
	}
	return c.JSON(http.StatusOK, health)
}

// readyCheck checks if server is ready to handle requests
//
//	@Summary		Readiness check
//	@Description	Checks if the server is ready to handle requests by verifying database connectivity
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Failure		503	{object}	map[string]string
//	@Router			/ready [get]
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

// versionCheck returns version information
//
//	@Summary		Version information
//	@Description	Returns version, commit hash, and build time of the API server
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Router			/version [get]
func (s *Server) versionCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"version":   s.config.Version,
		"commit":    s.config.Commit,
		"buildTime": s.config.BuildTime,
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
