package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/api"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

func main() {
	// Load configuration from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://localhost:5432/ocpctl?sslmode=disable"
	}

	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "internal/profile/definitions"
	}

	port := 8080
	if portStr := os.Getenv("PORT"); portStr != "" {
		fmt.Sscanf(portStr, "%d", &port)
	}

	// JWT configuration
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Println("WARNING: Using default JWT_SECRET. Set JWT_SECRET environment variable in production!")
		jwtSecret = "change-me-in-production-min-32-chars"
	}

	// CORS configuration
	corsOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if corsOrigins == "" {
		corsOrigins = "http://localhost:3000"
	}

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

	log.Printf("Server configured:")
	log.Printf("  Port: %d", config.Port)
	log.Printf("  Auth enabled: %v", config.EnableAuth)
	log.Printf("  CORS origins: %v", config.AllowedOrigins)

	// Create API server
	server := api.NewServer(config, st, registry, policyEngine)

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
