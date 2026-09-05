package janitor

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// These mocks satisfy the janitor store seams (see stores.go) so the DB-driven
// cleanup/safety methods can be exercised without a live PostgreSQL pool. Each
// mock records the mutating calls the tests assert on and returns preconfigured
// data for the reads.

type statusUpdate struct {
	id     string
	status types.ClusterStatus
}

type mockClusterStore struct {
	// reads
	expired         []*types.Cluster
	expiredErr      error
	listResult      []*types.Cluster
	listErr         error
	forWorkHours    []*types.Cluster
	forWorkHoursErr error
	byID            map[string]*types.Cluster
	streamBatches   [][]*types.Cluster
	streamErr       error
	deleteN         int
	deleteErr       error
	updateStatusErr error
	idByName        map[string]string
	idByNameErr     error

	// recorded writes
	statusUpdates []statusUpdate
	lastCheckIDs  []string
	deleteCutoff  time.Time
}

func (m *mockClusterStore) GetByID(ctx context.Context, id string) (*types.Cluster, error) {
	if m.byID != nil {
		if c, ok := m.byID[id]; ok {
			return c, nil
		}
	}
	return nil, fmt.Errorf("cluster %s not found", id)
}

func (m *mockClusterStore) List(ctx context.Context, filters store.ListFilters) ([]*types.Cluster, int, error) {
	return m.listResult, len(m.listResult), m.listErr
}

func (m *mockClusterStore) UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status types.ClusterStatus) error {
	m.statusUpdates = append(m.statusUpdates, statusUpdate{id: id, status: status})
	return m.updateStatusErr
}

func (m *mockClusterStore) GetExpiredClusters(ctx context.Context) ([]*types.Cluster, error) {
	return m.expired, m.expiredErr
}

func (m *mockClusterStore) ListAllStreaming(ctx context.Context, batchSize int) (<-chan []*types.Cluster, <-chan error) {
	ch := make(chan []*types.Cluster, len(m.streamBatches)+1)
	errCh := make(chan error, 1)
	if m.streamErr != nil {
		// Leave ch open and empty so the janitor's select fires the error case.
		errCh <- m.streamErr
		return ch, errCh
	}
	for _, b := range m.streamBatches {
		ch <- b
	}
	close(ch)
	close(errCh)
	return ch, errCh
}

func (m *mockClusterStore) UpdateLastWorkHoursCheck(ctx context.Context, clusterID string) error {
	m.lastCheckIDs = append(m.lastCheckIDs, clusterID)
	return nil
}

func (m *mockClusterStore) DeleteDestroyedClusters(ctx context.Context, olderThan time.Time) (int, error) {
	m.deleteCutoff = olderThan
	return m.deleteN, m.deleteErr
}

func (m *mockClusterStore) GetClustersForWorkHoursEnforcement(ctx context.Context) ([]*types.Cluster, error) {
	return m.forWorkHours, m.forWorkHoursErr
}

func (m *mockClusterStore) GetMostRecentIDByName(ctx context.Context, name string) (string, error) {
	if m.idByNameErr != nil {
		return "", m.idByNameErr
	}
	if m.idByName != nil {
		return m.idByName[name], nil
	}
	return "", nil
}

type mockJobStore struct {
	// reads
	byCluster     map[string][]*types.Job
	listErr       error
	byID          map[string]*types.Job
	stuck         []*types.Job
	stuckErr      error
	incomplete    []*types.Job
	incompleteErr error
	createErr     error

	// recorded writes
	created      []*types.Job
	markedFailed []string
	markedRetry  []string
	completed    []string
}

func (m *mockJobStore) Create(ctx context.Context, tx pgx.Tx, job *types.Job) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.created = append(m.created, job)
	return nil
}

func (m *mockJobStore) GetByID(ctx context.Context, id string) (*types.Job, error) {
	if m.byID != nil {
		if j, ok := m.byID[id]; ok {
			return j, nil
		}
	}
	return nil, fmt.Errorf("job %s not found", id)
}

func (m *mockJobStore) ListByClusterID(ctx context.Context, clusterID string) ([]*types.Job, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.byCluster != nil {
		return m.byCluster[clusterID], nil
	}
	return nil, nil
}

func (m *mockJobStore) MarkFailed(ctx context.Context, id, errorCode, errorMessage string) error {
	m.markedFailed = append(m.markedFailed, id)
	return nil
}

func (m *mockJobStore) MarkFailedForRetry(ctx context.Context, id, errorCode, errorMessage string) error {
	m.markedRetry = append(m.markedRetry, id)
	return nil
}

func (m *mockJobStore) GetStuckJobs(ctx context.Context, threshold time.Duration) ([]*types.Job, error) {
	return m.stuck, m.stuckErr
}

func (m *mockJobStore) GetIncompleteFailedJobs(ctx context.Context) ([]*types.Job, error) {
	return m.incomplete, m.incompleteErr
}

func (m *mockJobStore) CompleteFailedJobCleanup(ctx context.Context, id string) error {
	m.completed = append(m.completed, id)
	return nil
}

type mockJobLockStore struct {
	expiredN int64
	stuck    []*types.JobLock
	stuckErr error

	released []string // "clusterID:jobID"
}

func (m *mockJobLockStore) Release(ctx context.Context, clusterID, jobID string) error {
	m.released = append(m.released, clusterID+":"+jobID)
	return nil
}

func (m *mockJobLockStore) CleanupExpired(ctx context.Context) (int64, error) {
	return m.expiredN, nil
}

func (m *mockJobLockStore) GetStuckLocks(ctx context.Context, stuckThreshold time.Duration) ([]*types.JobLock, error) {
	return m.stuck, m.stuckErr
}

type mockIdempotencyStore struct {
	expiredN int64
	err      error
}

func (m *mockIdempotencyStore) CleanupExpired(ctx context.Context) (int64, error) {
	return m.expiredN, m.err
}

type mockUserStore struct {
	byID map[string]*types.User
}

func (m *mockUserStore) GetByID(ctx context.Context, id string) (*types.User, error) {
	if m.byID != nil {
		if u, ok := m.byID[id]; ok {
			return u, nil
		}
	}
	return nil, fmt.Errorf("user %s not found", id)
}

type resolvedCall struct {
	id         string
	resolvedBy string
	notes      string
}

type mockOrphanedResourceStore struct {
	upserts []*types.OrphanedResource
	err     error

	// reads
	listResult []*types.OrphanedResource
	listTotal  int // overrides the reported total when > 0 (to simulate truncation)
	listErr    error

	// recorded writes
	resolved     []resolvedCall
	markResolved error
}

func (m *mockOrphanedResourceStore) Upsert(ctx context.Context, resource *types.OrphanedResource) error {
	m.upserts = append(m.upserts, resource)
	return m.err
}

func (m *mockOrphanedResourceStore) List(ctx context.Context, filters store.OrphanedResourceFilters) ([]*types.OrphanedResource, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	total := len(m.listResult)
	if m.listTotal > 0 {
		total = m.listTotal
	}
	return m.listResult, total, nil
}

func (m *mockOrphanedResourceStore) MarkResolved(ctx context.Context, id, resolvedBy, notes string) error {
	m.resolved = append(m.resolved, resolvedCall{id: id, resolvedBy: resolvedBy, notes: notes})
	return m.markResolved
}

type mockAuditStore struct {
	events []*types.AuditEvent
}

func (m *mockAuditStore) Log(ctx context.Context, event *types.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

type mockSystemSettingsStore struct {
	values   map[string][]byte
	getErr   error
	upserted map[string][]byte // key -> last value written
}

func (m *mockSystemSettingsStore) Get(ctx context.Context, key string) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.values == nil {
		return nil, nil
	}
	return m.values[key], nil
}

func (m *mockSystemSettingsStore) Upsert(ctx context.Context, key string, value []byte, updatedBy string) error {
	if m.upserted == nil {
		m.upserted = make(map[string][]byte)
	}
	m.upserted[key] = value
	return nil
}

type mockDeploymentMetricsStore struct {
	updatedN int
	err      error
}

func (m *mockDeploymentMetricsStore) UpdateAllMetrics(ctx context.Context) (int, error) {
	return m.updatedN, m.err
}
