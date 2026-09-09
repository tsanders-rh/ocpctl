package janitor

import (
	"context"
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/store"
)

func newReconcileJanitor(staleAge time.Duration, orphaned *mockOrphanedResourceStore) *Janitor {
	return &Janitor{
		config: &Config{OrphanStaleResolveAge: staleAge},
		stores: janitorStores{orphaned: orphaned},
	}
}

func TestReconcileStaleOrphans_DisabledWhenAgeZero(t *testing.T) {
	m := &mockOrphanedResourceStore{}
	j := newReconcileJanitor(0, m)

	j.reconcileStaleOrphans(context.Background(), true, true)

	if len(m.resolveStaleCalls) != 0 {
		t.Fatalf("expected no ResolveStale calls when disabled, got %d", len(m.resolveStaleCalls))
	}
}

func TestReconcileStaleOrphans_SkipsCloudWhoseDetectionFailed(t *testing.T) {
	m := &mockOrphanedResourceStore{resolveStaleN: 3}
	j := newReconcileJanitor(2*time.Hour, m)

	// AWS detection succeeded, GCP detection errored -> only AWS is swept.
	j.reconcileStaleOrphans(context.Background(), true, false)

	if len(m.resolveStaleCalls) != 1 {
		t.Fatalf("expected exactly 1 ResolveStale call, got %d", len(m.resolveStaleCalls))
	}
	if got := m.resolveStaleCalls[0].cloud; got != store.OrphanCloudAWS {
		t.Fatalf("expected AWS sweep, got %q", got)
	}
}

func TestReconcileStaleOrphans_SkipsAllWhenBothDetectionsFailed(t *testing.T) {
	m := &mockOrphanedResourceStore{}
	j := newReconcileJanitor(2*time.Hour, m)

	// Both detection passes errored (e.g. expired creds / outage) -> never sweep.
	j.reconcileStaleOrphans(context.Background(), false, false)

	if len(m.resolveStaleCalls) != 0 {
		t.Fatalf("expected no ResolveStale calls when both detections failed, got %d", len(m.resolveStaleCalls))
	}
}

// TestReconcileStaleOrphans_SweepsCleanCloud is the regression guard for the
// incident: a cloud whose detection succeeds but finds ZERO orphans (the healthy
// steady state after the detector correctly matches every resource to a live
// cluster) must still sweep its lingering ACTIVE zombies. The gate is success,
// not count>0, so awsOK/gcpOK are true even when the cycle detected nothing.
func TestReconcileStaleOrphans_SweepsCleanCloud(t *testing.T) {
	m := &mockOrphanedResourceStore{resolveStaleN: 2}
	j := newReconcileJanitor(2*time.Hour, m)

	j.reconcileStaleOrphans(context.Background(), true, true)

	if len(m.resolveStaleCalls) != 2 {
		t.Fatalf("expected both clouds swept on a clean-but-successful cycle, got %d", len(m.resolveStaleCalls))
	}
}

func TestReconcileStaleOrphans_SweepsBothCloudsWhenHealthy(t *testing.T) {
	m := &mockOrphanedResourceStore{resolveStaleN: 1}
	j := newReconcileJanitor(2*time.Hour, m)

	before := time.Now()
	j.reconcileStaleOrphans(context.Background(), true, true)
	after := time.Now()

	if len(m.resolveStaleCalls) != 2 {
		t.Fatalf("expected 2 ResolveStale calls (AWS+GCP), got %d", len(m.resolveStaleCalls))
	}

	clouds := map[store.OrphanCloud]bool{}
	for _, c := range m.resolveStaleCalls {
		clouds[c.cloud] = true
		if c.resolvedBy != "janitor" {
			t.Errorf("expected resolvedBy=janitor, got %q", c.resolvedBy)
		}
		// Cutoff must be ~staleAge in the past.
		wantMin := before.Add(-2 * time.Hour)
		wantMax := after.Add(-2 * time.Hour)
		if c.cutoff.Before(wantMin) || c.cutoff.After(wantMax) {
			t.Errorf("cutoff %v not within expected window [%v, %v]", c.cutoff, wantMin, wantMax)
		}
	}
	if !clouds[store.OrphanCloudAWS] || !clouds[store.OrphanCloudGCP] {
		t.Fatalf("expected both AWS and GCP sweeps, got %v", clouds)
	}
}

func TestReconcileStaleOrphans_StoreErrorIsNonFatal(t *testing.T) {
	m := &mockOrphanedResourceStore{resolveStaleErr: context.DeadlineExceeded}
	j := newReconcileJanitor(2*time.Hour, m)

	// Must not panic and must still attempt both clouds.
	j.reconcileStaleOrphans(context.Background(), true, true)

	if len(m.resolveStaleCalls) != 2 {
		t.Fatalf("expected both clouds attempted despite errors, got %d calls", len(m.resolveStaleCalls))
	}
}

func TestOrphanStaleResolveAgeFromEnv(t *testing.T) {
	t.Run("default when unset", func(t *testing.T) {
		t.Setenv("ORPHAN_STALE_RESOLVE_HOURS", "")
		if got := orphanStaleResolveAgeFromEnv(); got != defaultOrphanStaleResolveAge {
			t.Fatalf("got %s, want default %s", got, defaultOrphanStaleResolveAge)
		}
	})
	t.Run("explicit hours", func(t *testing.T) {
		t.Setenv("ORPHAN_STALE_RESOLVE_HOURS", "6")
		if got := orphanStaleResolveAgeFromEnv(); got != 6*time.Hour {
			t.Fatalf("got %s, want 6h", got)
		}
	})
	t.Run("zero disables", func(t *testing.T) {
		t.Setenv("ORPHAN_STALE_RESOLVE_HOURS", "0")
		if got := orphanStaleResolveAgeFromEnv(); got != 0 {
			t.Fatalf("got %s, want 0", got)
		}
	})
	t.Run("invalid falls back to default", func(t *testing.T) {
		t.Setenv("ORPHAN_STALE_RESOLVE_HOURS", "nonsense")
		if got := orphanStaleResolveAgeFromEnv(); got != defaultOrphanStaleResolveAge {
			t.Fatalf("got %s, want default %s", got, defaultOrphanStaleResolveAge)
		}
	})
	t.Run("negative falls back to default", func(t *testing.T) {
		t.Setenv("ORPHAN_STALE_RESOLVE_HOURS", "-3")
		if got := orphanStaleResolveAgeFromEnv(); got != defaultOrphanStaleResolveAge {
			t.Fatalf("got %s, want default %s", got, defaultOrphanStaleResolveAge)
		}
	})
}
