package main

import (
	"context"
	"log"
	"os"

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable not set")
	}

	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "/opt/ocpctl/profiles"
	}

	ctx := context.Background()

	st, err := store.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer st.Close()

	loader := profile.NewLoader(profilesDir)
	profiles, err := loader.LoadAll()
	if err != nil {
		log.Fatalf("Failed to load profiles: %v", err)
	}

	log.Printf("Found %d profiles to sync", len(profiles))

	for _, p := range profiles {
		log.Printf("Syncing: %s (creds: %s)", p.Name, p.CredentialsMode)
		if err := st.UpsertProfile(ctx, p); err != nil {
			log.Printf("  ERROR: %v", err)
		} else {
			log.Printf("  ✓ Done")
		}
	}

	log.Println("Sync complete!")
}
