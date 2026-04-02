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
func (s *Syncer) Sync(ctx context.Context) error {
	log.Println("Syncing add-ons from YAML to database...")

	addons, err := s.loader.LoadAll()
	if err != nil {
		return fmt.Errorf("load add-on definitions: %w", err)
	}

	synced := 0
	for _, addon := range addons {
		for i := range addon.Versions {
			if err := s.syncVersion(ctx, addon, &addon.Versions[i]); err != nil {
				return fmt.Errorf("sync %s:%s: %w", addon.ID, addon.Versions[i].Channel, err)
			}
			synced++
		}
	}

	log.Printf("✓ Synced %d add-on versions from YAML to database", synced)
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
