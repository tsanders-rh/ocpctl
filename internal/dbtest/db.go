// Package dbtest provides a shared PostgreSQL harness for store- and API-level
// tests. Tests that need a real database call New; when TEST_DATABASE_URL is not
// set (e.g. a developer running `go test` without a database) the test is skipped
// rather than failed. CI sets TEST_DATABASE_URL so these tests always run there.
package dbtest

import (
	"os"
	"testing"

	"github.com/tsanders-rh/ocpctl/internal/store"
)

// New returns a migrated Store connected to the database in TEST_DATABASE_URL,
// or skips the calling test when that variable is unset.
func New(t testing.TB) *store.Store {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database-backed test")
	}

	s, err := store.NewStore(url)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	if err := s.Migrate(); err != nil {
		s.Close()
		t.Fatalf("migrate test database: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}
