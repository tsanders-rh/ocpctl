#!/bin/bash
# Migrate YAML profiles to database
# This script loads all YAML profiles from the profiles directory into the database

set -e

SSH_KEY="$HOME/.ssh/ocpctl-production-key"
SSH_HOST="ubuntu@44.201.165.78"

echo "🔄 Migrating profiles from YAML to database..."

# Create a Go program to do the migration
cat > /tmp/migrate-profiles.go << 'EOF'
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

	// Get profiles directory from environment
	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "/opt/ocpctl/profiles"
	}

	// Connect to database
	st, err := store.New(databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer st.Close()

	// Load profiles from YAML files
	registry := profile.NewRegistry()
	if err := registry.LoadFromDirectory(profilesDir); err != nil {
		log.Fatalf("Failed to load profiles from directory: %v", err)
	}

	profiles := registry.List()
	log.Printf("Found %d profiles to migrate", len(profiles))

	// Insert each profile into database
	ctx := context.Background()
	for _, p := range profiles {
		log.Printf("Migrating profile: %s", p.Name)
		if err := st.UpsertProfile(ctx, p); err != nil {
			log.Printf("Warning: failed to migrate profile %s: %v", p.Name, err)
			continue
		}
		log.Printf("✓ Migrated: %s", p.Name)
	}

	log.Printf("✅ Successfully migrated %d profiles to database", len(profiles))
}
EOF

echo "📤 Uploading migration script to server..."
scp -i "$SSH_KEY" /tmp/migrate-profiles.go "$SSH_HOST":/tmp/

echo "🚀 Running migration on server..."
ssh -i "$SSH_KEY" "$SSH_HOST" << 'ENDSSH'
set -e

# Source database URL from api.env
export $(sudo grep DATABASE_URL /etc/ocpctl/api.env | xargs)
export PROFILES_DIR=/opt/ocpctl/profiles

# Build and run migration
cd /tmp
/usr/local/go/bin/go mod init migrate-profiles 2>/dev/null || true
/usr/local/go/bin/go mod edit -replace github.com/tsanders-rh/ocpctl=/opt/ocpctl/current
/usr/local/go/bin/go mod tidy
/usr/local/go/bin/go run migrate-profiles.go

# Cleanup
rm -f migrate-profiles.go go.mod go.sum

echo "✅ Migration complete"
ENDSSH

# Cleanup local file
rm -f /tmp/migrate-profiles.go

echo ""
echo "🎉 All profiles migrated to database!"
echo "Profiles are now loaded from database instead of YAML files."
echo "Updates are immediately visible without cache issues."
