package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	pool, err := pgxpool.New(context.Background(), databaseURL)
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
