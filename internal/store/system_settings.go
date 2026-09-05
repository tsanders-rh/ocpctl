package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SystemSettingsStore is a generic key/value store backed by the system_settings
// table. Values are opaque JSON; callers marshal/unmarshal their own typed
// structs. It exists so the API (admin console) and the worker/janitor -- which
// run as separate processes on separate hosts -- can share runtime-tunable
// settings through the database, the only channel between them.
type SystemSettingsStore struct {
	pool *pgxpool.Pool
}

// Get returns the raw JSON value stored under key, or (nil, nil) if the key is
// absent. Absence is not an error: callers treat it as "fall back to the
// bootstrap default".
func (s *SystemSettingsStore) Get(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get system setting %s: %w", key, err)
	}
	return value, nil
}

// Upsert writes (or overwrites) the JSON value for key, recording who changed it.
// value must be valid JSON; the ::jsonb cast makes the string-encoded payload
// land in the JSONB column reliably.
func (s *SystemSettingsStore) Upsert(ctx context.Context, key string, value []byte, updatedBy string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_by, updated_at)
		VALUES ($1, $2::jsonb, $3, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()
	`, key, string(value), updatedBy)
	if err != nil {
		return fmt.Errorf("upsert system setting %s: %w", key, err)
	}
	return nil
}
