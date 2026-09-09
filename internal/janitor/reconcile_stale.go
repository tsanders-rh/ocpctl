package janitor

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/store"
)

// defaultOrphanStaleResolveAge is how long an ACTIVE orphaned-resource record may
// go without being re-detected before the janitor auto-resolves it. 2h is roughly
// eight 15-minute detection cycles -- long enough that a transient per-service
// scan error (one detector momentarily failing) does not cause a false resolve.
const defaultOrphanStaleResolveAge = 2 * time.Hour

// orphanStaleResolveAgeFromEnv reads ORPHAN_STALE_RESOLVE_HOURS (int hours),
// falling back to defaultOrphanStaleResolveAge. A value of 0 disables the sweep.
// Negative or unparseable values fall back to the default.
func orphanStaleResolveAgeFromEnv() time.Duration {
	v := os.Getenv("ORPHAN_STALE_RESOLVE_HOURS")
	if v == "" {
		return defaultOrphanStaleResolveAge
	}
	h, err := strconv.Atoi(v)
	if err != nil || h < 0 {
		log.Printf("[orphan-reconcile] invalid ORPHAN_STALE_RESOLVE_HOURS=%q, using default %s", v, defaultOrphanStaleResolveAge)
		return defaultOrphanStaleResolveAge
	}
	return time.Duration(h) * time.Hour
}

// reconcileStaleOrphans auto-resolves ACTIVE orphaned-resource records whose
// underlying cloud resource has disappeared (the detector stopped seeing them).
// Without this, such records sit ACTIVE forever, never re-detected, permanently
// below the safety gate's min-detections threshold -- so the console shows
// "deletion blocked" on resources that no longer exist and the active count is
// inflated by zombies.
//
// It NEVER touches the cloud; it only reconciles DB state. It is gated per-cloud
// on that cloud's detection pass SUCCEEDING this cycle (no error): a broken
// detection pass (expired creds, API outage) returns an error and must not sweep
// the entire active backlog into RESOLVED. Because the guard is per-cloud, a
// broken GCP cycle can't false-resolve AWS orphans and vice versa.
//
// A successful scan that finds ZERO orphans is the healthy steady state (e.g.
// once the detector correctly matches every resource to a live cluster) and MUST
// still sweep stale rows -- otherwise pre-existing ACTIVE zombies get stuck
// forever the moment a cloud goes clean. Gating on success (not count>0) is what
// makes that work; an earlier count>0 gate wedged stale rows whenever a cloud
// legitimately reported zero.
//
// Residual limitation (documented, accepted): if a single resource *type's*
// scan within an otherwise-healthy cloud fails without surfacing an error for
// longer than the stale window, that type's still-present orphans could be
// resolved. The 2h default (~8 cycles) makes this unlikely, and a later
// re-detection simply re-inserts the record as ACTIVE.
func (j *Janitor) reconcileStaleOrphans(ctx context.Context, awsOK, gcpOK bool) {
	if j.config.OrphanStaleResolveAge <= 0 {
		return // disabled
	}
	cutoff := time.Now().Add(-j.config.OrphanStaleResolveAge)

	// Only sweep a cloud whose detection pass completed without error this
	// cycle. A failed pass is the one case where a zero result is untrustworthy;
	// a successful pass is trusted even when it found nothing.
	if awsOK {
		j.resolveStaleForCloud(ctx, store.OrphanCloudAWS, cutoff)
	}
	if gcpOK {
		j.resolveStaleForCloud(ctx, store.OrphanCloudGCP, cutoff)
	}
}

func (j *Janitor) resolveStaleForCloud(ctx context.Context, cloud store.OrphanCloud, cutoff time.Time) {
	notes := fmt.Sprintf(
		"Auto-resolved by janitor: not re-detected since before %s (older than stale window %s); resource no longer present in cloud",
		cutoff.UTC().Format(time.RFC3339), j.config.OrphanStaleResolveAge)

	n, err := j.stores.orphaned.ResolveStale(ctx, cloud, cutoff, "janitor", notes)
	if err != nil {
		log.Printf("[orphan-reconcile] failed to resolve stale %s orphaned resources: %v", cloud, err)
		return
	}
	if n > 0 {
		log.Printf("[orphan-reconcile] auto-resolved %d stale %s orphaned resource(s) not re-detected in the last %s",
			n, cloud, j.config.OrphanStaleResolveAge)
	}
}
