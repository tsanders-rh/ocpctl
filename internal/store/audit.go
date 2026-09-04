package store

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// normalizeIP validates a captured client IP before it is written to the
// audit_events.ip_address inet column. The IP comes from c.RealIP(), which
// trusts client-controlled proxy headers (X-Forwarded-For / X-Real-IP), so a
// malformed or crafted header (e.g. "\104.190.230.9") would otherwise fail the
// inet cast and drop the entire audit row. Returns a canonical IP string, or
// nil (SQL NULL) when the value is absent or not a valid IP — audit logging must
// never fail because of untrusted input.
func normalizeIP(raw *string) *string {
	if raw == nil {
		return nil
	}
	ip := net.ParseIP(strings.TrimSpace(*raw))
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}

// AuditStore handles audit event operations
type AuditStore struct {
	pool *pgxpool.Pool
}

// Log creates an immutable audit event record
func (s *AuditStore) Log(ctx context.Context, event *types.AuditEvent) error {
	query := `
		INSERT INTO audit_events (
			id, actor, action, target_cluster_id, target_job_id, target_user_id,
			status, metadata, ip_address, user_agent
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
	`

	_, err := s.pool.Exec(ctx, query,
		event.ID,
		event.Actor,
		event.Action,
		event.TargetClusterID,
		event.TargetJobID,
		event.TargetUserID,
		event.Status,
		event.Metadata,
		normalizeIP(event.IPAddress),
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
		SELECT id, actor, action, target_cluster_id, target_job_id, target_user_id,
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
			&event.TargetUserID,
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
		SELECT id, actor, action, target_cluster_id, target_job_id, target_user_id,
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
			&event.TargetUserID,
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
