package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store provides database operations
type Store struct {
	pool *pgxpool.Pool

	Clusters             *ClusterStore
	Jobs                 *JobStore
	JobLocks             *JobLockStore
	JobRetryHistory      *JobRetryHistoryStore
	Idempotency          *IdempotencyStore
	RBAC                 *RBACStore
	Audit                *AuditStore
	DestroyAudit         *DestroyAuditStore
	ClusterOutputs       *ClusterOutputsStore
	Artifacts            *ArtifactStore
	Usage                *UsageStore
	Users                *UserStore
	RefreshTokens        *RefreshTokenStore
	APIKeys              *APIKeyStore
	IAMMappings          *IAMMappingStore
	DeploymentLogs       *DeploymentLogStore
	StorageGroups             *StorageGroupStore
	ClusterStorageLinks       *ClusterStorageLinkStore
	OrphanedResources         *OrphanedResourceStore
	ClusterConfigurations     *ClusterConfigurationStore
	ProfileDeploymentMetrics  *ProfileDeploymentMetricsStore
	PostConfigAddons          *PostConfigAddonStore
	PostConfigTemplates       *PostConfigTemplateStore
}

// New creates a new Store with all sub-stores initialized using the provided database connection pool.
// All store operations (Clusters, Jobs, etc.) share the same connection pool for efficient resource usage.
func New(pool *pgxpool.Pool) *Store {
	s := &Store{
		pool: pool,
	}

	s.Clusters = &ClusterStore{pool: pool}
	s.Jobs = &JobStore{pool: pool}
	s.JobLocks = &JobLockStore{pool: pool}
	s.JobRetryHistory = &JobRetryHistoryStore{pool: pool}
	s.Idempotency = &IdempotencyStore{pool: pool}
	s.RBAC = &RBACStore{pool: pool}
	s.Audit = &AuditStore{pool: pool}
	s.DestroyAudit = &DestroyAuditStore{pool: pool}
	s.ClusterOutputs = &ClusterOutputsStore{pool: pool}
	s.Artifacts = &ArtifactStore{pool: pool}
	s.Usage = &UsageStore{pool: pool}
	s.Users = &UserStore{pool: pool}
	s.RefreshTokens = &RefreshTokenStore{pool: pool}
	s.APIKeys = &APIKeyStore{pool: pool}
	s.IAMMappings = &IAMMappingStore{
		pool:  pool,
		cache: make(map[string]*types.IAMPrincipalMapping),
	}
	s.DeploymentLogs = &DeploymentLogStore{pool: pool}
	s.StorageGroups = &StorageGroupStore{pool: pool}
	s.ClusterStorageLinks = &ClusterStorageLinkStore{pool: pool}
	s.OrphanedResources = &OrphanedResourceStore{pool: pool}
	s.ClusterConfigurations = &ClusterConfigurationStore{pool: pool}
	s.ProfileDeploymentMetrics = &ProfileDeploymentMetricsStore{pool: pool}
	s.PostConfigAddons = &PostConfigAddonStore{pool: pool}
	s.PostConfigTemplates = &PostConfigTemplateStore{pool: pool}

	return s
}

// BeginTx starts a new database transaction.
// The caller is responsible for committing or rolling back the transaction.
func (s *Store) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

// WithTx executes a function within a transaction
// If the function returns an error, the transaction is rolled back
// Otherwise, the transaction is committed
func (s *Store) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			tx.Rollback(ctx)
		}
	}()

	err = fn(tx)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Close closes the database connection pool
func (s *Store) Close() {
	s.pool.Close()
}

// Ping verifies the database connection is alive
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Stats returns database pool statistics
func (s *Store) Stats() *pgxpool.Stat {
	return s.pool.Stat()
}

// GetDatabaseHost returns the database host:port from the connection config
func (s *Store) GetDatabaseHost() string {
	config := s.pool.Config()
	if config == nil || config.ConnConfig == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", config.ConnConfig.Host, config.ConnConfig.Port)
}

// NewStore creates a new Store from a database URL
func NewStore(databaseURL string) (*Store, error) {
	// Parse the database URL to get pool config
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	// Configure connection pool timeouts
	config.MaxConns = 20                        // Maximum number of connections
	config.MinConns = 2                         // Minimum number of connections
	config.MaxConnLifetime = 1 * time.Hour      // Max lifetime of a connection
	config.MaxConnIdleTime = 30 * time.Minute   // Max idle time before closing
	config.HealthCheckPeriod = 1 * time.Minute  // How often to check connection health

	// Add statement timeout to prevent long-running queries
	// This adds a runtime parameter that PostgreSQL will enforce
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000" // 30 seconds in milliseconds

	// Create the pool with configured timeouts
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}

	return New(pool), nil
}

// Migrate runs database migrations
func (s *Store) Migrate() error {
	ctx := context.Background()

	// Create migrations tracking table if it doesn't exist
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Read migration files from embedded filesystem
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	// Sort migration files by name to ensure they run in order
	var migrationFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrationFiles = append(migrationFiles, entry.Name())
		}
	}
	sort.Strings(migrationFiles)

	// Run each migration that hasn't been applied yet
	for _, filename := range migrationFiles {
		// Extract version from filename (e.g., "00001" from "00001_initial_schema.sql")
		version := strings.Split(filename, "_")[0]

		// Check if migration has already been applied
		var exists bool
		err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration status for %s: %w", filename, err)
		}

		if exists {
			continue // Skip already applied migrations
		}

		// Read migration file
		content, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("read migration file %s: %w", filename, err)
		}

		// Extract SQL between -- +goose Up and -- +goose Down
		sql := string(content)

		// Look for +goose Up marker
		upMarker := "-- +goose Up"
		upIndex := strings.Index(sql, upMarker)
		if upIndex == -1 {
			return fmt.Errorf("missing '-- +goose Up' marker in %s", filename)
		}

		// Look for +goose Down marker
		downMarker := "-- +goose Down"
		downIndex := strings.Index(sql, downMarker)
		if downIndex == -1 {
			return fmt.Errorf("missing '-- +goose Down' marker in %s", filename)
		}

		// Extract everything between Up and Down markers
		// Skip to the line after the Up marker
		startPos := upIndex + len(upMarker)
		if nlIndex := strings.Index(sql[startPos:], "\n"); nlIndex != -1 {
			startPos = startPos + nlIndex + 1
		}

		// Extract the SQL and remove any goose-specific markers
		migrationSQL := sql[startPos:downIndex]

		// Remove any embedded -- +goose StatementBegin/End markers
		// These are goose-specific and not needed when executing all SQL at once
		migrationSQL = strings.ReplaceAll(migrationSQL, "-- +goose StatementBegin", "")
		migrationSQL = strings.ReplaceAll(migrationSQL, "-- +goose StatementEnd", "")

		migrationSQL = strings.TrimSpace(migrationSQL)

		// Execute migration in a transaction
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction for %s: %w", filename, err)
		}

		_, err = tx.Exec(ctx, migrationSQL)
		if err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", filename, err)
		}

		// Record that migration was applied
		_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
		if err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", filename, err)
		}

		err = tx.Commit(ctx)
		if err != nil {
			return fmt.Errorf("commit migration %s: %w", filename, err)
		}
	}

	return nil
}

// GetSchemaVersion returns the current database schema version
func (s *Store) GetSchemaVersion(ctx context.Context) (string, error) {
	var version string
	err := s.pool.QueryRow(ctx, `
		SELECT version FROM schema_migrations
		ORDER BY version DESC
		LIMIT 1
	`).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("get schema version: %w", err)
	}
	return version, nil
}

// GetExpectedSchemaVersion returns the latest migration version from embedded files
func (s *Store) GetExpectedSchemaVersion() (string, error) {
	// Read migration files from embedded filesystem
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return "", fmt.Errorf("read migrations directory: %w", err)
	}

	// Find the highest version number
	var latestVersion string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			// Extract version from filename (e.g., "00024" from "00024_description.sql")
			version := strings.Split(entry.Name(), "_")[0]
			if version > latestVersion {
				latestVersion = version
			}
		}
	}

	if latestVersion == "" {
		return "", fmt.Errorf("no migration files found")
	}

	return latestVersion, nil
}

// VerifyMigrations verifies that all expected migrations have been applied
func (s *Store) VerifyMigrations(ctx context.Context) error {
	expected, err := s.GetExpectedSchemaVersion()
	if err != nil {
		return fmt.Errorf("get expected schema version: %w", err)
	}

	current, err := s.GetSchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("get current schema version: %w", err)
	}

	if current != expected {
		return fmt.Errorf("schema version mismatch: expected %s, got %s (run migrations)", expected, current)
	}

	return nil
}
