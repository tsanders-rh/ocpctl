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
		       enabled, version, display_name, is_default, metadata,
		       addon_source, created_by_user_id, is_published, published_at,
		       parent_version_id, version_number, is_immutable,
		       created_at, updated_at
		FROM post_config_addons
		WHERE enabled = TRUE
		  AND (addon_source = 'system' OR (addon_source = 'user' AND is_published = TRUE))
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
		var metadataJSON []byte

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
			&metadataJSON,
			&addon.AddonSource,
			&addon.CreatedByUserID,
			&addon.IsPublished,
			&addon.PublishedAt,
			&addon.ParentVersionID,
			&addon.VersionNumber,
			&addon.IsImmutable,
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

		// Unmarshal metadata JSONB if present
		if len(metadataJSON) > 0 {
			addon.Metadata = &types.AddonMetadata{}
			if err := json.Unmarshal(metadataJSON, addon.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal add-on metadata: %w", err)
			}
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
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, metadata,
		       addon_source, created_by_user_id, is_published, published_at,
		       parent_version_id, version_number, is_immutable,
		       created_at, updated_at
		FROM post_config_addons
		WHERE id = $1
	`

	var addon types.PostConfigAddon
	var configJSON []byte
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, query, id).Scan(
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
		&metadataJSON,
		&addon.AddonSource,
		&addon.CreatedByUserID,
		&addon.IsPublished,
		&addon.PublishedAt,
		&addon.ParentVersionID,
		&addon.VersionNumber,
		&addon.IsImmutable,
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

	// Unmarshal metadata JSONB if present
	if len(metadataJSON) > 0 {
		addon.Metadata = &types.AddonMetadata{}
		if err := json.Unmarshal(metadataJSON, addon.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal add-on metadata: %w", err)
		}
	}

	return &addon, nil
}

// GetByAddonID retrieves an add-on by its addon_id (e.g., "oadp")
func (s *PostConfigAddonStore) GetByAddonID(ctx context.Context, addonID string) (*types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, metadata,
		       addon_source, created_by_user_id, is_published, published_at,
		       parent_version_id, version_number, is_immutable,
		       created_at, updated_at
		FROM post_config_addons
		WHERE addon_id = $1 AND enabled = TRUE AND is_default = TRUE
		  AND (addon_source = 'system' OR (addon_source = 'user' AND is_published = TRUE))
	`

	var addon types.PostConfigAddon
	var configJSON []byte
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, query, addonID).Scan(
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
		&metadataJSON,
		&addon.AddonSource,
		&addon.CreatedByUserID,
		&addon.IsPublished,
		&addon.PublishedAt,
		&addon.ParentVersionID,
		&addon.VersionNumber,
		&addon.IsImmutable,
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

	// Unmarshal metadata JSONB if present
	if len(metadataJSON) > 0 {
		addon.Metadata = &types.AddonMetadata{}
		if err := json.Unmarshal(metadataJSON, addon.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal add-on metadata: %w", err)
		}
	}

	return &addon, nil
}

// Create creates a new add-on in the database
func (s *PostConfigAddonStore) Create(ctx context.Context, addon *types.PostConfigAddon) error {
	// Use ConfigJSON if provided, otherwise marshal Config
	configJSON := addon.ConfigJSON
	if len(configJSON) == 0 {
		var err error
		configJSON, err = json.Marshal(addon.Config)
		if err != nil {
			return fmt.Errorf("marshal add-on config: %w", err)
		}
	}

	query := `
		INSERT INTO post_config_addons (
			addon_id, name, description, category, config, supported_platforms,
			enabled, version, display_name, is_default, metadata,
			addon_source, created_by_user_id, is_published, published_at,
			parent_version_id, version_number, is_immutable
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
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
		addon.MetadataJSON,
		addon.AddonSource,
		addon.CreatedByUserID,
		addon.IsPublished,
		addon.PublishedAt,
		addon.ParentVersionID,
		addon.VersionNumber,
		addon.IsImmutable,
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
		       enabled, version, display_name, is_default, metadata,
		       addon_source, created_by_user_id, is_published, published_at,
		       parent_version_id, version_number, is_immutable,
		       created_at, updated_at
		FROM post_config_addons
		WHERE addon_id = $1 AND version = $2
		  AND (addon_source = 'system' OR (addon_source = 'user' AND is_published = TRUE))
	`

	var addon types.PostConfigAddon
	var configJSON []byte
	var metadataJSON []byte

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
		&metadataJSON,
		&addon.AddonSource,
		&addon.CreatedByUserID,
		&addon.IsPublished,
		&addon.PublishedAt,
		&addon.ParentVersionID,
		&addon.VersionNumber,
		&addon.IsImmutable,
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

	if metadataJSON != nil {
		if err := json.Unmarshal(metadataJSON, &addon.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return &addon, nil
}

// GetByAddonIDAndVersionForUser retrieves a specific version of an add-on, including draft addons owned by the specified user
func (s *PostConfigAddonStore) GetByAddonIDAndVersionForUser(ctx context.Context, addonID string, version string, userID string) (*types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, metadata,
		       addon_source, created_by_user_id, is_published, published_at,
		       parent_version_id, version_number, is_immutable,
		       created_at, updated_at
		FROM post_config_addons
		WHERE addon_id = $1 AND version = $2
		  AND (addon_source = 'system'
		       OR (addon_source = 'user' AND is_published = TRUE)
		       OR (addon_source = 'user' AND created_by_user_id = $3))
	`

	var addon types.PostConfigAddon
	var configJSON []byte
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, query, addonID, version, userID).Scan(
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
		&metadataJSON,
		&addon.AddonSource,
		&addon.CreatedByUserID,
		&addon.IsPublished,
		&addon.PublishedAt,
		&addon.ParentVersionID,
		&addon.VersionNumber,
		&addon.IsImmutable,
		&addon.CreatedAt,
		&addon.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get add-on by addon_id and version for user: %w", err)
	}

	if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if metadataJSON != nil {
		if err := json.Unmarshal(metadataJSON, &addon.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return &addon, nil
}

// ListVersions returns all versions of a specific add-on
func (s *PostConfigAddonStore) ListVersions(ctx context.Context, addonID string) ([]types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, metadata,
		       addon_source, created_by_user_id, is_published, published_at,
		       parent_version_id, version_number, is_immutable,
		       created_at, updated_at
		FROM post_config_addons
		WHERE addon_id = $1 AND enabled = TRUE
		  AND (addon_source = 'system' OR (addon_source = 'user' AND is_published = TRUE))
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
		var metadataJSON []byte

		if err := rows.Scan(
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
			&metadataJSON,
			&addon.AddonSource,
			&addon.CreatedByUserID,
			&addon.IsPublished,
			&addon.PublishedAt,
			&addon.ParentVersionID,
			&addon.VersionNumber,
			&addon.IsImmutable,
			&addon.CreatedAt,
			&addon.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}

		if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		if metadataJSON != nil {
			if err := json.Unmarshal(metadataJSON, &addon.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		}

		versions = append(versions, addon)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return versions, nil
}

// Update modifies an existing add-on (only if not published and immutable)
func (s *PostConfigAddonStore) Update(ctx context.Context, id string, addon *types.PostConfigAddon) error {
	// Check if addon is a published user addon (which are immutable)
	var isPublished bool
	var isImmutable bool
	var addonSource string
	checkQuery := `SELECT is_published, is_immutable, addon_source FROM post_config_addons WHERE id = $1`
	if err := s.pool.QueryRow(ctx, checkQuery, id).Scan(&isPublished, &isImmutable, &addonSource); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("check addon status: %w", err)
	}

	// Only block updates for published user addons (which are immutable for version control)
	// System addons can always be updated when YAML changes
	if addonSource == "user" && isPublished && isImmutable {
		return fmt.Errorf("cannot update published addon (clone to create new version)")
	}

	// Use ConfigJSON if provided, otherwise marshal Config
	configJSON := addon.ConfigJSON
	if len(configJSON) == 0 {
		var err error
		configJSON, err = json.Marshal(addon.Config)
		if err != nil {
			return fmt.Errorf("marshal add-on config: %w", err)
		}
	}

	query := `
		UPDATE post_config_addons
		SET name = $2, description = $3, category = $4, config = $5,
		    supported_platforms = $6, enabled = $7, version = $8,
		    display_name = $9, is_default = $10, metadata = $11, updated_at = NOW()
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query,
		id,
		addon.Name,
		addon.Description,
		addon.Category,
		configJSON,
		addon.SupportedPlatforms,
		addon.Enabled,
		addon.Version,
		addon.DisplayName,
		addon.IsDefault,
		addon.MetadataJSON,
	)

	if err != nil {
		return fmt.Errorf("update add-on: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetUserAddons retrieves all addons created by a specific user
func (s *PostConfigAddonStore) GetUserAddons(ctx context.Context, userID string) ([]types.PostConfigAddon, error) {
	query := `
		SELECT id, addon_id, name, description, category, config, supported_platforms,
		       enabled, version, display_name, is_default, metadata,
		       addon_source, created_by_user_id, is_published, published_at,
		       parent_version_id, version_number, is_immutable,
		       created_at, updated_at
		FROM post_config_addons
		WHERE addon_source = 'user' AND created_by_user_id = $1
		ORDER BY updated_at DESC
	`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query user addons: %w", err)
	}
	defer rows.Close()

	var addons []types.PostConfigAddon
	for rows.Next() {
		var addon types.PostConfigAddon
		var configJSON []byte
		var metadataJSON []byte

		err := rows.Scan(
			&addon.ID, &addon.AddonID, &addon.Name, &addon.Description,
			&addon.Category, &configJSON, &addon.SupportedPlatforms,
			&addon.Enabled, &addon.Version, &addon.DisplayName,
			&addon.IsDefault, &metadataJSON,
			&addon.AddonSource, &addon.CreatedByUserID, &addon.IsPublished,
			&addon.PublishedAt, &addon.ParentVersionID, &addon.VersionNumber,
			&addon.IsImmutable, &addon.CreatedAt, &addon.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan addon: %w", err)
		}

		if err := json.Unmarshal(configJSON, &addon.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		if len(metadataJSON) > 0 {
			addon.Metadata = &types.AddonMetadata{}
			if err := json.Unmarshal(metadataJSON, addon.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		}

		addons = append(addons, addon)
	}

	return addons, rows.Err()
}

// PublishAddon marks an addon as published and immutable
func (s *PostConfigAddonStore) PublishAddon(ctx context.Context, id string) error {
	query := `
		UPDATE post_config_addons
		SET is_published = TRUE,
		    is_immutable = TRUE,
		    published_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1 AND addon_source = 'user' AND is_published = FALSE
	`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("publish addon: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("addon not found, not a user addon, or already published")
	}

	return nil
}

// CloneAddon creates a new draft addon based on a parent version
func (s *PostConfigAddonStore) CloneAddon(ctx context.Context, parentID string, userID string) (*types.PostConfigAddon, error) {
	// First, get the parent addon
	parent, err := s.GetByID(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("get parent addon: %w", err)
	}

	// Create new addon as a clone
	clone := &types.PostConfigAddon{
		AddonID:            fmt.Sprintf("%s-v%d", parent.AddonID, parent.VersionNumber+1),
		Name:               parent.Name,
		Description:        parent.Description,
		Category:           parent.Category,
		Config:             parent.Config,
		SupportedPlatforms: parent.SupportedPlatforms,
		Enabled:            parent.Enabled,
		Version:            parent.Version,
		DisplayName:        parent.DisplayName,
		IsDefault:          false, // Clones are never default
		Metadata:           parent.Metadata,
		AddonSource:        "user",
		CreatedByUserID:    &userID,
		IsPublished:        false,
		ParentVersionID:    &parentID,
		VersionNumber:      parent.VersionNumber + 1,
		IsImmutable:        false,
	}

	// Marshal config for storage
	configJSON, err := json.Marshal(clone.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	clone.ConfigJSON = configJSON

	// Marshal metadata if present
	if clone.Metadata != nil {
		metadataJSON, err := json.Marshal(clone.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		clone.MetadataJSON = metadataJSON
	}

	// Create the clone
	if err := s.Create(ctx, clone); err != nil {
		return nil, fmt.Errorf("create clone: %w", err)
	}

	return clone, nil
}
