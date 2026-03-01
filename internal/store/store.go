package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Store provides database operations
type Store struct {
	pool *pgxpool.Pool

	Clusters       *ClusterStore
	Jobs           *JobStore
	JobLocks       *JobLockStore
	Idempotency    *IdempotencyStore
	RBAC           *RBACStore
	Audit          *AuditStore
	ClusterOutputs *ClusterOutputsStore
	Artifacts      *ArtifactStore
	Usage          *UsageStore
	Users          *UserStore
	RefreshTokens  *RefreshTokenStore
	IAMMappings    *IAMMappingStore
}

// New creates a new Store with all sub-stores initialized
func New(pool *pgxpool.Pool) *Store {
	s := &Store{
		pool: pool,
	}

	s.Clusters = &ClusterStore{pool: pool}
	s.Jobs = &JobStore{pool: pool}
	s.JobLocks = &JobLockStore{pool: pool}
	s.Idempotency = &IdempotencyStore{pool: pool}
	s.RBAC = &RBACStore{pool: pool}
	s.Audit = &AuditStore{pool: pool}
	s.ClusterOutputs = &ClusterOutputsStore{pool: pool}
	s.Artifacts = &ArtifactStore{pool: pool}
	s.Usage = &UsageStore{pool: pool}
	s.Users = &UserStore{pool: pool}
	s.RefreshTokens = &RefreshTokenStore{pool: pool}
	s.IAMMappings = &IAMMappingStore{
		pool:  pool,
		cache: make(map[string]*types.IAMPrincipalMapping),
	}

	return s
}

// BeginTx starts a new transaction
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
	// TODO: implement migrations
	return nil
}
