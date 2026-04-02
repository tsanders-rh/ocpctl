package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, created_at, updated_at
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

	query += " ORDER BY category, name, is_default DESC"

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
			&addon.Version,
			&addon.DisplayName,
			&addon.IsDefault,
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
	// Use ConfigJSON if provided, otherwise marshal Config
	configJSON := addon.ConfigJSON
	if len(configJSON) == 0 && addon.Config.Operators != nil {
		var err error
		configJSON, err = json.Marshal(addon.Config)
		if err != nil {
			return fmt.Errorf("marshal add-on config: %w", err)
		}
	}

	query := `
		INSERT INTO post_config_addons (
			addon_id, name, description, category, config, supported_platforms,
			enabled, version, display_name, is_default
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`

	err := s.pool.QueryRow(ctx, query,
		addon.AddonID,
		addon.Name,
		addon.Description,
		addon.Category,
		configJSON,
		addon.SupportedPlatforms,
		addon.Enabled,
		addon.Version,
		addon.DisplayName,
		addon.IsDefault,
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

// GetByAddonIDAndVersion retrieves a specific version of an add-on
func (s *PostConfigAddonStore) GetByAddonIDAndVersion(ctx context.Context, addonID string, version string) (*types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, created_at, updated_at
		FROM post_config_addons
		WHERE addon_id = $1 AND version = $2
	`

	var addon types.PostConfigAddon
	var configJSON []byte

	err := s.pool.QueryRow(ctx, query, addonID, version).Scan(
		&addon.ID,
		&addon.AddonID,
		&addon.Name,
		&addon.Description,
		&addon.Category,
		&configJSON,
		&addon.SupportedPlatforms,
		&addon.Enabled,
		&addon.Version,
		&addon.DisplayName,
		&addon.IsDefault,
		&addon.CreatedAt,
		&addon.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get add-on by addon_id and version: %w", err)
	}

	if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &addon, nil
}

// ListVersions returns all versions of a specific add-on
func (s *PostConfigAddonStore) ListVersions(ctx context.Context, addonID string) ([]types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, created_at, updated_at
		FROM post_config_addons
		WHERE addon_id = $1 AND enabled = TRUE
		ORDER BY is_default DESC, version DESC
	`

	rows, err := s.pool.Query(ctx, query, addonID)
	if err != nil {
		return nil, fmt.Errorf("query versions: %w", err)
	}
	defer rows.Close()

	versions := []types.PostConfigAddon{}
	for rows.Next() {
		var addon types.PostConfigAddon
		var configJSON []byte

		if err := rows.Scan(
			&addon.ID, &addon.AddonID, &addon.Name, &addon.Description,
			&addon.Category, &configJSON, &addon.SupportedPlatforms,
			&addon.Enabled, &addon.Version, &addon.DisplayName,
			&addon.IsDefault, &addon.CreatedAt, &addon.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}

		if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		versions = append(versions, addon)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return versions, nil
}

// Update modifies an existing add-on
func (s *PostConfigAddonStore) Update(ctx context.Context, id string, addon *types.PostConfigAddon) error {
	query := `
		UPDATE post_config_addons
		SET name = $2, description = $3, category = $4, config = $5,
		    supported_platforms = $6, enabled = $7, version = $8,
		    display_name = $9, is_default = $10, updated_at = NOW()
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query,
		id,
		addon.Name,
		addon.Description,
		addon.Category,
		addon.ConfigJSON,
		addon.SupportedPlatforms,
		addon.Enabled,
		addon.Version,
		addon.DisplayName,
		addon.IsDefault,
	)

	if err != nil {
		return fmt.Errorf("update add-on: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
