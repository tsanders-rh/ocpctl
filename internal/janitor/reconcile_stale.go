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
// on that cloud having re-detected at least one resource this cycle: a broken
// detection pass (expired creds, API outage) that finds nothing must not sweep
// the entire active backlog into RESOLVED. Because the guard is per-cloud, a
// broken GCP cycle can't false-resolve AWS orphans and vice versa.
//
// Residual limitation (documented, accepted): if a single resource *type's*
// scan within an otherwise-healthy cloud fails continuously for longer than the
// stale window, that type's still-present orphans could be resolved. The 2h
// default (~8 cycles) makes this unlikely, and a later re-detection simply
// re-inserts the record as ACTIVE.
func (j *Janitor) reconcileStaleOrphans(ctx context.Context, awsDetected, gcpDetected int) {
	if j.config.OrphanStaleResolveAge <= 0 {
		return // disabled
	}
	cutoff := time.Now().Add(-j.config.OrphanStaleResolveAge)

	// Only sweep a cloud that proved healthy this cycle by re-detecting >0
	// resources. A zero count means either the cloud is genuinely clean or
	// detection is broken; both look identical here, so we fail safe and skip.
	if awsDetected > 0 {
		j.resolveStaleForCloud(ctx, store.OrphanCloudAWS, cutoff)
	}
	if gcpDetected > 0 {
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
