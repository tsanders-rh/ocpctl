// Package main OCPCTL API Server
//
//	@title						OCPCTL API
//	@version					1.0
//	@description				OCPCTL is a self-service OpenShift cluster provisioning and management platform. This API provides endpoints for creating, managing, and destroying OpenShift clusters with automated lifecycle management, policy enforcement, and resource tracking.
//	@termsOfService				http://github.com/tsanders-rh/ocpctl
//
//	@contact.name				OCPCTL Support
//	@contact.url				http://github.com/tsanders-rh/ocpctl/issues
//
//	@license.name				Apache 2.0
//	@license.url				http://www.apache.org/licenses/LICENSE-2.0.html
//
//	@host						localhost:8080
//	@BasePath					/api/v1
//	@schemes					http https
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				JWT access token. Format: "Bearer {token}". Obtain from /api/v1/auth/login endpoint.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/tsanders-rh/ocpctl/docs" // Import generated docs
	"github.com/tsanders-rh/ocpctl/internal/api"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/secrets"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

// Version information (set via -ldflags at build time)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Get environment for configuration validation
	environment := os.Getenv("ENVIRONMENT")

	// Load configuration from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		if environment == "production" {
			log.Fatalf("CRITICAL: DATABASE_URL must be set in production environment")
		}
		// Development fallback with warning
		log.Println("WARNING: DATABASE_URL not set, using localhost (development only)")
		log.Println("         Set DATABASE_URL environment variable before deploying to production!")
		dbURL = "postgres://localhost:5432/ocpctl?sslmode=disable"
	}

	// Validate SSL mode in production
	if environment == "production" {
		if strings.Contains(dbURL, "sslmode=disable") || !strings.Contains(dbURL, "sslmode=") {
			log.Fatalf("CRITICAL: Database connections must use SSL in production (sslmode=require or sslmode=verify-full)")
		}
	}

	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "internal/profile/definitions"
	}

	port := 8080
	if portStr := os.Getenv("PORT"); portStr != "" {
		fmt.Sscanf(portStr, "%d", &port)
	}

	// Initialize secrets manager
	log.Println("Initializing secrets manager...")
	ctx := context.Background()
	secretsManager, err := secrets.NewManager(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize secrets manager: %v", err)
	}

	// JWT configuration - retrieve from AWS Secrets Manager (or env var in development)
	jwtSecretName := os.Getenv("JWT_SECRET_NAME") // Name of secret in AWS Secrets Manager
	jwtSecret, err := secretsManager.GetSecretWithFallback(ctx, jwtSecretName, "JWT_SECRET", true)
	if err != nil {
		log.Fatalf("CRITICAL: Failed to retrieve JWT_SECRET: %v", err)
	}

	// Use default for development if still empty
	if jwtSecret == "" {
		if environment == "production" {
			log.Fatalf("CRITICAL: JWT_SECRET must be set in production environment")
		}
		log.Println("WARNING: Using default JWT_SECRET for development only")
		log.Println("         Set JWT_SECRET environment variable or JWT_SECRET_NAME for AWS Secrets Manager!")
		jwtSecret = "change-me-in-production-min-32-chars"
	}

	// Validate JWT secret length
	if len(jwtSecret) < 32 {
		if environment == "production" {
			log.Fatalf("CRITICAL: JWT_SECRET must be at least 32 characters (current: %d)", len(jwtSecret))
		}
		log.Printf("WARNING: JWT_SECRET should be at least 32 characters (current: %d)", len(jwtSecret))
	}

	// CORS configuration
	corsOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if corsOrigins == "" {
		corsOrigins = "http://localhost:3000"
	}

	// IAM authentication configuration
	enableIAMAuth := os.Getenv("ENABLE_IAM_AUTH") == "true"
	iamAllowedGroup := os.Getenv("IAM_ALLOWED_GROUP")

	// Initialize store
	log.Println("Connecting to database...")
	st, err := store.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer st.Close()

	// Run migrations
	log.Println("Running database migrations...")
	if err := st.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Verify migrations completed successfully
	log.Println("Verifying database schema...")
	if err := st.VerifyMigrations(ctx); err != nil {
		log.Fatalf("Migration verification failed: %v", err)
	}

	currentVersion, _ := st.GetSchemaVersion(ctx)
	log.Printf("Database schema version: %s", currentVersion)

	// Initialize profile registry
	log.Println("Loading cluster profiles...")
	loader := profile.NewLoader(profilesDir)
	registry, err := profile.NewRegistry(loader)
	if err != nil {
		log.Fatalf("Failed to load profiles: %v", err)
	}

	profileCount := registry.Count()
	enabledCount := registry.CountEnabled()
	log.Printf("Loaded %d profiles (%d enabled)", profileCount, enabledCount)

	// Initialize policy engine
	policyEngine := policy.NewEngine(registry)

	// Create server config
	config := api.DefaultServerConfig()
	config.Port = port
	config.JWTSecret = jwtSecret
	config.AllowedOrigins = []string{corsOrigins}
	config.EnableIAMAuth = enableIAMAuth
	config.IAMAllowedGroup = iamAllowedGroup
	config.Environment = environment

	log.Printf("Server configured:")
	log.Printf("  Port: %d", config.Port)
	log.Printf("  Auth enabled: %v (JWT: true, IAM: %v)", config.EnableAuth, config.EnableIAMAuth)
	log.Printf("  CORS origins: %v", config.AllowedOrigins)

	// Set version information
	config.Version = Version
	config.Commit = Commit
	config.BuildTime = BuildTime

	// Create API server
	server, err := api.NewServer(config, st, registry, policyEngine)
	if err != nil {
		log.Fatalf("Failed to create API server: %v", err)
	}

	// Start server in a goroutine
	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Gracefully shutdown the server with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
