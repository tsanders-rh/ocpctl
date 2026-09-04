package janitor

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// The interfaces below capture exactly the store methods the janitor uses. They
// exist so the janitor's DB-driven cleanup/safety logic can be unit-tested with
// mocks instead of a live PostgreSQL pool. The concrete *store.ClusterStore
// etc. satisfy them structurally, so production wiring (see storesFromStore) is
// unchanged. Keep these method sets minimal — add a method only when the
// janitor actually calls it.

type clusterStore interface {
	GetByID(ctx context.Context, id string) (*types.Cluster, error)
	List(ctx context.Context, filters store.ListFilters) ([]*types.Cluster, int, error)
	UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status types.ClusterStatus) error
	GetExpiredClusters(ctx context.Context) ([]*types.Cluster, error)
	ListAllStreaming(ctx context.Context, batchSize int) (<-chan []*types.Cluster, <-chan error)
	UpdateLastWorkHoursCheck(ctx context.Context, clusterID string) error
	DeleteDestroyedClusters(ctx context.Context, olderThan time.Time) (int, error)
	GetClustersForWorkHoursEnforcement(ctx context.Context) ([]*types.Cluster, error)
	GetMostRecentIDByName(ctx context.Context, name string) (string, error)
}

type jobStore interface {
	Create(ctx context.Context, tx pgx.Tx, job *types.Job) error
	GetByID(ctx context.Context, id string) (*types.Job, error)
	ListByClusterID(ctx context.Context, clusterID string) ([]*types.Job, error)
	MarkFailed(ctx context.Context, id string, errorCode, errorMessage string) error
	MarkFailedForRetry(ctx context.Context, id string, errorCode, errorMessage string) error
	GetStuckJobs(ctx context.Context, threshold time.Duration) ([]*types.Job, error)
	GetIncompleteFailedJobs(ctx context.Context) ([]*types.Job, error)
	CompleteFailedJobCleanup(ctx context.Context, id string) error
}

type jobLockStore interface {
	Release(ctx context.Context, clusterID, jobID string) error
	CleanupExpired(ctx context.Context) (int64, error)
	GetStuckLocks(ctx context.Context, stuckThreshold time.Duration) ([]*types.JobLock, error)
}

type idempotencyStore interface {
	CleanupExpired(ctx context.Context) (int64, error)
}

type userStore interface {
	GetByID(ctx context.Context, id string) (*types.User, error)
}

type orphanedResourceStore interface {
	Upsert(ctx context.Context, resource *types.OrphanedResource) error
	List(ctx context.Context, filters store.OrphanedResourceFilters) ([]*types.OrphanedResource, int, error)
	MarkResolved(ctx context.Context, id string, resolvedBy string, notes string) error
}

type auditStore interface {
	Log(ctx context.Context, event *types.AuditEvent) error
}

type deploymentMetricsStore interface {
	UpdateAllMetrics(ctx context.Context) (int, error)
}

// janitorStores groups the store seams the janitor depends on.
type janitorStores struct {
	clusters      clusterStore
	jobs          jobStore
	jobLocks      jobLockStore
	idempotency   idempotencyStore
	users         userStore
	orphaned      orphanedResourceStore
	audit         auditStore
	deployMetrics deploymentMetricsStore
}

// storesFromStore adapts a concrete *store.Store into the interface seams. A nil
// store yields empty seams (used only by tests that inject their own mocks).
func storesFromStore(st *store.Store) janitorStores {
	if st == nil {
		return janitorStores{}
	}
	return janitorStores{
		clusters:      st.Clusters,
		jobs:          st.Jobs,
		jobLocks:      st.JobLocks,
		idempotency:   st.Idempotency,
		users:         st.Users,
		orphaned:      st.OrphanedResources,
		audit:         st.Audit,
		deployMetrics: st.ProfileDeploymentMetrics,
	}
}
