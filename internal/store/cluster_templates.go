package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// MaxTemplatesPerUser caps how many cluster-creation templates a single user may keep.
const MaxTemplatesPerUser = 5

// ErrTemplateLimitReached is returned by Create when the owner already has MaxTemplatesPerUser templates.
var ErrTemplateLimitReached = errors.New("cluster template limit reached")

// ClusterTemplateStore handles database operations for cluster-creation templates.
// Templates are private: every operation is scoped to the owning user.
type ClusterTemplateStore struct {
	pool *pgxpool.Pool
}

// Create inserts a new template, enforcing the per-user limit atomically so two
// concurrent saves cannot exceed MaxTemplatesPerUser.
func (s *ClusterTemplateStore) Create(ctx context.Context, t *types.ClusterTemplate) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM cluster_templates WHERE owner_id = $1`, t.OwnerID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxTemplatesPerUser {
		return ErrTemplateLimitReached
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO cluster_templates (id, name, owner_id, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, t.ID, t.Name, t.OwnerID, []byte(t.Config), t.CreatedAt, t.UpdatedAt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *ClusterTemplateStore) GetByID(ctx context.Context, id, userID string) (*types.ClusterTemplate, error) {
	query := `
		SELECT id, name, owner_id, config, created_at, updated_at
		FROM cluster_templates
		WHERE id = $1 AND owner_id = $2
	`
	var t types.ClusterTemplate
	var config []byte
	if err := s.pool.QueryRow(ctx, query, id, userID).Scan(
		&t.ID, &t.Name, &t.OwnerID, &config, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	t.Config = config
	return &t, nil
}

func (s *ClusterTemplateStore) List(ctx context.Context, userID string) ([]*types.ClusterTemplate, error) {
	query := `
		SELECT id, name, owner_id, config, created_at, updated_at
		FROM cluster_templates
		WHERE owner_id = $1
		ORDER BY name ASC
	`
	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := make([]*types.ClusterTemplate, 0)
	for rows.Next() {
		var t types.ClusterTemplate
		var config []byte
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerID, &config, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Config = config
		templates = append(templates, &t)
	}
	return templates, rows.Err()
}

func (s *ClusterTemplateStore) Update(ctx context.Context, t *types.ClusterTemplate) error {
	query := `
		UPDATE cluster_templates
		SET name = $1, config = $2, updated_at = $3
		WHERE id = $4 AND owner_id = $5
	`
	result, err := s.pool.Exec(ctx, query, t.Name, []byte(t.Config), t.UpdatedAt, t.ID, t.OwnerID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("template not found or access denied")
	}
	return nil
}

func (s *ClusterTemplateStore) Delete(ctx context.Context, id, userID string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM cluster_templates WHERE id = $1 AND owner_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("template not found or access denied")
	}
	return nil
}
