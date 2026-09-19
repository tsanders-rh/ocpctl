package store

// These tests guard the migration *filenames* rather than their SQL. They need no
// database, so they run in CI's short mode alongside the rest of the build.
//
// Why they exist: RunMigrations keys each migration on the numeric prefix alone
// (`strings.Split(filename, "_")[0]`) and skips any version already recorded in
// schema_migrations. Two files sharing a prefix therefore mean whichever sorts
// first wins and the other is silently never applied — on an existing database the
// new one is skipped, and on a fresh database the older one is. Git merges the two
// files cleanly because the filenames differ, and VerifyMigrations compares only
// the highest version, so it passes either way. Nothing else catches this.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// migrationFilePattern is the shape RunMigrations and GetExpectedSchemaVersion
// both assume: a zero-padded 5-digit version, an underscore, then a description.
var migrationFilePattern = regexp.MustCompile(`^\d{5}_[a-z0-9_]+\.sql$`)

// migrationFilenames returns the same set of files, selected the same way, that
// RunMigrations iterates over.
func migrationFilenames(t *testing.T) []string {
	t.Helper()

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations directory: %v", err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no migrations found in the embedded filesystem")
	}
	return names
}

// TestMigrationVersionsAreUnique fails when two migrations share a numeric prefix.
// Renumber the newer one to the next free version.
func TestMigrationVersionsAreUnique(t *testing.T) {
	seen := make(map[string]string) // version -> first filename claiming it

	for _, filename := range migrationFilenames(t) {
		version := strings.Split(filename, "_")[0]

		if first, dup := seen[version]; dup {
			t.Errorf("migration version %s is claimed by two files: %s and %s\n"+
				"Only one of them will ever run — the runner keys on the numeric "+
				"prefix and skips a version it has already applied. Renumber the "+
				"newer file to the next free version.", version, first, filename)
			continue
		}
		seen[version] = filename
	}
}

// TestMigrationFilenamesAreWellFormed keeps every filename parseable by the
// prefix-splitting the runner does. A name like "71_foo.sql" or "00073.sql" would
// be accepted by RunMigrations but sort and compare wrongly against its siblings.
func TestMigrationFilenamesAreWellFormed(t *testing.T) {
	for _, filename := range migrationFilenames(t) {
		if !migrationFilePattern.MatchString(filename) {
			t.Errorf("migration %q does not match %s — expected a zero-padded "+
				"5-digit version, an underscore, then a lowercase description, "+
				"e.g. 00073_add_baremetal_platform_support.sql",
				filename, migrationFilePattern)
		}
	}
}

// TestMigrationVersionsAreContiguous reports gaps in the version sequence. A gap is
// not fatal to the runner, but it usually means a migration was renamed or dropped
// on one branch while another branch numbered past it — the same merge situation
// that produces duplicates. Informational only.
func TestMigrationVersionsAreContiguous(t *testing.T) {
	versions := make(map[int]bool)
	highest := 0

	for _, filename := range migrationFilenames(t) {
		var n int
		if _, err := fmt.Sscanf(strings.Split(filename, "_")[0], "%d", &n); err != nil {
			continue // covered by TestMigrationFilenamesAreWellFormed
		}
		versions[n] = true
		if n > highest {
			highest = n
		}
	}

	var missing []string
	for n := 1; n < highest; n++ {
		if !versions[n] {
			missing = append(missing, fmt.Sprintf("%05d", n))
		}
	}
	if len(missing) > 0 {
		t.Logf("gaps in the migration sequence (highest is %05d): %s",
			highest, strings.Join(missing, ", "))
	}
}
