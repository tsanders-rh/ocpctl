package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PostConfigAddonStore handles database operations for add-ons
type PostConfigAddonStore struct {
	pool *pgxpool.Pool
}

// NewPostConfigAddonStore creates a new add-on store
func NewPostConfigAddonStore(pool *pgxpool.Pool) *PostConfigAddonStore {
	return &PostConfigAddonStore{pool: pool}
}

// List retrieves all enabled add-ons, optionally filtered by category and platform
func (s *PostConfigAddonStore) List(ctx context.Context, category *string, platform *string) ([]types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms, enabled, created_at, updated_at
		FROM post_config_addons
		WHERE enabled = TRUE
	`

	args := []interface{}{}
	argCount := 1

	if category != nil && *category != "" {
		query += fmt.Sprintf(" AND category = $%d", argCount)
		args = append(args, *category)
		argCount++
	}

	if platform != nil && *platform != "" {
		query += fmt.Sprintf(" AND $%d = ANY(supported_platforms)", argCount)
		args = append(args, *platform)
		argCount++
	}

	query += " ORDER BY category, name"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query add-ons: %w", err)
	}
	defer rows.Close()

	var addons []types.PostConfigAddon
	for rows.Next() {
		var addon types.PostConfigAddon
		var configJSON []byte

		err := rows.Scan(
			&addon.ID,
			&addon.AddonID,
			&addon.Name,
			&addon.Description,
			&addon.Category,
			&configJSON,
			&addon.SupportedPlatforms,
			&addon.Enabled,
			&addon.CreatedAt,
			&addon.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan add-on: %w", err)
		}

		// Unmarshal config JSONB
		if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
			return nil, fmt.Errorf("unmarshal add-on config: %w", err)
		}

		addons = append(addons, addon)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return addons, nil
}

// GetByID retrieves an add-on by its UUID
func (s *PostConfigAddonStore) GetByID(ctx context.Context, id string) (*types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms, enabled, created_at, updated_at
		FROM post_config_addons
		WHERE id = $1
	`

	var addon types.PostConfigAddon
	var configJSON []byte

	err := s.pool.QueryRow(ctx, query, id).Scan(
		&addon.ID,
		&addon.AddonID,
		&addon.Name,
		&addon.Description,
		&addon.Category,
		&configJSON,
		&addon.SupportedPlatforms,
		&addon.Enabled,
		&addon.CreatedAt,
		&addon.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get add-on by id: %w", err)
	}

	// Unmarshal config JSONB
	if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
		return nil, fmt.Errorf("unmarshal add-on config: %w", err)
	}

	return &addon, nil
}

// GetByAddonID retrieves an add-on by its addon_id (e.g., "oadp")
func (s *PostConfigAddonStore) GetByAddonID(ctx context.Context, addonID string) (*types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms, enabled, created_at, updated_at
		FROM post_config_addons
		WHERE addon_id = $1 AND enabled = TRUE
	`

	var addon types.PostConfigAddon
	var configJSON []byte

	err := s.pool.QueryRow(ctx, query, addonID).Scan(
		&addon.ID,
		&addon.AddonID,
		&addon.Name,
		&addon.Description,
		&addon.Category,
		&configJSON,
		&addon.SupportedPlatforms,
		&addon.Enabled,
		&addon.CreatedAt,
		&addon.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get add-on by addon_id: %w", err)
	}

	// Unmarshal config JSONB
	if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
		return nil, fmt.Errorf("unmarshal add-on config: %w", err)
	}

	return &addon, nil
}

// Create creates a new add-on in the database
func (s *PostConfigAddonStore) Create(ctx context.Context, addon *types.PostConfigAddon) error {
	// Marshal config to JSONB
	configJSON, err := json.Marshal(addon.Config)
	if err != nil {
		return fmt.Errorf("marshal add-on config: %w", err)
	}

	query := `
		INSERT INTO post_config_addons (addon_id, name, description, category, config, supported_platforms, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`

	err = s.pool.QueryRow(ctx, query,
		addon.AddonID,
		addon.Name,
		addon.Description,
		addon.Category,
		configJSON,
		addon.SupportedPlatforms,
		addon.Enabled,
	).Scan(&addon.ID, &addon.CreatedAt, &addon.UpdatedAt)

	if err != nil {
		return fmt.Errorf("create add-on: %w", err)
	}

	return nil
}

// Delete deletes an add-on by its ID
func (s *PostConfigAddonStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM post_config_addons WHERE id = $1`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete add-on: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("add-on not found")
	}

	return nil
}
