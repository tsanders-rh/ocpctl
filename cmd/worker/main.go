package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/janitor"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/internal/worker"
)

// HealthCheckServer provides health and readiness endpoints
type HealthCheckServer struct {
	store  *store.Store
	worker *worker.Worker
	ready  bool
}

// healthHandler returns basic health status
func (h *HealthCheckServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// readyHandler checks if the worker is ready to process jobs
func (h *HealthCheckServer) readyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check database connectivity
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.store.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_ready",
			"reason": "database_unavailable",
		})
		return
	}

	// Check if worker is marked as ready
	if !h.ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_ready",
			"reason": "worker_initializing",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// startHealthCheckServer starts the health check HTTP server
func startHealthCheckServer(hcs *HealthCheckServer, port string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", hcs.healthHandler)
	mux.HandleFunc("/ready", hcs.readyHandler)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Health check server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Health check server error: %v", err)
		}
	}()

	return server
}

func main() {
	// Load configuration from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://localhost:5432/ocpctl?sslmode=disable"
	}

	workDir := os.Getenv("WORKER_WORK_DIR")
	if workDir == "" {
		workDir = "/tmp/ocpctl"
	}

	healthCheckPort := os.Getenv("WORKER_HEALTH_PORT")
	if healthCheckPort == "" {
		healthCheckPort = "8081"
	}

	// Validate OPENSHIFT_PULL_SECRET (prefer file, fall back to env var)
	pullSecret := os.Getenv("OPENSHIFT_PULL_SECRET")
	pullSecretFile := os.Getenv("OPENSHIFT_PULL_SECRET_FILE")
	environment := os.Getenv("ENVIRONMENT")

	// If a file path is specified, read from file
	if pullSecretFile != "" {
		fileData, err := os.ReadFile(pullSecretFile)
		if err != nil {
			if environment == "production" {
				log.Fatalf("CRITICAL: Failed to read OPENSHIFT_PULL_SECRET_FILE (%s): %v", pullSecretFile, err)
			}
			log.Printf("WARNING: Failed to read OPENSHIFT_PULL_SECRET_FILE (%s): %v", pullSecretFile, err)
			pullSecret = "" // Clear any env var value
		} else {
			pullSecret = string(fileData)
			log.Printf("Loaded pull secret from file: %s", pullSecretFile)
		}
	}

	if pullSecret == "" {
		if environment == "production" {
			log.Fatalf("CRITICAL: OPENSHIFT_PULL_SECRET must be set in production environment")
		}
		log.Println("WARNING: OPENSHIFT_PULL_SECRET not set. Cluster provisioning will fail until configured.")
		log.Println("See docs/OPENSHIFT_INSTALL_SETUP.md for instructions on obtaining a pull secret.")
	} else {
		// Validate that pull secret is valid JSON
		var js map[string]interface{}
		if err := json.Unmarshal([]byte(pullSecret), &js); err != nil {
			if environment == "production" {
				log.Fatalf("CRITICAL: OPENSHIFT_PULL_SECRET is not valid JSON: %v", err)
			}
			log.Printf("WARNING: OPENSHIFT_PULL_SECRET is not valid JSON: %v", err)
		}

		// Validate that pull secret has expected structure (should have "auths" key)
		if _, ok := js["auths"]; !ok {
			if environment == "production" {
				log.Fatalf("CRITICAL: OPENSHIFT_PULL_SECRET missing required 'auths' key")
			}
			log.Println("WARNING: OPENSHIFT_PULL_SECRET missing required 'auths' key")
		}
	}

	// Initialize store
	log.Println("Connecting to database...")
	st, err := store.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer st.Close()

	// Test database connection
	ctx := context.Background()
	if err := st.Ping(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Println("Database connection successful")

	// Create worker
	workerConfig := worker.DefaultConfig()
	workerConfig.WorkDir = workDir

	w := worker.NewWorker(workerConfig, st)

	// Create janitor
	janitorConfig := janitor.DefaultConfig()
	j := janitor.NewJanitor(janitorConfig, st, workDir)

	// Start health check server
	healthCheck := &HealthCheckServer{
		store:  st,
		worker: w,
		ready:  false,
	}
	healthServer := startHealthCheckServer(healthCheck, healthCheckPort)

	// Start worker and janitor in separate goroutines
	workerCtx, workerCancel := context.WithCancel(context.Background())
	janitorCtx, janitorCancel := context.WithCancel(context.Background())

	go func() {
		if err := w.Start(workerCtx); err != nil && err != context.Canceled {
			log.Printf("Worker error: %v", err)
		}
	}()

	go func() {
		if err := j.Start(janitorCtx); err != nil && err != context.Canceled {
			log.Printf("Janitor error: %v", err)
		}
	}()

	log.Println("Worker and janitor started successfully")

	// Mark health check as ready
	healthCheck.ready = true

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down worker and janitor...")

	// Mark as not ready during shutdown
	healthCheck.ready = false

	// Stop worker and janitor
	workerCancel()
	janitorCancel()

	// Shutdown health check server gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Health check server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}
