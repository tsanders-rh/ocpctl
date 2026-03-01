package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tsanders-rh/ocpctl/internal/janitor"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/internal/worker"
)

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

	// Check for pull secret (required for cluster provisioning, but not for worker startup)
	if os.Getenv("OPENSHIFT_PULL_SECRET") == "" {
		log.Println("WARNING: OPENSHIFT_PULL_SECRET not set. Cluster provisioning will fail until configured.")
		log.Println("See docs/OPENSHIFT_INSTALL_SETUP.md for instructions on obtaining a pull secret.")
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
	j := janitor.NewJanitor(janitorConfig, st)

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

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down worker and janitor...")

	// Stop worker and janitor
	workerCancel()
	janitorCancel()

	log.Println("Shutdown complete")
}
