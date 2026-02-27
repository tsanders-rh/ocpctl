package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// AuditStore handles audit event operations
type AuditStore struct {
	pool *pgxpool.Pool
}

// Log creates an immutable audit event record
func (s *AuditStore) Log(ctx context.Context, event *types.AuditEvent) error {
	query := `
		INSERT INTO audit_events (
			id, actor, action, target_cluster_id, target_job_id,
			status, metadata, ip_address, user_agent
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	_, err := s.pool.Exec(ctx, query,
		event.ID,
		event.Actor,
		event.Action,
		event.TargetClusterID,
		event.TargetJobID,
		event.Status,
		event.Metadata,
		event.IPAddress,
		event.UserAgent,
	)

	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return nil
}

// ListByActor retrieves audit events for an actor
func (s *AuditStore) ListByActor(ctx context.Context, actor string, limit, offset int) ([]*types.AuditEvent, error) {
	query := `
		SELECT id, actor, action, target_cluster_id, target_job_id,
			status, metadata, ip_address, user_agent, created_at
		FROM audit_events
		WHERE actor = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, actor, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query audit events by actor: %w", err)
	}
	defer rows.Close()

	events := []*types.AuditEvent{}
	for rows.Next() {
		var event types.AuditEvent
		err := rows.Scan(
			&event.ID,
			&event.Actor,
			&event.Action,
			&event.TargetClusterID,
			&event.TargetJobID,
			&event.Status,
			&event.Metadata,
			&event.IPAddress,
			&event.UserAgent,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}

	return events, nil
}

// ListByCluster retrieves audit events for a cluster
func (s *AuditStore) ListByCluster(ctx context.Context, clusterID string) ([]*types.AuditEvent, error) {
	query := `
		SELECT id, actor, action, target_cluster_id, target_job_id,
			status, metadata, ip_address, user_agent, created_at
		FROM audit_events
		WHERE target_cluster_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query audit events by cluster: %w", err)
	}
	defer rows.Close()

	events := []*types.AuditEvent{}
	for rows.Next() {
		var event types.AuditEvent
		err := rows.Scan(
			&event.ID,
			&event.Actor,
			&event.Action,
			&event.TargetClusterID,
			&event.TargetJobID,
			&event.Status,
			&event.Metadata,
			&event.IPAddress,
			&event.UserAgent,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}

	return events, nil
}
