package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// BroadcastAlertStore handles database operations for admin broadcast alerts.
type BroadcastAlertStore struct {
	pool *pgxpool.Pool
}

// Create inserts a new broadcast alert, populating the generated ID and created_at.
func (s *BroadcastAlertStore) Create(ctx context.Context, a *types.BroadcastAlert) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO broadcast_alerts (title, body, severity, created_by, active, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, a.Title, a.Body, string(a.Severity), a.CreatedBy, a.Active, a.ExpiresAt).Scan(&a.ID, &a.CreatedAt)
}

// ListActiveForUser returns active, non-expired alerts the user has not yet
// acknowledged, ordered most-severe then most-recent first. Users created after
// an alert was posted are covered automatically (they simply have no ack row).
func (s *BroadcastAlertStore) ListActiveForUser(ctx context.Context, userID string) ([]*types.BroadcastAlert, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.title, a.body, a.severity, a.created_by, a.active, a.expires_at, a.created_at
		FROM broadcast_alerts a
		LEFT JOIN broadcast_alert_acks k
		  ON k.alert_id = a.id AND k.user_id = $1
		WHERE a.active = TRUE
		  AND (a.expires_at IS NULL OR a.expires_at > NOW())
		  AND k.alert_id IS NULL
		ORDER BY
		  CASE a.severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
		  a.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAlerts(rows)
}

// Acknowledge records that a user has dismissed an alert. Idempotent.
func (s *BroadcastAlertStore) Acknowledge(ctx context.Context, alertID, userID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO broadcast_alert_acks (alert_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (alert_id, user_id) DO NOTHING
	`, alertID, userID)
	return err
}

// ListAll returns every alert with acknowledgment counts, newest first (admin view).
func (s *BroadcastAlertStore) ListAll(ctx context.Context) ([]*types.BroadcastAlert, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.title, a.body, a.severity, a.created_by, a.active, a.expires_at, a.created_at,
		       (SELECT COUNT(*) FROM broadcast_alert_acks k WHERE k.alert_id = a.id) AS ack_count,
		       (SELECT COUNT(*) FROM users) AS total_users
		FROM broadcast_alerts a
		ORDER BY a.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	alerts := make([]*types.BroadcastAlert, 0)
	for rows.Next() {
		var a types.BroadcastAlert
		if err := rows.Scan(&a.ID, &a.Title, &a.Body, &a.Severity, &a.CreatedBy, &a.Active, &a.ExpiresAt, &a.CreatedAt, &a.AckCount, &a.TotalUsers); err != nil {
			return nil, err
		}
		alerts = append(alerts, &a)
	}
	return alerts, rows.Err()
}

// Deactivate marks an alert inactive so it stops showing to users.
func (s *BroadcastAlertStore) Deactivate(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `UPDATE broadcast_alerts SET active = FALSE WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("alert not found")
	}
	return nil
}

// scanAlerts scans a result set of the standard alert column list (no ack counts).
func scanAlerts(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*types.BroadcastAlert, error) {
	alerts := make([]*types.BroadcastAlert, 0)
	for rows.Next() {
		var a types.BroadcastAlert
		if err := rows.Scan(&a.ID, &a.Title, &a.Body, &a.Severity, &a.CreatedBy, &a.Active, &a.ExpiresAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, &a)
	}
	return alerts, rows.Err()
}
