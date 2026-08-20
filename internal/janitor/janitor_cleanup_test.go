package janitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// testJanitor wires a Janitor with mock store seams. metricsPublisher is left
// nil (every use site is nil-guarded), and workDir defaults to a temp dir.
type testMocks struct {
	clusters *mockClusterStore
	jobs     *mockJobStore
	locks    *mockJobLockStore
	idem     *mockIdempotencyStore
	users    *mockUserStore
	orphaned *mockOrphanedResourceStore
	metrics  *mockDeploymentMetricsStore
}

func newTestJanitor(t *testing.T, cfg *Config) (*Janitor, *testMocks) {
	t.Helper()
	if cfg == nil {
		cfg = DefaultConfig()
	}
	m := &testMocks{
		clusters: &mockClusterStore{},
		jobs:     &mockJobStore{},
		locks:    &mockJobLockStore{},
		idem:     &mockIdempotencyStore{},
		users:    &mockUserStore{},
		orphaned: &mockOrphanedResourceStore{},
		metrics:  &mockDeploymentMetricsStore{},
	}
	j := &Janitor{
		config:  cfg,
		workDir: t.TempDir(),
		stores: janitorStores{
			clusters:      m.clusters,
			jobs:          m.jobs,
			jobLocks:      m.locks,
			idempotency:   m.idem,
			users:         m.users,
			orphaned:      m.orphaned,
			deployMetrics: m.metrics,
		},
	}
	return j, m
}

func TestCleanupExpiredClusters(t *testing.T) {
	t.Run("creates destroy job and marks destroying", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.clusters.expired = []*types.Cluster{{ID: "c1", Name: "alpha"}}

		if err := j.cleanupExpiredClusters(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 1 {
			t.Fatalf("expected 1 job created, got %d", len(m.jobs.created))
		}
		job := m.jobs.created[0]
		if job.JobType != types.JobTypeJanitorDestroy {
			t.Errorf("expected JANITOR_DESTROY job, got %s", job.JobType)
		}
		if job.Metadata["reason"] != "TTL_EXPIRED" {
			t.Errorf("expected reason TTL_EXPIRED, got %v", job.Metadata["reason"])
		}
		if len(m.clusters.statusUpdates) != 1 || m.clusters.statusUpdates[0].status != types.ClusterStatusDestroying {
			t.Errorf("expected cluster marked DESTROYING, got %+v", m.clusters.statusUpdates)
		}
	})

	t.Run("skips preserve_on_failure clusters", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.clusters.expired = []*types.Cluster{{ID: "c1", Name: "keep", PreserveOnFailure: true}}

		if err := j.cleanupExpiredClusters(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no jobs created, got %d", len(m.jobs.created))
		}
		if len(m.clusters.statusUpdates) != 0 {
			t.Errorf("expected no status updates, got %+v", m.clusters.statusUpdates)
		}
	})

	t.Run("skips clusters with existing destroy job", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.clusters.expired = []*types.Cluster{{ID: "c1", Name: "busy"}}
		m.jobs.byCluster = map[string][]*types.Job{
			"c1": {{ID: "j1", JobType: types.JobTypeDestroy, Status: types.JobStatusPending}},
		}

		if err := j.cleanupExpiredClusters(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no new jobs, got %d", len(m.jobs.created))
		}
	})

	t.Run("nothing expired", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		if err := j.cleanupExpiredClusters(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no jobs, got %d", len(m.jobs.created))
		}
	})
}

func TestCleanupStuckJobs(t *testing.T) {
	tests := []struct {
		name         string
		job          *types.Job
		wantRetry    bool
		wantFailed   bool
		wantStatus   *types.ClusterStatus
		wantReleased bool
	}{
		{
			name:         "retry when attempts remain",
			job:          &types.Job{ID: "j1", ClusterID: "c1", JobType: types.JobTypeCreate, Attempt: 1, MaxAttempts: 3},
			wantRetry:    true,
			wantReleased: true,
		},
		{
			name:         "non-destroy exhausted marks cluster FAILED",
			job:          &types.Job{ID: "j2", ClusterID: "c2", JobType: types.JobTypeCreate, Attempt: 3, MaxAttempts: 3},
			wantFailed:   true,
			wantStatus:   ptrStatus(types.ClusterStatusFailed),
			wantReleased: true,
		},
		{
			name:         "destroy exhausted marks cluster DESTROY_FAILED",
			job:          &types.Job{ID: "j3", ClusterID: "c3", JobType: types.JobTypeDestroy, Attempt: 3, MaxAttempts: 3},
			wantFailed:   true,
			wantStatus:   ptrStatus(types.ClusterStatusDestroyFailed),
			wantReleased: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, m := newTestJanitor(t, nil)
			m.jobs.stuck = []*types.Job{tt.job}

			if err := j.cleanupStuckJobs(context.Background()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotRetry := len(m.jobs.markedRetry) == 1
			if gotRetry != tt.wantRetry {
				t.Errorf("retry: got %v want %v", gotRetry, tt.wantRetry)
			}
			gotFailed := len(m.jobs.markedFailed) == 1
			if gotFailed != tt.wantFailed {
				t.Errorf("failed: got %v want %v", gotFailed, tt.wantFailed)
			}
			if tt.wantReleased && len(m.locks.released) != 1 {
				t.Errorf("expected lock released, got %v", m.locks.released)
			}
			if tt.wantStatus != nil {
				if len(m.clusters.statusUpdates) != 1 || m.clusters.statusUpdates[0].status != *tt.wantStatus {
					t.Errorf("expected status %s, got %+v", *tt.wantStatus, m.clusters.statusUpdates)
				}
			} else if len(m.clusters.statusUpdates) != 0 {
				t.Errorf("expected no status update, got %+v", m.clusters.statusUpdates)
			}
		})
	}
}

func TestCleanupIncompleteFailedJobs(t *testing.T) {
	j, m := newTestJanitor(t, nil)
	m.jobs.incomplete = []*types.Job{{ID: "j1", ClusterID: "c1", JobType: types.JobTypeCreate}}

	if err := j.cleanupIncompleteFailedJobs(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.locks.released) != 1 {
		t.Errorf("expected lock released, got %v", m.locks.released)
	}
	if len(m.jobs.completed) != 1 || m.jobs.completed[0] != "j1" {
		t.Errorf("expected job j1 cleanup completed, got %v", m.jobs.completed)
	}
}

func TestDetectStuckLocks(t *testing.T) {
	j, m := newTestJanitor(t, nil)
	m.locks.stuck = []*types.JobLock{
		{ClusterID: "c1", JobID: "job12345-full", LockedBy: "worker-1", LockedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(30 * time.Minute)},
	}
	// GetByID lookups for cluster/job intentionally miss (best-effort context).
	if err := j.detectStuckLocks(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCleanupExpiredLocksAndKeys(t *testing.T) {
	j, m := newTestJanitor(t, nil)
	m.locks.expiredN = 4
	m.idem.expiredN = 7

	if err := j.cleanupExpiredLocks(context.Background()); err != nil {
		t.Fatalf("cleanupExpiredLocks: %v", err)
	}
	if err := j.cleanupExpiredKeys(context.Background()); err != nil {
		t.Fatalf("cleanupExpiredKeys: %v", err)
	}
}

func TestCleanupDestroyedClusters(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DestroyedClusterRetentionDays = 30
	j, m := newTestJanitor(t, cfg)
	m.clusters.deleteN = 2

	if err := j.cleanupDestroyedClusters(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Cutoff should be ~30 days ago.
	wantCutoff := time.Now().AddDate(0, 0, -30)
	if diff := m.clusters.deleteCutoff.Sub(wantCutoff); diff > time.Minute || diff < -time.Minute {
		t.Errorf("cutoff off by %v (got %v, want ~%v)", diff, m.clusters.deleteCutoff, wantCutoff)
	}
}

func TestCleanupFailedClusterDirs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailedClusterDirRetentionDays = 7
	j, m := newTestJanitor(t, cfg)

	old := time.Now().AddDate(0, 0, -10)
	recent := time.Now()

	m.clusters.listResult = []*types.Cluster{
		{ID: "old-failed", Name: "old", UpdatedAt: old},
		{ID: "old-preserve", Name: "preserve", UpdatedAt: old, PreserveOnFailure: true},
		{ID: "recent-failed", Name: "recent", UpdatedAt: recent},
	}
	for _, id := range []string{"old-failed", "old-preserve", "recent-failed"} {
		if err := os.MkdirAll(filepath.Join(j.workDir, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := j.cleanupFailedClusterDirs(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNotExist(t, filepath.Join(j.workDir, "old-failed"))
	assertExist(t, filepath.Join(j.workDir, "old-preserve"))
	assertExist(t, filepath.Join(j.workDir, "recent-failed"))
}

func TestCleanupOrphanedDirs(t *testing.T) {
	j, m := newTestJanitor(t, nil)
	m.clusters.streamBatches = [][]*types.Cluster{
		{{ID: "valid-1"}},
	}

	// valid-1 has a DB record, orphan-1 does not; a stray file must be ignored.
	if err := os.MkdirAll(filepath.Join(j.workDir, "valid-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(j.workDir, "orphan-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(j.workDir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := j.cleanupOrphanedDirs(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertExist(t, filepath.Join(j.workDir, "valid-1"))
	assertNotExist(t, filepath.Join(j.workDir, "orphan-1"))
	assertExist(t, filepath.Join(j.workDir, "stray.txt"))
}

func TestEnforceWorkHours(t *testing.T) {
	// A user whose work-hours window covers the entire day (start==end triggers
	// the wraparound branch => always within), and one that never has a work day.
	alwaysOn := &types.User{ID: "u1", Email: "u1@x", Timezone: "UTC", WorkHoursEnabled: true, WorkDays: 0x7F, WorkHoursStart: midnight(), WorkHoursEnd: midnight()}
	neverOn := &types.User{ID: "u1", Email: "u1@x", Timezone: "UTC", WorkHoursEnabled: true, WorkDays: 0, WorkHoursStart: midnight(), WorkHoursEnd: midnight()}

	t.Run("hibernates READY AWS cluster outside hours", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.users.byID = map[string]*types.User{"u1": neverOn}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "web", OwnerID: "u1", Status: types.ClusterStatusReady, Platform: types.PlatformAWS},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 1 || m.jobs.created[0].JobType != types.JobTypeHibernate {
			t.Fatalf("expected 1 hibernate job, got %+v", m.jobs.created)
		}
		if len(m.clusters.statusUpdates) != 1 || m.clusters.statusUpdates[0].status != types.ClusterStatusHibernating {
			t.Errorf("expected HIBERNATING status, got %+v", m.clusters.statusUpdates)
		}
		if len(m.clusters.lastCheckIDs) != 1 {
			t.Errorf("expected last-check updated, got %v", m.clusters.lastCheckIDs)
		}
	})

	t.Run("resumes HIBERNATED AWS cluster within hours", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.users.byID = map[string]*types.User{"u1": alwaysOn}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "web", OwnerID: "u1", Status: types.ClusterStatusHibernated, Platform: types.PlatformAWS},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 1 || m.jobs.created[0].JobType != types.JobTypeResume {
			t.Fatalf("expected 1 resume job, got %+v", m.jobs.created)
		}
		if len(m.clusters.statusUpdates) != 1 || m.clusters.statusUpdates[0].status != types.ClusterStatusResuming {
			t.Errorf("expected RESUMING status, got %+v", m.clusters.statusUpdates)
		}
	})

	t.Run("respects grace period", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		future := time.Now().Add(time.Hour)
		m.users.byID = map[string]*types.User{"u1": neverOn}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "web", OwnerID: "u1", Status: types.ClusterStatusReady, Platform: types.PlatformAWS, LastWorkHoursCheck: &future},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no jobs during grace period, got %+v", m.jobs.created)
		}
		// Grace period is preserved: last-check timestamp is NOT touched.
		if len(m.clusters.lastCheckIDs) != 0 {
			t.Errorf("expected last-check untouched, got %v", m.clusters.lastCheckIDs)
		}
	})

	t.Run("skips while post-deployment pending", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		pending := "pending"
		m.users.byID = map[string]*types.User{"u1": neverOn}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "web", OwnerID: "u1", Status: types.ClusterStatusReady, Platform: types.PlatformAWS, PostDeployStatus: &pending},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no jobs, got %+v", m.jobs.created)
		}
		if len(m.clusters.lastCheckIDs) != 1 {
			t.Errorf("expected last-check updated to throttle logs, got %v", m.clusters.lastCheckIDs)
		}
	})

	t.Run("skips non-AWS platform", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.users.byID = map[string]*types.User{"u1": neverOn}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "gke", OwnerID: "u1", Status: types.ClusterStatusReady, Platform: types.PlatformGCP},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no jobs for non-AWS, got %+v", m.jobs.created)
		}
		if len(m.clusters.lastCheckIDs) != 1 {
			t.Errorf("expected last-check updated, got %v", m.clusters.lastCheckIDs)
		}
	})

	t.Run("skips when active job present", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.users.byID = map[string]*types.User{"u1": neverOn}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "web", OwnerID: "u1", Status: types.ClusterStatusReady, Platform: types.PlatformAWS},
		}
		m.jobs.byCluster = map[string][]*types.Job{
			"c1": {{ID: "job12345-active", JobType: types.JobTypePostConfigure, Status: types.JobStatusPending}},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no hibernate job while active job runs, got %+v", m.jobs.created)
		}
		if len(m.clusters.lastCheckIDs) != 1 {
			t.Errorf("expected last-check updated, got %v", m.clusters.lastCheckIDs)
		}
	})

	t.Run("no action within hours updates check only", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		m.users.byID = map[string]*types.User{"u1": alwaysOn}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "web", OwnerID: "u1", Status: types.ClusterStatusReady, Platform: types.PlatformAWS},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 {
			t.Errorf("expected no jobs, got %+v", m.jobs.created)
		}
		if len(m.clusters.lastCheckIDs) != 1 {
			t.Errorf("expected last-check updated, got %v", m.clusters.lastCheckIDs)
		}
	})

	t.Run("skips when work hours disabled", func(t *testing.T) {
		j, m := newTestJanitor(t, nil)
		disabled := &types.User{ID: "u1", Timezone: "UTC", WorkHoursEnabled: false}
		m.users.byID = map[string]*types.User{"u1": disabled}
		m.clusters.forWorkHours = []*types.Cluster{
			{ID: "c1", Name: "web", OwnerID: "u1", Status: types.ClusterStatusReady, Platform: types.PlatformAWS},
		}

		if err := j.enforceWorkHours(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.jobs.created) != 0 || len(m.clusters.lastCheckIDs) != 0 {
			t.Errorf("expected no action when work hours disabled")
		}
	})
}

func TestUpdateDeploymentMetrics(t *testing.T) {
	j, m := newTestJanitor(t, nil)
	m.metrics.updatedN = 5
	if err := j.updateDeploymentMetrics(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRun exercises the full cleanup orchestration against the mock seams. Cloud
// orphan detection is disabled so run() doesn't reach the inline-constructed
// AWS/GCP clients (those need the piece-2 client-interface refactor to test).
func TestRun(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OrphanDetection = false
	j, m := newTestJanitor(t, cfg)
	j.ctx = context.Background()

	m.clusters.expired = []*types.Cluster{{ID: "c1", Name: "alpha"}}
	m.jobs.stuck = []*types.Job{{ID: "j1", ClusterID: "c1", JobType: types.JobTypeCreate, Attempt: 1, MaxAttempts: 3}}
	m.metrics.updatedN = 1

	// run() logs and swallows per-task errors; it must not panic and must drive
	// the sub-tasks (a destroy job is created for the expired cluster).
	j.run()

	if len(m.jobs.created) == 0 {
		t.Errorf("expected run() to create a destroy job for the expired cluster")
	}
}

func TestStartStop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OrphanDetection = false
	cfg.CheckInterval = time.Hour // long enough that only the immediate run() fires
	j, _ := newTestJanitor(t, cfg)

	done := make(chan error, 1)
	go func() { done <- j.Start(context.Background()) }()

	// Give Start() a moment to perform its immediate run() and enter the loop.
	time.Sleep(50 * time.Millisecond)
	j.Stop()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected Start to return the cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

// --- helpers ---

func ptrStatus(s types.ClusterStatus) *types.ClusterStatus { return &s }

func midnight() time.Time { return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) }

func assertExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, stat err=%v", path, err)
	}
}
