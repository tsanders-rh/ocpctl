package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

func main() {
	// Get database URL from environment
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable required")
	}

	// Get profiles directory from environment or use default
	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "/opt/ocpctl/profiles"
	}

	log.Printf("Migrating profiles from %s to database", profilesDir)

	// Connect to database
	st, err := store.NewStore(databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer st.Close()

	// Load profiles from YAML files
	loader := profile.NewLoader(profilesDir)
	registry, err := profile.NewRegistry(loader)
	if err != nil {
		log.Fatalf("Failed to load profiles from directory: %v", err)
	}

	profiles := registry.List()
	log.Printf("Found %d profiles to migrate", len(profiles))

	// Insert each profile into database
	ctx := context.Background()
	successCount := 0
	failCount := 0

	for _, p := range profiles {
		log.Printf("Migrating profile: %s", p.Name)
		if err := st.UpsertProfile(ctx, p); err != nil {
			log.Printf("❌ Failed to migrate profile %s: %v", p.Name, err)
			failCount++
			continue
		}
		log.Printf("✅ Migrated: %s", p.Name)
		successCount++
	}

	fmt.Println()
	log.Printf("Migration complete: %d succeeded, %d failed", successCount, failCount)

	if failCount > 0 {
		os.Exit(1)
	}
}
