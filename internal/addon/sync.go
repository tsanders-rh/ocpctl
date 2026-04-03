package addon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Syncer handles synchronizing YAML definitions to the database
type Syncer struct {
	loader *Loader
	store  *store.PostConfigAddonStore
}

func NewSyncer(loader *Loader, store *store.PostConfigAddonStore) *Syncer {
	return &Syncer{
		loader: loader,
		store:  store,
	}
}

// Sync loads all YAML definitions and updates the database
// It also deletes versions that are no longer in YAML
func (s *Syncer) Sync(ctx context.Context) error {
	log.Println("Syncing add-ons from YAML to database...")

	addons, err := s.loader.LoadAll()
	if err != nil {
		return fmt.Errorf("load add-on definitions: %w", err)
	}

	synced := 0
	deleted := 0
	for _, addon := range addons {
		// Get all existing versions for this addon from database
		existingVersions, err := s.store.ListVersions(ctx, addon.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("list existing versions for %s: %w", addon.ID, err)
		}

		// Build set of channels from YAML
		yamlChannels := make(map[string]bool)
		for i := range addon.Versions {
			yamlChannels[addon.Versions[i].Channel] = true
		}

		// Delete versions not in YAML
		for _, existingVersion := range existingVersions {
			if !yamlChannels[existingVersion.Version] {
				if err := s.store.Delete(ctx, existingVersion.ID); err != nil {
					log.Printf("Warning: failed to delete old version %s:%s: %v", addon.ID, existingVersion.Version, err)
				} else {
					log.Printf("  Deleted old version: %s:%s", addon.ID, existingVersion.Version)
					deleted++
				}
			}
		}

		// Sync versions from YAML
		for i := range addon.Versions {
			if err := s.syncVersion(ctx, addon, &addon.Versions[i]); err != nil {
				return fmt.Errorf("sync %s:%s: %w", addon.ID, addon.Versions[i].Channel, err)
			}
			synced++
		}
	}

	if deleted > 0 {
		log.Printf("✓ Synced %d add-on versions from YAML to database (%d deleted)", synced, deleted)
	} else {
		log.Printf("✓ Synced %d add-on versions from YAML to database", synced)
	}
	return nil
}

func (s *Syncer) syncVersion(ctx context.Context, addon *AddonDefinition, version *AddonVersionConfig) error {
	// Marshal config to JSONB
	configJSON, err := json.Marshal(version.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Check if this addon+version exists
	existing, err := s.store.GetByAddonIDAndVersion(ctx, addon.ID, version.Channel)

	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("check existing: %w", err)
	}

	if existing != nil {
		// Update existing record
		return s.store.Update(ctx, existing.ID, &types.PostConfigAddon{
			AddonID:            addon.ID,
			Name:               addon.Name,
			Description:        addon.Description,
			Category:           addon.Category,
			ConfigJSON:         configJSON,
			SupportedPlatforms: addon.SupportedPlatforms,
			Enabled:            addon.Enabled,
			Version:            version.Channel,
			DisplayName:        version.DisplayName,
			IsDefault:          version.IsDefault,
		})
	}

	// Create new record
	return s.store.Create(ctx, &types.PostConfigAddon{
		AddonID:            addon.ID,
		Name:               addon.Name,
		Description:        addon.Description,
		Category:           addon.Category,
		ConfigJSON:         configJSON,
		SupportedPlatforms: addon.SupportedPlatforms,
		Enabled:            addon.Enabled,
		Version:            version.Channel,
		DisplayName:        version.DisplayName,
		IsDefault:          version.IsDefault,
	})
}
