package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PostConfigTemplateStore handles database operations for post-config templates
type PostConfigTemplateStore struct {
	pool *pgxpool.Pool
}

// Create creates a new template
func (s *PostConfigTemplateStore) Create(ctx context.Context, template *types.PostConfigTemplate) error {
	// Marshal config to JSONB
	configJSON, err := json.Marshal(template.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	query := `
		INSERT INTO postconfig_templates (id, name, description, config, owner_id, is_public, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = s.pool.Exec(ctx, query,
		template.ID,
		template.Name,
		template.Description,
		configJSON,
		template.OwnerID,
		template.IsPublic,
		template.Tags,
		template.CreatedAt,
		template.UpdatedAt,
	)

	return err
}

// GetByID retrieves a template by ID
func (s *PostConfigTemplateStore) GetByID(ctx context.Context, id string, userID string, isAdmin bool) (*types.PostConfigTemplate, error) {
	query := `
		SELECT id, name, description, config, owner_id, is_public, tags, created_at, updated_at
		FROM postconfig_templates
		WHERE id = $1 AND (is_public = TRUE OR owner_id = $2 OR $3 = TRUE)
	`

	var template types.PostConfigTemplate
	var configJSON []byte

	err := s.pool.QueryRow(ctx, query, id, userID, isAdmin).Scan(
		&template.ID,
		&template.Name,
		&template.Description,
		&configJSON,
		&template.OwnerID,
		&template.IsPublic,
		&template.Tags,
		&template.CreatedAt,
		&template.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// Unmarshal config
	if err := json.Unmarshal(configJSON, &template.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &template, nil
}

// List retrieves templates visible to the user
func (s *PostConfigTemplateStore) List(ctx context.Context, userID string, isAdmin bool, onlyPublic bool) ([]*types.PostConfigTemplate, error) {
	var query string
	var args []interface{}

	if onlyPublic {
		// Only public templates
		query = `
			SELECT id, name, description, config, owner_id, is_public, tags, created_at, updated_at
			FROM postconfig_templates
			WHERE is_public = TRUE
			ORDER BY created_at DESC
		`
		args = []interface{}{}
	} else if isAdmin {
		// Admins see all templates
		query = `
			SELECT id, name, description, config, owner_id, is_public, tags, created_at, updated_at
			FROM postconfig_templates
			ORDER BY created_at DESC
		`
		args = []interface{}{}
	} else {
		// Users see their own templates + public templates
		query = `
			SELECT id, name, description, config, owner_id, is_public, tags, created_at, updated_at
			FROM postconfig_templates
			WHERE owner_id = $1 OR is_public = TRUE
			ORDER BY created_at DESC
		`
		args = []interface{}{userID}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := make([]*types.PostConfigTemplate, 0)
	for rows.Next() {
		var template types.PostConfigTemplate
		var configJSON []byte

		if err := rows.Scan(
			&template.ID,
			&template.Name,
			&template.Description,
			&configJSON,
			&template.OwnerID,
			&template.IsPublic,
			&template.Tags,
			&template.CreatedAt,
			&template.UpdatedAt,
		); err != nil {
			return nil, err
		}

		// Unmarshal config
		if err := json.Unmarshal(configJSON, &template.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		templates = append(templates, &template)
	}

	return templates, rows.Err()
}

// Update updates a template (only owner can update)
func (s *PostConfigTemplateStore) Update(ctx context.Context, template *types.PostConfigTemplate) error {
	// Marshal config to JSONB
	configJSON, err := json.Marshal(template.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	query := `
		UPDATE postconfig_templates
		SET name = $1, description = $2, config = $3, is_public = $4, tags = $5, updated_at = $6
		WHERE id = $7 AND owner_id = $8
	`

	result, err := s.pool.Exec(ctx, query,
		template.Name,
		template.Description,
		configJSON,
		template.IsPublic,
		template.Tags,
		template.UpdatedAt,
		template.ID,
		template.OwnerID,
	)

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("template not found or access denied")
	}

	return nil
}

// Delete deletes a template (only owner can delete, or admin)
func (s *PostConfigTemplateStore) Delete(ctx context.Context, id string, userID string, isAdmin bool) error {
	var query string
	var args []interface{}

	if isAdmin {
		query = `DELETE FROM postconfig_templates WHERE id = $1`
		args = []interface{}{id}
	} else {
		query = `DELETE FROM postconfig_templates WHERE id = $1 AND owner_id = $2`
		args = []interface{}{id, userID}
	}

	result, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("template not found or access denied")
	}

	return nil
}

// SearchByTags searches templates by tags
func (s *PostConfigTemplateStore) SearchByTags(ctx context.Context, tags []string, userID string, isAdmin bool) ([]*types.PostConfigTemplate, error) {
	var query string
	var args []interface{}

	if isAdmin {
		query = `
			SELECT id, name, description, config, owner_id, is_public, tags, created_at, updated_at
			FROM postconfig_templates
			WHERE tags && $1
			ORDER BY created_at DESC
		`
		args = []interface{}{tags}
	} else {
		query = `
			SELECT id, name, description, config, owner_id, is_public, tags, created_at, updated_at
			FROM postconfig_templates
			WHERE tags && $1 AND (owner_id = $2 OR is_public = TRUE)
			ORDER BY created_at DESC
		`
		args = []interface{}{tags, userID}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := make([]*types.PostConfigTemplate, 0)
	for rows.Next() {
		var template types.PostConfigTemplate
		var configJSON []byte

		if err := rows.Scan(
			&template.ID,
			&template.Name,
			&template.Description,
			&configJSON,
			&template.OwnerID,
			&template.IsPublic,
			&template.Tags,
			&template.CreatedAt,
			&template.UpdatedAt,
		); err != nil {
			return nil, err
		}

		// Unmarshal config
		if err := json.Unmarshal(configJSON, &template.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		templates = append(templates, &template)
	}

	return templates, rows.Err()
}
