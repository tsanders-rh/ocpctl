# Store Package

Database access layer for ocpctl using pgx connection pooling.

## Structure

- `config.go` - Database configuration and connection pooling
- `store.go` - Main Store struct that aggregates all sub-stores
- `clusters.go` - Cluster CRUD operations
- `jobs.go` - Job CRUD operations
- `locks.go` - Job lock management (critical for worker concurrency)
- `idempotency.go` - Idempotency key storage
- `rbac.go` - RBAC mapping queries
- `audit.go` - Audit event logging
- `outputs.go` - Cluster outputs and artifacts
- `usage.go` - Usage tracking
- `errors.go` - Common store errors

## Usage

```go
import (
    "context"
    "github.com/tsanders-rh/ocpctl/internal/store"
    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// Create connection pool
ctx := context.Background()
cfg := store.DefaultConfig(databaseURL)
pool, err := store.NewPool(ctx, cfg)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()

// Initialize store
s := store.New(pool)

// Create a cluster
cluster := &types.Cluster{
    ID:       types.GenerateClusterID(),
    Name:     "my-cluster",
    Platform: types.PlatformAWS,
    Status:   types.ClusterStatusPending,
    // ... other fields
}
err = s.Clusters.Create(ctx, cluster)

// Get cluster by ID
cluster, err = s.Clusters.GetByID(ctx, clusterID)

// List clusters with filters
filters := store.ListFilters{
    Team:   ptr("team-a"),
    Status: ptr(types.ClusterStatusReady),
    Limit:  50,
    Offset: 0,
}
clusters, total, err := s.Clusters.List(ctx, filters)

// Use transactions for atomic operations
err = s.WithTx(ctx, func(tx pgx.Tx) error {
    // Acquire lock
    locked, err := s.JobLocks.Acquire(ctx, tx, clusterID, jobID, workerID, 60*time.Minute)
    if !locked || err != nil {
        return err
    }

    // Update cluster status
    err = s.Clusters.UpdateStatus(ctx, tx, clusterID, types.ClusterStatusCreating)
    if err != nil {
        return err
    }

    // Mark job started
    return s.Jobs.MarkStarted(ctx, tx, jobID)
})
```

## Critical Worker Pattern

When workers process jobs, they MUST use this lock acquisition pattern:

```go
// 1. Begin transaction
tx, err := s.BeginTx(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

// 2. Lock cluster row with FOR UPDATE
cluster, err := s.Clusters.GetByIDForUpdate(ctx, tx, clusterID)
if err != nil {
    return err
}

// 3. Validate state transition is allowed
if !isValidTransition(cluster.Status, job.JobType) {
    return ErrInvalidStateTransition
}

// 4. Acquire job lock
locked, err := s.JobLocks.Acquire(ctx, tx, clusterID, jobID, workerID, lockTTL)
if !locked || err != nil {
    return err // Lock held by another worker
}

// 5. Update job and cluster status
err = s.Jobs.MarkStarted(ctx, tx, jobID)
if err != nil {
    return err
}

err = s.Clusters.UpdateStatus(ctx, tx, clusterID, newStatus)
if err != nil {
    return err
}

// 6. Commit transaction
err = tx.Commit(ctx)
if err != nil {
    return err
}

// 7. Start heartbeat goroutine to maintain lock
go maintainLockHeartbeat(ctx, clusterID, jobID)
```

## Testing

Run unit tests:
```bash
go test -short ./internal/store/...
```

Run integration tests (requires PostgreSQL):
```bash
make docker-up
go test -tags=integration ./internal/store/...
```

Integration tests use testcontainers to spin up real PostgreSQL instances.

## Performance Considerations

1. **Connection Pooling**: Default max 25 connections, min 5
2. **Transaction Isolation**: Use serializable for lock acquisition
3. **Indexes**: All critical queries have indexes (see migration)
4. **Prepared Statements**: pgx automatically prepares frequently used queries

## Error Handling

All store methods return errors. Check for specific errors:

```go
cluster, err := s.Clusters.GetByID(ctx, id)
if errors.Is(err, store.ErrNotFound) {
    // Handle not found
}
if err != nil {
    // Handle other errors
}
```

Common errors:
- `store.ErrNotFound` - Record not found
- `store.ErrConflict` - Unique constraint violation
- `store.ErrLockHeld` - Lock already held by another worker
